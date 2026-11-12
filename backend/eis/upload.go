package eis

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	UploadDir     = "uploads"
	MaxUploadSize = 30 << 20 // 30 МБ
)

// UploadHandler — POST /api/upload (multipart/form-data, поле file)
func UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "нужен метод POST"})
		return
	}

	// небольшой запас сверху на служебные части multipart
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize+(1<<20))
	if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "файл больше 30 МБ"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ожидается multipart/form-data"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нет поля file"})
		return
	}
	defer file.Close()

	if header.Size > MaxUploadSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "файл больше 30 МБ"})
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".pdf" && ext != ".docx" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "разрешены только .pdf и .docx"})
		return
	}

	// Проверка «магических» байтов: pdf начинается с %PDF, docx (zip) - с PK
	head := make([]byte, 4)
	if _, err := io.ReadFull(file, head); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "файл пустой или повреждён"})
		return
	}
	if (ext == ".pdf" && string(head) != "%PDF") || (ext == ".docx" && string(head[:2]) != "PK") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "содержимое не соответствует расширению"})
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ошибка чтения файла"})
		return
	}

	if err := os.MkdirAll(UploadDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось создать папку"})
		return
	}
	// Имя генерируем сами - имя от пользователя в путь не попадает
	rnd := make([]byte, 4)
	rand.Read(rnd)
	path := filepath.Join(UploadDir, fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), hex.EncodeToString(rnd), ext))

	dst, err := os.Create(path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось сохранить файл"})
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось сохранить файл"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"path": filepath.ToSlash(path)})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
