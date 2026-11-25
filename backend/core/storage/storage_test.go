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
func TestCompleteAndFailStale(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := NewSQLiteRepositories(db)
	ctx := context.Background()

	tender := &Tender{ID: NewID(), TenderNumber: "", Title: "f.pdf", Customer: "", LawType: "44-ФЗ"}
	audit := &Audit{ID: NewID(), TenderID: tender.ID, Status: "processing"}
	if err := repos.Audits.CreateWithTender(ctx, tender, audit); err != nil {
		t.Fatal(err)
	}
	items := []RiskItem{
		{Category: "payment", Level: "red", Title: "a", Description: "d"},
		{Category: "delivery", Level: "yellow", Title: "b", Description: "d"},
		{Category: "termination", Level: "green", Title: "c", Description: "d"},
		{Category: "fines", Level: "red", Title: "d", Description: "d"},
	}
	if err := repos.Audits.Complete(ctx, audit.ID, items); err != nil {
		t.Fatal(err)
	}
	a, _ := repos.Audits.GetByID(ctx, audit.ID)
	if a.Status != "completed" || a.RedCount != 2 || a.YellowCount != 1 || a.GreenCount != 1 {
		t.Fatalf("audit after complete: %+v", a)
	}
	got, _ := repos.RiskItems.ListByAudit(ctx, audit.ID)
	if len(got) != 4 || got[0].Title != "a" || got[3].Title != "d" {
		t.Fatalf("risk items: %+v", got)
	}

	// откат: плохой level -> ничего не сохраняется, статус не меняется
	audit2 := &Audit{ID: NewID(), TenderID: tender.ID, Status: "processing"}
	if err := repos.Audits.Create(ctx, audit2); err != nil {
		t.Fatal(err)
	}
	bad := []RiskItem{{Category: "c", Level: "red", Title: "ok", Description: "d"}, {Category: "c", Level: "blue", Title: "bad", Description: "d"}}
	if err := repos.Audits.Complete(ctx, audit2.ID, bad); err == nil {
		t.Fatal("ожидалась ошибка CHECK level")
	}
	if items, _ := repos.RiskItems.ListByAudit(ctx, audit2.ID); len(items) != 0 {
		t.Fatalf("транзакция не откатилась: %+v", items)
	}
	if n, err := repos.Audits.FailStale(ctx); err != nil || n != 1 {
		t.Fatalf("FailStale: n=%d err=%v", n, err)
	}
	if a, _ := repos.Audits.GetByID(ctx, audit2.ID); a.Status != "failed" {
		t.Fatalf("ожидался failed, а не %s", a.Status)
	}
	if a, _ := repos.Audits.GetByID(ctx, audit.ID); a.Status != "completed" {
		t.Fatalf("completed не должен меняться: %s", a.Status)
	}
}
