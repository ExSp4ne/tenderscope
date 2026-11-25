package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tenderscope/backend/core/storage"
)

func newRepos(t *testing.T) *storage.Repositories {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return storage.NewSQLiteRepositories(db)
}

func newAudit(t *testing.T, repos *storage.Repositories) string {
	t.Helper()
	tn := &storage.Tender{ID: storage.NewID(), Title: "f.pdf", LawType: "44-ФЗ"}
	a := &storage.Audit{ID: storage.NewID(), TenderID: tn.ID, Status: "processing"}
	if err := repos.Audits.CreateWithTender(context.Background(), tn, a); err != nil {
		t.Fatal(err)
	}
	return a.ID
}

func waitStatus(t *testing.T, repos *storage.Repositories, id string) *storage.Audit {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a, err := repos.Audits.GetByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if a.Status != "processing" {
			return a
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("статус не сменился с processing")
	return nil
}

const okResult = `{"risks":[
 {"category":"payment","level":"red","title":"t1","description":"d","recommendation":"r","quote":"q","pageNumber":18,"clauseNumber":"4.3"},
 {"category":"delivery","level":"yellow","title":"t2","description":"d","exact_quote":"q2","page_number":25,"clause_number":"6.2"},
 {"category":"termination","level":"green","title":"t3","description":"d"}]}`

func TestRunnerCompletes(t *testing.T) {
	t.Setenv("TENDERSCOPE_TMP_DIR", t.TempDir())
	repos := newRepos(t)
	r := New(repos)
	var gotFile, gotLaw string
	r.exec = func(ctx context.Context, file, law, out string) error {
		gotFile, gotLaw = file, law
		return os.WriteFile(out, []byte(okResult), 0o644)
	}
	id := newAudit(t, repos)
	if err := r.Submit(id, "/x/contract.pdf", "44-ФЗ"); err != nil {
		t.Fatal(err)
	}
	a := waitStatus(t, repos, id)
	if a.Status != "completed" || a.RedCount != 1 || a.YellowCount != 1 || a.GreenCount != 1 {
		t.Fatalf("audit: %+v", a)
	}
	if gotFile != "/x/contract.pdf" || gotLaw != "44-ФЗ" {
		t.Fatalf("аргументы: %q %q", gotFile, gotLaw)
	}
	items, _ := repos.RiskItems.ListByAudit(context.Background(), id)
	if len(items) != 3 || *items[1].Quote != "q2" || *items[1].PageNumber != 25 || *items[1].ClauseNumber != "6.2" {
		t.Fatalf("items: %+v", items)
	}
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerFailures(t *testing.T) {
	cases := map[string]func(ctx context.Context, file, law, out string) error{
		"exit code": func(context.Context, string, string, string) error { return errors.New("exit status 1") },
		"no file":   func(context.Context, string, string, string) error { return nil },
		"bad json":  func(_ context.Context, _, _, out string) error { return os.WriteFile(out, []byte("{oops"), 0o644) },
		"bad level": func(_ context.Context, _, _, out string) error {
			return os.WriteFile(out, []byte(`{"risks":[{"category":"c","level":"blue","title":"t","description":"d"}]}`), 0o644)
		},
		"no risks": func(_ context.Context, _, _, out string) error { return os.WriteFile(out, []byte(`{}`), 0o644) },
		"panic":    func(context.Context, string, string, string) error { panic("boom") },
		"empty title": func(_ context.Context, _, _, out string) error {
			return os.WriteFile(out, []byte(`[{"category":"c","level":"red","title":" ","description":"d"}]`), 0o644)
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TENDERSCOPE_TMP_DIR", t.TempDir())
			repos := newRepos(t)
			r := New(repos)
			r.exec = fn
			id := newAudit(t, repos)
			if err := r.Submit(id, "f.pdf", "44-ФЗ"); err != nil {
				t.Fatal(err)
			}
			if a := waitStatus(t, repos, id); a.Status != "failed" {
				t.Fatalf("ожидался failed, получено %+v", a)
			}
			items, _ := repos.RiskItems.ListByAudit(context.Background(), id)
			if len(items) != 0 {
				t.Fatalf("риски не должны сохраняться при ошибке: %+v", items)
			}
			r.Shutdown(context.Background())
		})
	}
}

func TestRunnerEmptyRisksIsCompleted(t *testing.T) {
	t.Setenv("TENDERSCOPE_TMP_DIR", t.TempDir())
	repos := newRepos(t)
	r := New(repos)
	r.exec = func(_ context.Context, _, _, out string) error {
		return os.WriteFile(out, []byte(`{"risks":[]}`), 0o644)
	}
	id := newAudit(t, repos)
	r.Submit(id, "f.pdf", "223-ФЗ")
	if a := waitStatus(t, repos, id); a.Status != "completed" || a.RedCount != 0 {
		t.Fatalf("%+v", a)
	}
	r.Shutdown(context.Background())
}

// Много параллельных проверок + одновременное чтение: запускать с -race.
func TestRunnerConcurrent(t *testing.T) {
	t.Setenv("TENDERSCOPE_TMP_DIR", t.TempDir())
	repos := newRepos(t)
	r := New(repos)
	var mu sync.Mutex
	running, maxRunning := 0, 0
	r.exec = func(_ context.Context, _, _, out string) error {
		mu.Lock()
		running++
		if running > maxRunning {
			maxRunning = running
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return os.WriteFile(out, []byte(okResult), 0o644)
	}
	const n = 12
	ids := make([]string, n)
	for i := range ids {
		ids[i] = newAudit(t, repos)
		if err := r.Submit(ids[i], "f.pdf", "44-ФЗ"); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) { // читатели параллельно с записью воркеров
			defer wg.Done()
			for {
				a, err := repos.Audits.GetByID(context.Background(), id)
				if err != nil {
					t.Error(err)
					return
				}
				items, _ := repos.RiskItems.ListByAudit(context.Background(), id)
				if a.Status == "completed" && len(items) != 3 {
					t.Errorf("completed без рисков: %d", len(items))
				}
				if a.Status != "processing" {
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}(id)
	}
	wg.Wait()
	for _, id := range ids {
		if a, _ := repos.Audits.GetByID(context.Background(), id); a.Status != "completed" || a.RedCount != 1 {
			t.Fatalf("%s: %+v", id, a)
		}
	}
	if maxRunning > maxParallelJobs {
		t.Fatalf("одновременно %d задач, лимит %d", maxRunning, maxParallelJobs)
	}
	r.Shutdown(context.Background())
}

func TestShutdownCancelsAndRejects(t *testing.T) {
	t.Setenv("TENDERSCOPE_TMP_DIR", t.TempDir())
	repos := newRepos(t)
	r := New(repos)
	started := make(chan struct{})
	r.exec = func(ctx context.Context, _, _, _ string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	id := newAudit(t, repos)
	r.Submit(id, "f.pdf", "44-ФЗ")
	<-started
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Shutdown(sctx); err != nil {
		t.Fatal(err)
	}
	if a, _ := repos.Audits.GetByID(context.Background(), id); a.Status != "failed" {
		t.Fatalf("после остановки ожидался failed: %+v", a)
	}
	if err := r.Submit(id, "f.pdf", "44-ФЗ"); !errors.Is(err, ErrClosed) {
		t.Fatalf("ожидался ErrClosed, получено %v", err)
	}
}

func TestParseResultShapes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "r.json")
	os.WriteFile(p, []byte(`[{"category":"payment","level":"red","title":"t","description":"d"}]`), 0o644)
	if items, err := parseResult(p); err != nil || len(items) != 1 {
		t.Fatalf("массив: %v %v", items, err)
	}
	if _, err := parseResult(filepath.Join(dir, "nope.json")); err == nil || !strings.Contains(err.Error(), "не создал") {
		t.Fatalf("ожидалась ошибка про отсутствие файла: %v", err)
	}
}
