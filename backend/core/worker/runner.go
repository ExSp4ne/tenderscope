// Package worker запускает Python-анализатор в фоне и сохраняет результат в БД.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"tenderscope/backend/core/storage"
	"tenderscope/backend/eis"
)

const (
	// AnalysisTimeout — максимальное время одного запуска анализатора.
	AnalysisTimeout = 5 * time.Minute
	maxParallelJobs = 2
	maxResultSize   = 10 << 20 // 10 МБ
)

var ErrClosed = errors.New("worker is shut down")

// Runner выполняет проверки в фоновых горутинах (не более maxParallelJobs одновременно).
type Runner struct {
	repos *storage.Repositories

	// exec запускает анализатор и должен записать результат в outPath. В тестах подменяется.
	exec func(ctx context.Context, file, law, outPath string) error

	ctx    context.Context
	cancel context.CancelFunc
	sem    chan struct{}
	wg     sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

// New создаёт воркер. Python и скрипт определяются из окружения:
//
//	TENDERSCOPE_PYTHON — интерпретатор (по умолчанию python на Windows, python3 иначе),
//	ANALYZER_SCRIPT    — путь к run_analysis.py (по умолчанию ищется analyzer_bridge/run_analysis.py
//	                     вверх от текущей папки).
func New(repos *storage.Repositories) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{repos: repos, ctx: ctx, cancel: cancel, sem: make(chan struct{}, maxParallelJobs)}
	r.exec = defaultExec(pythonBin(), analyzerScript())
	return r
}

// Submit запускает анализ в отдельной горутине и сразу возвращается.
func (r *Runner) Submit(auditID, filePath, lawType string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	r.wg.Add(1)
	go r.run(auditID, filePath, lawType)
	return nil
}

// Shutdown отменяет текущие запуски и ждёт завершения горутин (не дольше ctx).
func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.cancel()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) run(auditID, filePath, lawType string) {
	defer r.wg.Done()
	defer func() {
		if p := recover(); p != nil {
			log.Printf("audit %s: panic: %v", auditID, p)
			r.fail(auditID)
		}
	}()

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-r.ctx.Done():
		log.Printf("audit %s: отменён до запуска", auditID)
		r.fail(auditID)
		return
	}

	items, err := r.analyze(auditID, filePath, lawType)
	if err != nil {
		log.Printf("audit %s: failed: %v", auditID, err)
		r.fail(auditID)
		return
	}

	// Контекст сохранения независим от r.ctx: результат, который уже получен, не теряем при остановке.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := r.repos.Audits.Complete(ctx, auditID, items); err != nil {
		log.Printf("audit %s: save result: %v", auditID, err)
		r.fail(auditID)
		return
	}
	log.Printf("audit %s: completed, risks: %d", auditID, len(items))
}

func (r *Runner) analyze(auditID, filePath, lawType string) ([]storage.RiskItem, error) {
	dir := filepath.Join(eis.TmpDir(), "audits", auditID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create tmp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "tmp_result.json")

	ctx, cancel := context.WithTimeout(r.ctx, AnalysisTimeout)
	defer cancel()
	if err := r.exec(ctx, filePath, lawType, outPath); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("таймаут %s: %w", AnalysisTimeout, err)
		}
		return nil, err
	}
	return parseResult(outPath)
}

func (r *Runner) fail(auditID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := r.repos.Audits.UpdateStatus(ctx, auditID, "failed"); err != nil {
		log.Printf("audit %s: mark failed: %v", auditID, err)
	}
}

// ---- запуск Python ----

func defaultExec(python, script string) func(ctx context.Context, file, law, outPath string) error {
	return func(ctx context.Context, file, law, outPath string) error {
		if script == "" {
			return errors.New("не найден analyzer_bridge/run_analysis.py (задайте ANALYZER_SCRIPT)")
		}
		cmd := exec.CommandContext(ctx, python, script, "--file="+file, "--law="+law, "--out="+outPath)
		cmd.Dir = filepath.Dir(script)
		cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
		cmd.WaitDelay = 5 * time.Second
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w; вывод: %s", python, err, tail(string(out), 2000))
		}
		return nil
	}
}

func pythonBin() string {
	if v := os.Getenv("TENDERSCOPE_PYTHON"); v != "" {
		return v
	}
	names := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		names = []string{"python", "python3"}
	}
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			return n
		}
	}
	return names[0]
}

func analyzerScript() string {
	if v := os.Getenv("ANALYZER_SCRIPT"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
		return v
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for d := wd; ; {
		p := filepath.Join(d, "analyzer_bridge", "run_analysis.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// ---- разбор результата ----

// rawRisk принимает оба написания полей (camelCase из контракта и snake_case из ml-промпта).
type rawRisk struct {
	Category       string `json:"category"`
	Level          string `json:"level"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
	Quote          string `json:"quote"`
	ExactQuote     string `json:"exact_quote"`
	PageNumber     int    `json:"pageNumber"`
	PageNumberSnk  int    `json:"page_number"`
	ClauseNumber   string `json:"clauseNumber"`
	ClauseNumSnk   string `json:"clause_number"`
}

// parseResult читает tmp_result.json: {"risks": [...]} или просто [...].
func parseResult(path string) ([]storage.RiskItem, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("анализатор не создал файл результата: %w", err)
	}
	if st.Size() > maxResultSize {
		return nil, fmt.Errorf("файл результата слишком большой (%d байт)", st.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raws []rawRisk
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		err = json.Unmarshal(data, &raws)
	} else {
		var doc struct {
			Risks *[]rawRisk `json:"risks"`
		}
		err = json.Unmarshal(data, &doc)
		if err == nil {
			if doc.Risks == nil {
				return nil, errors.New(`в результате нет поля "risks"`)
			}
			raws = *doc.Risks
		}
	}
	if err != nil {
		return nil, fmt.Errorf("некорректный JSON результата: %w", err)
	}

	items := make([]storage.RiskItem, 0, len(raws))
	for i, rr := range raws {
		switch rr.Level {
		case "red", "yellow", "green":
		default:
			return nil, fmt.Errorf("риск #%d: level %q (нужно red/yellow/green)", i+1, rr.Level)
		}
		if strings.TrimSpace(rr.Category) == "" || strings.TrimSpace(rr.Title) == "" || strings.TrimSpace(rr.Description) == "" {
			return nil, fmt.Errorf("риск #%d: category, title и description обязательны", i+1)
		}
		it := storage.RiskItem{
			ID: storage.NewID(), Category: rr.Category, Level: rr.Level,
			Title: rr.Title, Description: rr.Description,
		}
		if rr.Recommendation != "" {
			v := rr.Recommendation
			it.Recommendation = &v
		}
		if q := firstNonEmpty(rr.Quote, rr.ExactQuote); q != "" {
			it.Quote = &q
		}
		if p := firstNonZero(rr.PageNumber, rr.PageNumberSnk); p > 0 {
			it.PageNumber = &p
		}
		if c := firstNonEmpty(rr.ClauseNumber, rr.ClauseNumSnk); c != "" {
			it.ClauseNumber = &c
		}
		items = append(items, it)
	}
	return items, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
