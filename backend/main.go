package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"tenderscope/backend/core/handlers"
	"tenderscope/backend/core/storage"
	"tenderscope/backend/core/worker"
	"tenderscope/backend/eis"

	"github.com/go-chi/chi/v5"
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

// startCleanup запускает фоновую очистку и корректно останавливается вместе с сервером.
func startCleanup(ctx context.Context) {
	run := func() {
		if err := eis.CleanupOldFiles(cleanupMaxAge); err != nil {
			log.Printf("cleanup: %v", err)
		}
	}
	run() // первый прогон сразу при старте
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
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
	repos := storage.NewSQLiteRepositories(db)

	// Фоновые задачи не переживают перезапуск: зависшие processing переводим в failed.
	if n, err := repos.Audits.FailStale(context.Background()); err != nil {
		log.Printf("fail stale audits: %v", err)
	} else if n > 0 {
		log.Printf("помечено failed незавершённых проверок: %d", n)
	}

	runner := worker.New(repos)
	audits := handlers.NewAudits(repos, runner)

	// Корневой контекст завершения сервера (Ctrl+C / SIGTERM).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Автоочистка временных файлов EIS — останавливается вместе с сервером.
	startCleanup(ctx)

	r := chi.NewRouter()
	r.Use(cors)
	r.Get("/api/audits/{id}", audits.Get)
	r.Post("/api/audits", audits.Create)
	r.Post("/api/upload", eis.UploadHandler)

	srv := &http.Server{Addr: "localhost:8080", Handler: r, ReadHeaderTimeout: 10 * time.Second}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		log.Printf("останавливаюсь...")
		sctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if err := srv.Shutdown(sctx); err != nil {
			log.Printf("http shutdown: %v", err)
		}
		if err := runner.Shutdown(sctx); err != nil {
			log.Printf("worker shutdown: %v", err)
		}
	}()

	log.Printf("listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownDone // дождаться остановки воркера до закрытия БД (defer db.Close)
}