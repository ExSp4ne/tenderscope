// Package storage инициализирует БД и применяет миграции.
// Репозитории работают через database/sql и интерфейсы (см. repository.go),
// поэтому замена SQLite на PostgreSQL сводится к смене драйвера/DSN в Open.
package storage

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3" // драйвер SQLite (CGO)
)

const schema = `
CREATE TABLE IF NOT EXISTS tenders (
    id            TEXT PRIMARY KEY,
    tender_number TEXT NOT NULL,
    title         TEXT NOT NULL,
    customer      TEXT NOT NULL,
    price         NUMERIC NOT NULL,
    law_type      TEXT NOT NULL,
    file_path     TEXT,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS audits (
    id            TEXT PRIMARY KEY,
    tender_id     TEXT NOT NULL REFERENCES tenders(id),
    status        TEXT NOT NULL CHECK (status IN ('created','processing','completed','failed')),
    red_count     INTEGER NOT NULL DEFAULT 0,
    yellow_count  INTEGER NOT NULL DEFAULT 0,
    green_count   INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS risk_items (
    id              TEXT PRIMARY KEY,
    audit_id        TEXT NOT NULL REFERENCES audits(id),
    category        TEXT NOT NULL,
    level           TEXT NOT NULL CHECK (level IN ('red','yellow','green')),
    title           TEXT NOT NULL,
    description     TEXT NOT NULL,
    recommendation  TEXT,
    quote           TEXT,
    page_number     INTEGER,
    clause_number   TEXT
);
`

// Open открывает SQLite-файл по пути path.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", path))
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

// Migrate создаёт таблицы, если их ещё нет.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
}
