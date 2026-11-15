package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRepositoriesRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := NewSQLiteRepositories(db)
	ctx := context.Background()
	if err := repos.Tenders.Create(ctx, &Tender{ID: "t1", TenderNumber: "1", Title: "T", Customer: "C", Price: 1234.5, LawType: "44-ФЗ"}); err != nil {
		t.Fatal(err)
	}
	if tn, err := repos.Tenders.GetByID(ctx, "t1"); err != nil || tn.Price != 1234.5 {
		t.Fatalf("tender: %v %+v", err, tn)
	}
	if err := repos.Audits.Create(ctx, &Audit{ID: "a1", TenderID: "t1", Status: "created"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Audits.UpdateStatus(ctx, "a1", "completed"); err != nil {
		t.Fatal(err)
	}
	if a, err := repos.Audits.GetByID(ctx, "a1"); err != nil || a.Status != "completed" {
		t.Fatalf("audit: %v %+v", err, a)
	}
	if err := repos.Audits.Create(ctx, &Audit{ID: "a2", TenderID: "nope", Status: "created"}); err == nil {
		t.Fatal("ожидалась ошибка FK")
	}
	if err := repos.Audits.Create(ctx, &Audit{ID: "a3", TenderID: "t1", Status: "bad"}); err == nil {
		t.Fatal("ожидалась ошибка CHECK status")
	}
	q := "цитата"
	if err := repos.RiskItems.Create(ctx, &RiskItem{ID: "r1", AuditID: "a1", Category: "c", Level: "red", Title: "t", Description: "d", Quote: &q}); err != nil {
		t.Fatal(err)
	}
	if err := repos.RiskItems.Create(ctx, &RiskItem{ID: "r2", AuditID: "a1", Category: "c", Level: "blue", Title: "t", Description: "d"}); err == nil {
		t.Fatal("ожидалась ошибка CHECK level")
	}
	items, err := repos.RiskItems.ListByAudit(ctx, "a1")
	if err != nil || len(items) != 1 || items[0].Quote == nil || *items[0].Quote != q {
		t.Fatalf("risk items: %v %+v", err, items)
	}
}