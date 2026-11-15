package storage

import (
	"context"
	"database/sql"
	"time"
)

type Tender struct {
	ID           string    `json:"id"`
	TenderNumber string    `json:"tender_number"`
	Title        string    `json:"title"`
	Customer     string    `json:"customer"`
	Price        float64   `json:"price"`
	LawType      string    `json:"law_type"`
	FilePath     *string   `json:"file_path,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Audit struct {
	ID          string    `json:"id"`
	TenderID    string    `json:"tender_id"`
	Status      string    `json:"status"`
	RedCount    int       `json:"red_count"`
	YellowCount int       `json:"yellow_count"`
	GreenCount  int       `json:"green_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type RiskItem struct {
	ID             string  `json:"id"`
	AuditID        string  `json:"audit_id,omitempty"`
	Category       string  `json:"category"`
	Level          string  `json:"level"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	Recommendation *string `json:"recommendation,omitempty"`
	Quote          *string `json:"quote,omitempty"`
	PageNumber     *int    `json:"page_number,omitempty"`
	ClauseNumber   *string `json:"clause_number,omitempty"`
}

type TenderRepository interface {
	Create(ctx context.Context, t *Tender) error
	GetByID(ctx context.Context, id string) (*Tender, error)
}

type AuditRepository interface {
	Create(ctx context.Context, a *Audit) error
	GetByID(ctx context.Context, id string) (*Audit, error)
	UpdateStatus(ctx context.Context, id, status string) error
}

type RiskItemRepository interface {
	Create(ctx context.Context, r *RiskItem) error
	ListByAudit(ctx context.Context, auditID string) ([]RiskItem, error)
}

type Repositories struct {
	Tenders   TenderRepository
	Audits    AuditRepository
	RiskItems RiskItemRepository
}

func NewSQLiteRepositories(db *sql.DB) *Repositories {
	return &Repositories{
		Tenders:   &sqlTenders{db},
		Audits:    &sqlAudits{db},
		RiskItems: &sqlRiskItems{db},
	}
}

type sqlTenders struct{ db *sql.DB }

func (r *sqlTenders) Create(ctx context.Context, t *Tender) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tenders (id, tender_number, title, customer, price, law_type, file_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.TenderNumber, t.Title, t.Customer, t.Price, t.LawType, t.FilePath)
	return err
}

func (r *sqlTenders) GetByID(ctx context.Context, id string) (*Tender, error) {
	t := &Tender{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, tender_number, title, customer, price, law_type, file_path, created_at
		 FROM tenders WHERE id = ?`, id).
		Scan(&t.ID, &t.TenderNumber, &t.Title, &t.Customer, &t.Price, &t.LawType, &t.FilePath, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

type sqlAudits struct{ db *sql.DB }

func (r *sqlAudits) Create(ctx context.Context, a *Audit) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO audits (id, tender_id, status, red_count, yellow_count, green_count)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.TenderID, a.Status, a.RedCount, a.YellowCount, a.GreenCount)
	return err
}

func (r *sqlAudits) GetByID(ctx context.Context, id string) (*Audit, error) {
	a := &Audit{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, tender_id, status, red_count, yellow_count, green_count, created_at
		 FROM audits WHERE id = ?`, id).
		Scan(&a.ID, &a.TenderID, &a.Status, &a.RedCount, &a.YellowCount, &a.GreenCount, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *sqlAudits) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE audits SET status = ? WHERE id = ?`, status, id)
	return err
}

type sqlRiskItems struct{ db *sql.DB }

func (r *sqlRiskItems) Create(ctx context.Context, ri *RiskItem) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO risk_items (id, audit_id, category, level, title, description, recommendation, quote, page_number, clause_number)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ri.ID, ri.AuditID, ri.Category, ri.Level, ri.Title, ri.Description,
		ri.Recommendation, ri.Quote, ri.PageNumber, ri.ClauseNumber)
	return err
}

func (r *sqlRiskItems) ListByAudit(ctx context.Context, auditID string) ([]RiskItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, audit_id, category, level, title, description, recommendation, quote, page_number, clause_number
		 FROM risk_items WHERE audit_id = ?`, auditID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RiskItem{}
	for rows.Next() {
		var ri RiskItem
		if err := rows.Scan(&ri.ID, &ri.AuditID, &ri.Category, &ri.Level, &ri.Title, &ri.Description,
			&ri.Recommendation, &ri.Quote, &ri.PageNumber, &ri.ClauseNumber); err != nil {
			return nil, err
		}
		out = append(out, ri)
	}
	return out, rows.Err()
}