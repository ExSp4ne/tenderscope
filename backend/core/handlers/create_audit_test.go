package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"tenderscope/backend/core/storage"
	"tenderscope/backend/core/worker"
	"tenderscope/backend/eis"
)

type submitCall struct{ id, file, law string }

type fakeRunner struct {
	mu    sync.Mutex
	calls []submitCall
	err   error
}

func (f *fakeRunner) Submit(id, file, law string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, submitCall{id, file, law})
	return f.err
}

func setup(t *testing.T) (*Audits, *fakeRunner, http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TENDERSCOPE_UPLOAD_DIR", filepath.Join(root, "uploads"))
	t.Setenv("TENDERSCOPE_TMP_DIR", filepath.Join(root, "tmp"))
	os.MkdirAll(filepath.Join(root, "uploads"), 0o755)
	os.MkdirAll(filepath.Join(root, "tmp"), 0o755)

	db, err := storage.Open(filepath.Join(root, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{}
	h := NewAudits(storage.NewSQLiteRepositories(db), fr)
	r := chi.NewRouter()
	r.Get("/api/audits/{id}", h.Get)
	r.Post("/api/audits", h.Create)
	return h, fr, r, root
}

func post(r http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/audits", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCreateByFilePath(t *testing.T) {
	_, fr, r, root := setup(t)
	file := filepath.Join(root, "uploads", "c.pdf")
	os.WriteFile(file, []byte("%PDF-1.4"), 0o644)

	rec := post(r, `{"filePath":"`+filepath.ToSlash(file)+`","lawType":"44-ФЗ"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("код %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "processing" || resp["id"] == "" {
		t.Fatalf("ответ: %v", resp)
	}
	if len(fr.calls) != 1 || fr.calls[0].id != resp["id"] || fr.calls[0].law != "44-ФЗ" {
		t.Fatalf("Submit: %+v", fr.calls)
	}

	// GET сразу после POST: processing, пустые риски (не null)
	req := httptest.NewRequest(http.MethodGet, "/api/audits/"+resp["id"], nil)
	g := httptest.NewRecorder()
	r.ServeHTTP(g, req)
	if g.Code != 200 || !strings.Contains(g.Body.String(), `"status":"processing"`) || !strings.Contains(g.Body.String(), `"risks":[]`) || !strings.Contains(g.Body.String(), `"lawType":"44-ФЗ"`) {
		t.Fatalf("GET: %d %s", g.Code, g.Body)
	}
}

func TestCreateByTenderNumber(t *testing.T) {
	h, fr, r, root := setup(t)
	dl := filepath.Join(root, "tmp", "0373200009324000123", "contract.docx")
	os.MkdirAll(filepath.Dir(dl), 0o755)
	os.WriteFile(dl, []byte("PK"), 0o644)
	h.Download = func(n string) (string, eis.TenderMeta, error) {
		return dl, eis.TenderMeta{Title: "Поставка", Customer: `ГБУ "Гормост"`, Price: 14500000, Law: "44-ФЗ"}, nil
	}
	rec := post(r, `{"tenderNumber":"0373200009324000123"}`)
	if rec.Code != 202 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if fr.calls[0].file != dl || fr.calls[0].law != "44-ФЗ" {
		t.Fatalf("Submit: %+v", fr.calls)
	}
	g := httptest.NewRecorder()
	r.ServeHTTP(g, httptest.NewRequest(http.MethodGet, "/api/audits/"+resp["id"], nil))
	var got auditResponse
	json.Unmarshal(g.Body.Bytes(), &got)
	if got.TenderNumber != "0373200009324000123" || got.Customer != `ГБУ "Гормост"` || got.Price != 14500000 || got.LawType != "44-ФЗ" {
		t.Fatalf("GET: %+v", got)
	}

	h.Download = func(string) (string, eis.TenderMeta, error) {
		return "", eis.TenderMeta{}, errors.New("закупка не найдена")
	}
	if rec := post(r, `{"tenderNumber":"0373200009324000123"}`); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "не найдена") {
		t.Fatalf("ожидался 502: %d %s", rec.Code, rec.Body)
	}
}

func TestCreateValidation(t *testing.T) {
	_, fr, r, root := setup(t)
	outside := filepath.Join(root, "secret.pdf")
	os.WriteFile(outside, []byte("%PDF"), 0o644)
	txt := filepath.Join(root, "uploads", "a.txt")
	os.WriteFile(txt, []byte("x"), 0o644)
	// симлинк из uploads наружу
	link := filepath.Join(root, "uploads", "link.pdf")
	_ = os.Symlink(outside, link)

	cases := map[string]string{
		"битый json":       `{`,
		"пусто":            `{}`,
		"оба поля":         `{"tenderNumber":"0373200009324000123","filePath":"x"}`,
		"плохой номер":     `{"tenderNumber":"123"}`,
		"нет lawType":      `{"filePath":"` + filepath.ToSlash(outside) + `"}`,
		"latin lawType":    `{"filePath":"` + filepath.ToSlash(outside) + `","lawType":"44-FZ"}`,
		"файл вне uploads": `{"filePath":"` + filepath.ToSlash(outside) + `","lawType":"44-ФЗ"}`,
		"обход через ..":   `{"filePath":"` + filepath.ToSlash(filepath.Join(root, "uploads", "..", "secret.pdf")) + `","lawType":"44-ФЗ"}`,
		"не существует":    `{"filePath":"` + filepath.ToSlash(filepath.Join(root, "uploads", "no.pdf")) + `","lawType":"44-ФЗ"}`,
		"не pdf/docx":      `{"filePath":"` + filepath.ToSlash(txt) + `","lawType":"44-ФЗ"}`,
		"симлинк наружу":   `{"filePath":"` + filepath.ToSlash(link) + `","lawType":"44-ФЗ"}`,
	}
	for name, body := range cases {
		if rec := post(r, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: ожидался 400, получен %d: %s", name, rec.Code, rec.Body)
		}
	}
	if len(fr.calls) != 0 {
		t.Fatalf("Submit не должен вызываться: %+v", fr.calls)
	}
}

func TestSubmitFailureMarksFailed(t *testing.T) {
	h, fr, r, root := setup(t)
	fr.err = worker.ErrClosed
	file := filepath.Join(root, "uploads", "c.docx")
	os.WriteFile(file, []byte("PK"), 0o644)
	rec := post(r, `{"filePath":"`+filepath.ToSlash(file)+`","lawType":"223-ФЗ"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("код %d", rec.Code)
	}
	a, err := h.Repos.Audits.GetByID(context.Background(), fr.calls[0].id)
	if err != nil || a.Status != "failed" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestGetDemoAndNotFound(t *testing.T) {
	_, _, r, _ := setup(t)
	g := httptest.NewRecorder()
	r.ServeHTTP(g, httptest.NewRequest(http.MethodGet, "/api/audits/demo-1", nil))
	if g.Code != 200 || !bytes.Contains(g.Body.Bytes(), []byte(`"id":"demo-1"`)) {
		t.Fatalf("demo: %d", g.Code)
	}
	g = httptest.NewRecorder()
	r.ServeHTTP(g, httptest.NewRequest(http.MethodGet, "/api/audits/unknown", nil))
	if g.Code != 404 || !strings.Contains(g.Body.String(), "audit not found") {
		t.Fatalf("404: %d %s", g.Code, g.Body)
	}
}
