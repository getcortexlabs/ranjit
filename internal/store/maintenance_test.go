package store

import (
	"errors"
	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupRestoreLockRollbackAndIntegrity(t *testing.T) {
	db, _, path := fixture(t)
	ctx := t.Context()
	input := csvInput(t, "id,date,description,amount\na,2026-11-06,Café,-1.23\n")
	imported, err := db.Import(ctx, "personal", "pessoal", input, testNow)
	if err != nil {
		t.Fatal(err)
	}
	copy := filepath.Join(t.TempDir(), "copy.db")
	if err = db.Backup(ctx, copy); err != nil {
		t.Fatal(err)
	}
	if err = db.Backup(ctx, copy); err == nil {
		t.Fatal("overwrote backup")
	}
	if _, err = Restore(ctx, copy, path, true); err == nil {
		t.Fatal("restored while database in use")
	}
	input = csvInput(t, "id,date,description,amount\nb,2026-11-07,Outro,3\n")
	if _, err = db.Import(ctx, "personal", "pessoal", input, testNow); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err = Restore(ctx, copy, path, false); err == nil {
		t.Fatal("replaced without flag")
	}
	corrupt := filepath.Join(t.TempDir(), "broken.db")
	os.WriteFile(corrupt, []byte("not a database"), 0600)
	if _, err = Restore(ctx, corrupt, path, true); err == nil {
		t.Fatal("restored corrupt input")
	}
	rollback, err := Restore(ctx, copy, path, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path  string
		count int
	}{{path, 1}, {rollback, 2}} {
		restored, err := Open(ctx, item.path)
		if err != nil {
			t.Fatal(err)
		}
		status, err := restored.Status(ctx, imported.ConnectionID, testNow)
		restored.Close()
		if err != nil || status.Transactions != item.count {
			t.Fatalf("restore lost history: %+v %v", status, err)
		}
	}
	// A fresh destination works too; imported statements retain their identity.
	target := filepath.Join(t.TempDir(), "fresh.db")
	if _, err = Restore(ctx, copy, target, false); err != nil {
		t.Fatal(err)
	}
}

func TestDirectHistoryRetainedPauseFencesAndRestorePauses(t *testing.T) {
	db, c, _ := fixture(t)
	db.EnableWebhooks("https://backend.example/events")
	ctx := t.Context()
	batch, _ := (sandbox.Source{}).Fetch(ctx, c)
	c.ID = ranjit.NewID("con")
	c.InstitutionID = "efi"
	c.Provider = "efi"
	c.AuthorizationMode = "api_credentials"
	c.CredentialID = "binding"
	if err := db.CreateAuthorizedConnection(ctx, c); err != nil {
		t.Fatal(err)
	}
	j := claim(t, db, c, testNow)
	if err := db.Complete(ctx, j, batch, testNow); err != nil {
		t.Fatal(err)
	}
	future := testNow.Add(2 * time.Hour)
	if err := db.Expire(ctx, future); err != nil {
		t.Fatal(err)
	}
	if a, err := db.Accounts(ctx, c.ID, "", 100, future); err != nil || len(a.Data) != 2 {
		t.Fatalf("expired history: %+v %v", a, err)
	}
	if _, err := db.Enqueue(ctx, c.ID, future); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatal("expired grant synced", err)
	}
	if _, err := db.Renew(ctx, c.ID, "wrong", future.Add(time.Hour), future); err == nil {
		t.Fatal("renewed wrong binding")
	}
	if _, err := db.Renew(ctx, c.ID, c.CredentialID, future.Add(time.Hour), future); err != nil {
		t.Fatal(err)
	}
	running, err := db.Claim(ctx, future, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Pause(ctx, c.ID, future); err != nil {
		t.Fatal(err)
	}
	if err = db.Complete(ctx, running, batch, future); err == nil {
		t.Fatal("paused job published")
	}
	if s, err := db.Status(ctx, c.ID, future); err != nil || s.Transactions != 4 {
		t.Fatal("pause lost data", s, err)
	}
	if _, err = db.Renew(ctx, c.ID, c.CredentialID, future.Add(time.Hour), future); err != nil {
		t.Fatal(err)
	}
	copy := filepath.Join(t.TempDir(), "copy.db")
	if err = db.Backup(ctx, copy); err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored.db")
	if _, err = Restore(ctx, copy, restoredPath, false); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var deliveries int
	if err = restored.db.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&deliveries); err != nil || deliveries != 0 {
		t.Fatal("restore resurrected deliveries", deliveries, err)
	}
	s, err := restored.Status(ctx, c.ID, future)
	if err != nil || s.Connection.Status != "paused" || s.Transactions != 4 {
		t.Fatalf("restored access: %+v %v", s, err)
	}
	if err = db.Revoke(ctx, c.ID, future); err != nil {
		t.Fatal(err)
	}
	var n int
	db.db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE connection_id=?`, c.ID).Scan(&n)
	if n != 0 {
		t.Fatal("revoked data retained")
	}
}

func TestPreviewDoesNotWriteAndReconciles(t *testing.T) {
	db, _, _ := fixture(t)
	ctx := t.Context()
	input := csvInput(t, "id,date,description,amount\na,2026-11-06,Café,-1.23\n")
	result, err := db.PreviewImport(ctx, "personal", "pessoal", input, testNow)
	if err != nil || result.New != 1 {
		t.Fatal(result, err)
	}
	if _, err = db.Connection(ctx, result.ConnectionID, testNow); !errors.Is(err, ranjit.ErrNotFound) {
		t.Fatal("preview saved connection", err)
	}
	if _, err = db.Import(ctx, "personal", "pessoal", input, testNow); err != nil {
		t.Fatal(err)
	}
	changed := csvInput(t, "id,date,description,amount\na,2026-11-06,Café,-1.22\nb,2026-11-07,Outro,3\n")
	result, err = db.PreviewImport(ctx, "personal", "pessoal", changed, testNow)
	if err != nil || result.New != 1 || result.Updated != 1 {
		t.Fatal(result, err)
	}
	status, err := db.Status(ctx, result.ConnectionID, testNow)
	if err != nil || status.Transactions != 1 {
		t.Fatal("preview changed data", status, err)
	}
	input.Snapshot.Transactions[0].Amount.AmountMinor = 200
	if _, err = db.PreviewImport(ctx, "personal", "pessoal", input, testNow); !errors.Is(err, ranjit.ErrInvalid) {
		t.Fatal("same file reinterpreted silently", err)
	}
}

func TestSearchCustomerDateLiteralTextAndPagination(t *testing.T) {
	db, _, _ := fixture(t)
	ctx := t.Context()
	input := csvInput(t, "id,date,description,amount\na,2026-11-06,100% Café,-1.23\nb,2026-11-07,Outro,3\nc,2026-11-07,Outro,4\n")
	for _, customer := range []string{"personal", "other"} {
		if _, err := db.Import(ctx, customer, "pessoal", input, testNow); err != nil {
			t.Fatal(err)
		}
	}
	f := Search{Customer: "personal", Limit: 1}
	seen := map[string]bool{}
	for {
		page, err := db.SearchTransactions(ctx, f, testNow)
		if err != nil {
			t.Fatal(err)
		}
		for _, tx := range page.Data {
			if seen[tx.ID] {
				t.Fatal("repeated cursor")
			}
			seen[tx.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		f.Cursor = page.NextCursor
	}
	if len(seen) != 3 {
		t.Fatal("wrong customer or missing results", len(seen))
	}
	page, err := db.SearchTransactions(ctx, Search{Customer: "personal", Query: "%", From: "2026-11-06", Through: "2026-11-06", Limit: 100}, testNow)
	if err != nil || len(page.Data) != 1 {
		t.Fatal("wildcard or date filtering", page, err)
	}
}
