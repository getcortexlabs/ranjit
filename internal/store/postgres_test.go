package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
)

func postgresFixture(t *testing.T) (*Store, string) {
	t.Helper()
	raw := os.Getenv("RANJIT_TEST_POSTGRES_URL")
	if raw == "" {
		t.Skip("RANJIT_TEST_POSTGRES_URL is not configured")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal("cannot open test database")
	}
	schema := strings.ToLower(ranjit.NewID("test"))
	if _, err = admin.ExecContext(t.Context(), `CREATE SCHEMA "`+schema+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, u.String()
}

func TestPostgresSyncLifecycleAndRestart(t *testing.T) {
	s, dsn := postgresFixture(t)
	c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "backend-user", InstitutionID: "bank", Provider: "efi", AuthorizationMode: "api_credentials", CredentialID: "binding", Status: "active", CreatedAt: testNow, ConsentExpiresAt: testNow.Add(time.Hour)}
	if err := s.CreateAuthorizedConnection(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(t.Context(), testNow, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cp := Checkpoint{Store: s, Job: j, Now: func() time.Time { return testNow }}
	if err = cp.Save(t.Context(), "/transactions", []byte(`{"fixture":true}`)); err != nil {
		t.Fatal(err)
	}
	other, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatal("second startup migration", err)
	}
	defer other.Close()
	cp.Store = other
	if body, err := cp.Load(t.Context(), "/transactions"); err != nil || string(body) != `{"fixture":true}` {
		t.Fatal("checkpoint did not survive a new process", err)
	}
	if _, err = other.Claim(t.Context(), testNow, time.Minute); !errors.Is(err, ranjit.ErrNotFound) {
		t.Fatal("live lease stolen", err)
	}
	now := testNow.Add(time.Minute)
	next, err := other.Claim(t.Context(), now, time.Minute)
	if err != nil || next.LeaseToken == j.LeaseToken {
		t.Fatal("expired lease not recovered", err)
	}
	batch, err := (sandbox.Source{}).Fetch(t.Context(), ranjit.Connection{Provider: "sandbox", InstitutionID: "sandbox-bank"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(t.Context(), j, batch, now); !errors.Is(err, ranjit.ErrLease) {
		t.Fatal("old worker accepted", err)
	}
	if err = other.Complete(t.Context(), next, batch, now); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		job := claim(t, other, c, now)
		if err = other.Complete(t.Context(), job, batch, now); err != nil {
			t.Fatal(err)
		}
	}
	status, err := s.Status(t.Context(), c.ID, now)
	if err != nil || status.Accounts != 2 || status.Transactions != 4 {
		t.Fatalf("upsert: %+v %v", status, err)
	}
	var count int
	if err = s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sync_responses`).Scan(&count); err != nil || count != 0 {
		t.Fatal("finished checkpoint retained", count, err)
	}
	if err = s.Pause(t.Context(), c.ID, now); err != nil {
		t.Fatal(err)
	}
	if page, err := s.Accounts(t.Context(), c.ID, "", 100, now); err != nil || len(page.Data) != 2 {
		t.Fatal("paused history lost", err)
	}
	if _, err = s.Renew(t.Context(), c.ID, "binding", now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	j, err = s.Claim(t.Context(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(t.Context(), c.ID, now); err != nil {
		t.Fatal(err)
	}
	if err = other.Complete(t.Context(), j, batch, now); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatal("revoked import accepted", err)
	}
	if err = s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM transactions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked data retained", count, err)
	}
	if err = s.Backup(t.Context(), filepath.Join(t.TempDir(), "copy.db")); err == nil {
		t.Fatal("SQLite backup accepted for PostgreSQL")
	}
}

func TestPostgresConcurrentClaims(t *testing.T) {
	s, dsn := postgresFixture(t)
	other, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "customer", InstitutionID: "sandbox-bank", Provider: "sandbox", Status: "active", CreatedAt: testNow, ConsentExpiresAt: testNow.Add(time.Hour)}
	if err = s.CreateConnection(t.Context(), c, "token", testNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		store := []*Store{s, other}[i%2]
		wg.Go(func() { _, err := store.Enqueue(t.Context(), c.ID, testNow); errs <- err })
	}
	wg.Wait()
	for range 16 {
		if err = <-errs; err != nil {
			t.Fatal(err)
		}
	}
	for i := range 16 {
		store := []*Store{s, other}[i%2]
		wg.Go(func() { _, err := store.Claim(t.Context(), testNow, time.Minute); errs <- err })
	}
	wg.Wait()
	claimed := 0
	for range 16 {
		if err = <-errs; err == nil {
			claimed++
		} else if !errors.Is(err, ranjit.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed %d times", claimed)
	}
}

func TestPostgresImportPreviewSearchAndRollback(t *testing.T) {
	s, _ := postgresFixture(t)
	input := csvInput(t, "id,date,description,amount\na,2026-11-06,Café,-1.23\n")
	preview, err := s.PreviewImport(t.Context(), "customer", "personal", input, testNow)
	if err != nil || preview.New != 1 {
		t.Fatal(preview, err)
	}
	if _, err = s.Connection(t.Context(), preview.ConnectionID, testNow); !errors.Is(err, ranjit.ErrNotFound) {
		t.Fatal("preview persisted", err)
	}
	result, err := s.Import(t.Context(), "customer", "personal", input, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.Import(t.Context(), "customer", "personal", input, testNow); err != nil || !again.Duplicate {
		t.Fatal("duplicate import", err)
	}
	page, err := s.SearchTransactions(t.Context(), Search{Customer: "customer", Query: "CAF", Limit: 100}, testNow)
	if err != nil || len(page.Data) != 1 || page.Data[0].Amount.AmountMinor != -123 {
		t.Fatal(page, err)
	}
	accounts, err := s.Accounts(t.Context(), result.ConnectionID, "", 100, testNow)
	if err != nil || len(accounts.Data) != 1 || accounts.Data[0].Balance != nil {
		t.Fatal("unknown balance", accounts, err)
	}
	_, err = s.db.ExecContext(t.Context(), `CREATE FUNCTION reject_import() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.description='rejected' THEN RAISE EXCEPTION 'test'; END IF; RETURN NEW; END; $$; CREATE TRIGGER reject_import BEFORE INSERT ON transactions FOR EACH ROW EXECUTE FUNCTION reject_import();`)
	if err != nil {
		t.Fatal(err)
	}
	bad := csvInput(t, "id,date,description,amount\nb,2026-11-07,valid,1\nc,2026-11-07,rejected,2\n")
	if _, err = s.Import(t.Context(), "customer", "personal", bad, testNow); err == nil {
		t.Fatal("expected write failure")
	}
	status, err := s.Status(t.Context(), result.ConnectionID, testNow)
	if err != nil || status.Transactions != 1 {
		t.Fatal("partial import committed", status, err)
	}
	if err = s.Expire(t.Context(), testNow.AddDate(1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(t.Context(), result.ConnectionID, testNow); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM imports`).Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked ledger retained", count, err)
	}
}

func TestPostgresConfigurationErrorsHideCredentials(t *testing.T) {
	_, err := Open(t.Context(), "postgres://user:never-print-me@localhost:invalid/db")
	if err == nil || strings.Contains(err.Error(), "never-print-me") {
		t.Fatal("invalid URL accepted or credential exposed")
	}
	if _, err = Restore(t.Context(), "backup.db", "postgres://user:secret@db/name", true); err == nil {
		t.Fatal("PostgreSQL treated as a path")
	}
}
