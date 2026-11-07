package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
	"github.com/getcortexlabs/ranjit/internal/statement"
)

func csvInput(t *testing.T, data string) statement.Statement {
	t.Helper()
	s, err := statement.Parse(strings.NewReader(data), statement.Options{Format: "csv", Account: "pessoal"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestImportAtomicDeduplicationAndUnknownBalance(t *testing.T) {
	db, _, _ := fixture(t)
	input := csvInput(t, "date,description,amount\n2026-11-06,Café,-1.23\n2026-11-06,Café,-1.23\n")
	result, err := db.Import(t.Context(), "personal", "pessoal", input, testNow)
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.Accounts(t.Context(), result.ConnectionID, "", 100, testNow)
	if err != nil || len(page.Data) != 1 || page.Data[0].Balance != nil {
		t.Fatalf("unknown balance: %+v %v", page, err)
	}
	second, err := db.Import(t.Context(), "personal", "pessoal", input, testNow)
	if err != nil || !second.Duplicate {
		t.Fatalf("exact duplicate: %+v %v", second, err)
	}
	overlap := csvInput(t, "date,description,amount\n2026-11-06,Café,-1.23\n2026-11-07,Outro,-2\n")
	if _, err = db.Import(t.Context(), "personal", "pessoal", overlap, testNow); !errors.Is(err, ranjit.ErrInvalid) {
		t.Fatalf("ambiguous overlap: %v", err)
	}
	status, err := db.Status(t.Context(), result.ConnectionID, testNow)
	if err != nil || status.Transactions != 2 {
		t.Fatalf("duplicate legitimate rows lost or partial import: %+v %v", status, err)
	}
	if _, err = db.Enqueue(t.Context(), result.ConnectionID, testNow); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatalf("file scheduled: %v", err)
	}
	if err = db.Expire(t.Context(), testNow.AddDate(1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Accounts(t.Context(), result.ConnectionID, "", 100, testNow.AddDate(1, 0, 0)); err != nil {
		t.Fatal("file data expired", err)
	}
	if err = db.Revoke(t.Context(), result.ConnectionID, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Import(t.Context(), "personal", "pessoal", input, testNow); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatal("revoked connection reused", err)
	}
}

func TestStableIDsReconcileAndImportRollback(t *testing.T) {
	db, _, _ := fixture(t)
	input := csvInput(t, "id,date,description,amount\na,2026-11-06,Café,-1.23\n")
	result, err := db.Import(t.Context(), "personal", "pessoal", input, testNow)
	if err != nil {
		t.Fatal(err)
	}
	input = csvInput(t, "id,date,description,amount\na,2026-11-06,Café corrigido,-1.22\nb,2026-11-07,Outro,3\n")
	if _, err = db.Import(t.Context(), "personal", "pessoal", input, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec(`CREATE TRIGGER reject_import BEFORE INSERT ON transactions WHEN NEW.description='rejected' BEGIN SELECT RAISE(ABORT,'test'); END`); err != nil {
		t.Fatal(err)
	}
	bad := csvInput(t, "id,date,description,amount\nc,2026-11-07,valid,1\nd,2026-11-07,rejected,2\n")
	if _, err = db.Import(t.Context(), "personal", "pessoal", bad, testNow); err == nil {
		t.Fatal("expected rollback")
	}
	status, err := db.Status(t.Context(), result.ConnectionID, testNow)
	if err != nil || status.Transactions != 2 {
		t.Fatalf("partial import: %+v %v", status, err)
	}
	var count int
	if err = db.db.QueryRow(`SELECT COUNT(*) FROM imports WHERE connection_id=?`, result.ConnectionID).Scan(&count); err != nil || count != 2 {
		t.Fatal("failed import recorded", count, err)
	}
}

func TestCheckpointFencedRetainedAndDeleted(t *testing.T) {
	db, c, _ := fixture(t)
	j := claim(t, db, c, testNow)
	now := testNow
	cp := Checkpoint{Store: db, Job: j, Now: func() time.Time { return now }}
	if err := cp.Save(t.Context(), "https://bank.example/private?account=123", []byte(`{"balance":123}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.Fail(t.Context(), j, "provider_unavailable", now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	next, err := db.Claim(t.Context(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = cp.Save(t.Context(), "https://bank.example/old", []byte(`{}`)); !errors.Is(err, ranjit.ErrLease) {
		t.Fatalf("old lease wrote checkpoint: %v", err)
	}
	cp.Job = next
	body, err := cp.Load(t.Context(), "https://bank.example/private?account=123")
	if err != nil || string(body) != `{"balance":123}` {
		t.Fatalf("checkpoint lost: %s %v", body, err)
	}
	batch, err := (sandbox.Source{}).Fetch(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Complete(t.Context(), next, batch, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.db.QueryRow(`SELECT COUNT(*) FROM sync_responses`).Scan(&count); err != nil || count != 0 {
		t.Fatal("responses retained after completion", count, err)
	}
	j = claim(t, db, c, now)
	cp.Job = j
	if err = cp.Save(t.Context(), "https://bank.example/new", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err = db.Revoke(t.Context(), c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err = db.db.QueryRow(`SELECT COUNT(*) FROM sync_responses`).Scan(&count); err != nil || count != 0 {
		t.Fatal("responses retained after revocation", count, err)
	}
}
