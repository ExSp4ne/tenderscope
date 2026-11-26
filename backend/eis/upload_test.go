package eis

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func doUpload(t *testing.T, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	UploadHandler(rec, req)
	return rec
}

func uploadedFiles(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return entries
}

func TestUploadHandler_ValidDocx(t *testing.T) {
	_, up := setupEnv(t)
	rec := doUpload(t, "contract.docx", fakeDocx(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	p := resp["path"]
	if filepath.Ext(p) != ".docx" || filepath.Dir(filepath.FromSlash(p)) != up {
		t.Errorf("неожиданный path %q (UploadDir=%s)", p, up)
	}
	if !fileExists(filepath.FromSlash(p)) {
		t.Error("файл не сохранён")
	}
}

func TestUploadHandler_ValidPDF(t *testing.T) {
	setupEnv(t)
	if rec := doUpload(t, "contract.PDF", []byte("%PDF-1.7\n...")); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestUploadHandler_TooLarge(t *testing.T) {
	_, up := setupEnv(t)
	big := append([]byte("PK\x03\x04"), bytes.Repeat([]byte("a"), MaxUploadSize+1)...)
	rec := doUpload(t, "big.docx", big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413: %s", rec.Code, rec.Body)
	}
	if n := len(uploadedFiles(t, up)); n != 0 {
		t.Errorf("слишком большой файл не должен сохраняться, файлов: %d", n)
	}
}

func TestUploadHandler_RenamedExe(t *testing.T) {
	_, up := setupEnv(t)
	exe := append([]byte("MZ\x90\x00"), bytes.Repeat([]byte{0}, 1024)...)
	for _, name := range []string{"virus.docx", "virus.pdf"} {
		rec := doUpload(t, name, exe)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
	if n := len(uploadedFiles(t, up)); n != 0 {
		t.Errorf("подделка не должна сохраняться, файлов: %d", n)
	}
}

func TestUploadHandler_WrongExtension(t *testing.T) {
	setupEnv(t)
	if rec := doUpload(t, "run.exe", []byte("MZ\x90\x00")); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
}

func TestUploadHandler_WrongMethodAndMissingField(t *testing.T) {
	setupEnv(t)
	rec := httptest.NewRecorder()
	UploadHandler(rec, httptest.NewRequest(http.MethodGet, "/api/upload", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d, want 405", rec.Code)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("other", "x")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec = httptest.NewRecorder()
	UploadHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("без поля file: status %d, want 400", rec.Code)
	}
}
