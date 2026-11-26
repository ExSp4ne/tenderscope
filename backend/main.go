package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"tenderscope/backend/core/handlers"
	"tenderscope/backend/core/storage"
	"tenderscope/backend/eis"
)

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Как часто чистим временные файлы и файлы старше какого возраста удаляем
const (
	cleanupInterval = time.Hour
	cleanupMaxAge   = 24 * time.Hour
)

func startCleanup() {
	run := func() {
		if err := eis.CleanupOldFiles(cleanupMaxAge); err != nil {
			log.Printf("cleanup: %v", err)
		}
	}
	run()
	go func() {
		for range time.Tick(cleanupInterval) {
			run()
		}
	}()
}

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "core/storage/data.db"
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	_ = storage.NewSQLiteRepositories(db)
	startCleanup()

	r := chi.NewRouter()
	r.Use(cors)
	r.Get("/api/audits/{id}", handlers.GetAuditByID)
	r.Post("/api/upload", eis.UploadHandler)

	addr := "localhost:8080"
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}