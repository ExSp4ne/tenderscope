package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"tenderscope/backend/core/storage"
	"tenderscope/backend/eis"
)

type createAuditRequest struct {
	TenderNumber string `json:"tenderNumber"`
	FilePath     string `json:"filePath"`
	LawType      string `json:"lawType"`
}

var reTenderNumber = regexp.MustCompile(`^(\d{19}|\d{11})$`)

// Create — POST /api/audits.
// Тело: {"tenderNumber": "..."} ИЛИ {"filePath": "...", "lawType": "44-ФЗ"}.
// Создаёт закупку и проверку (processing), запускает фоновый анализ и сразу отвечает 202.
func (h *Audits) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req createAuditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ожидается JSON вида {\"tenderNumber\": \"...\"} или {\"filePath\": \"...\", \"lawType\": \"44-ФЗ\"}"})
		return
	}
	req.TenderNumber = strings.TrimSpace(req.TenderNumber)
	req.FilePath = strings.TrimSpace(req.FilePath)
	req.LawType = strings.TrimSpace(req.LawType)

	if (req.TenderNumber == "") == (req.FilePath == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "укажите ровно одно из полей: tenderNumber или filePath"})
		return
	}

	tender := &storage.Tender{ID: storage.NewID()}
	var filePath string

	if req.TenderNumber != "" {
		if !reTenderNumber.MatchString(req.TenderNumber) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный номер закупки: нужно 19 цифр (44-ФЗ) или 11 цифр (223-ФЗ)"})
			return
		}
		path, meta, err := h.Download(req.TenderNumber)
		if err != nil {
			log.Printf("download tender %s: %v", req.TenderNumber, err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		filePath = path
		tender.TenderNumber = req.TenderNumber
		tender.Title = meta.Title
		tender.Customer = meta.Customer
		tender.Price = meta.Price
		tender.LawType = meta.Law
	} else {
		if req.LawType != "44-ФЗ" && req.LawType != "223-ФЗ" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "lawType должен быть \"44-ФЗ\" или \"223-ФЗ\" (кириллицей)"})
			return
		}
		path, err := resolveUploadedFile(req.FilePath)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		filePath = path
		tender.Title = filepath.Base(path) // метаданных ЕИС у загруженного файла нет
		tender.LawType = req.LawType
	}
	tender.FilePath = &filePath

	audit := &storage.Audit{ID: storage.NewID(), TenderID: tender.ID, Status: "processing"}
	if err := h.Repos.Audits.CreateWithTender(r.Context(), tender, audit); err != nil {
		log.Printf("create audit: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Записи уже в БД — теперь можно запускать фон (воркер сам переведёт статус).
	if err := h.Runner.Submit(audit.ID, filePath, tender.LawType); err != nil {
		log.Printf("submit audit %s: %v", audit.ID, err)
		if uerr := h.Repos.Audits.UpdateStatus(r.Context(), audit.ID, "failed"); uerr != nil {
			log.Printf("mark audit %s failed: %v", audit.ID, uerr)
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "сервис останавливается, повторите запрос позже"})
		return
	}

	w.Header().Set("Location", "/api/audits/"+audit.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": audit.ID, "status": "processing"})
}

// resolveUploadedFile разрешает только существующие .pdf/.docx внутри папок загрузок и скачанных
// закупок: иначе через filePath можно было бы отдать анализатору любой файл сервера.
func resolveUploadedFile(p string) (string, error) {
	abs, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		return "", errors.New("некорректный путь к файлу")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", errors.New("файл не найден (сначала загрузите его через POST /api/upload)")
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.Mode().IsRegular() {
		return "", errors.New("файл не найден (сначала загрузите его через POST /api/upload)")
	}
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext != ".pdf" && ext != ".docx" {
		return "", errors.New("разрешены только .pdf и .docx")
	}
	for _, root := range []string{eis.UploadDir(), eis.TmpDir()} {
		rootReal, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rootReal, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return resolved, nil
		}
	}
	return "", errors.New("файл должен быть загружен через POST /api/upload")
}
