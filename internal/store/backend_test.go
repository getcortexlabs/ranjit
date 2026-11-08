package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
)

func backendStores(t *testing.T, test func(*testing.T, *Store, string)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "db.sqlite")
		s, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		test(t, s, path)
	})
	t.Run("postgres", func(t *testing.T) { s, dsn := postgresFixture(t); test(t, s, dsn) })
}

func directGrant() ranjit.Connection {
	return ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "backend-customer", InstitutionID: "efi", Provider: "efi", Status: "active", AuthorizationMode: "api_credentials", CredentialID: "binding", CreatedAt: testNow, ConsentExpiresAt: testNow.Add(90 * 24 * time.Hour)}
}

func TestBackendMigrationPreservesExistingGrantAndJob(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, path string) {
		c := directGrant()
		if err := s.CreateAuthorizedConnection(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		migration := "007_backend.sql"
		if s.db.postgres {
			migration = "002_backend.sql"
		}
		if _, err := s.db.ExecContext(t.Context(), `DROP TABLE webhook_deliveries; DROP TABLE connection_requests;`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(t.Context(), `DELETE FROM schema_migrations WHERE name>=?`, migration); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		got, err := s.Connection(t.Context(), c.ID, testNow)
		if err != nil || got.CredentialID != c.CredentialID || got.Status != "active" {
			t.Fatal(got, err)
		}
		j, err := s.Claim(t.Context(), testNow, time.Minute)
		if err != nil || j.ConnectionID != c.ID {
			t.Fatal(j, err)
		}
		for _, table := range []string{"connection_requests", "webhook_deliveries"} {
			var count int
			if err = s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil || count != 0 {
				t.Fatal(table, count, err)
			}
		}
	})
}

func TestIdempotentConnectionConcurrencyRestartAndExpiry(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, path string) {
		var wg sync.WaitGroup
		ids := make(chan string, 16)
		errs := make(chan error, 16)
		for range 16 {
			wg.Go(func() {
				c, _, err := s.CreateConnectionOnce(t.Context(), directGrant(), nil, time.Time{}, "key-hash", "request-hash")
				ids <- c.ID
				errs <- err
			})
		}
		wg.Wait()
		id := <-ids
		for range 16 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		for range 15 {
			if next := <-ids; next != id {
				t.Fatal("duplicate grant", next, id)
			}
		}
		for _, table := range []string{"connections", "jobs", "connection_requests"} {
			var count int
			if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil || count != 1 {
				t.Fatal(table, count, err)
			}
		}
		if _, _, err := s.CreateConnectionOnce(t.Context(), directGrant(), nil, time.Time{}, "key-hash", "different"); !errors.Is(err, ranjit.ErrIdempotency) {
			t.Fatal(err)
		}
		s.Close()
		s, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		c, _, err := s.ReplayConnection(t.Context(), "key-hash", "request-hash", testNow.Add(time.Hour))
		if err != nil || c.ID != id {
			t.Fatal(c, err)
		}
		if err = s.Revoke(t.Context(), id, testNow); err != nil {
			t.Fatal(err)
		}
		c, _, err = s.ReplayConnection(t.Context(), "key-hash", "request-hash", testNow)
		if err != nil || c.Status != "revoked" {
			t.Fatal(c, err)
		}
		next := directGrant()
		next.CreatedAt = testNow.Add(IdempotencyTTL)
		c, _, err = s.CreateConnectionOnce(t.Context(), next, nil, time.Time{}, "key-hash", "new-request")
		if err != nil || c.ID == id {
			t.Fatal(c, err)
		}
	})
}

func TestConnectionRequestRollsBackWithGrant(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, _ string) {
		c := directGrant()
		c.Provider = "unsupported"
		if _, _, err := s.CreateConnectionOnce(t.Context(), c, nil, time.Time{}, "key", "request"); err == nil {
			t.Fatal("invalid grant accepted")
		}
		if _, _, err := s.ReplayConnection(t.Context(), "key", "request", testNow); !errors.Is(err, ranjit.ErrNotFound) {
			t.Fatal("failed request retained", err)
		}
		c = directGrant()
		if _, _, err := s.CreateConnectionOnce(t.Context(), c, nil, time.Time{}, "key", "request"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestWebhookDeliveryRestartFencingRetryAndTarget(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, path string) {
		target := "https://backend.example/webhooks"
		c := directGrant()
		if err := s.CreateAuthorizedConnection(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		s.EnableWebhooks(target)
		j, err := s.Claim(t.Context(), testNow, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		batch, _ := (sandbox.Source{}).Fetch(t.Context(), c)
		if err = s.Complete(t.Context(), j, batch, testNow); err != nil {
			t.Fatal(err)
		}
		if err = s.Complete(t.Context(), j, batch, testNow); !errors.Is(err, ranjit.ErrLease) {
			t.Fatal(err)
		}
		page, err := s.WebhookDeliveries(t.Context(), "", 100)
		if err != nil || len(page.Data) != 1 {
			t.Fatal(page, err)
		}
		event := page.Data[0].Event
		if event.Type != "sync.succeeded" || event.Data.JobID != j.ID || event.Data.CustomerID != c.CustomerID {
			t.Fatal(event)
		}
		old, err := s.ClaimWebhook(t.Context(), testNow)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err = Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		s.EnableWebhooks("https://another.example/webhooks")
		if _, err = s.ClaimWebhook(t.Context(), testNow.Add(time.Minute)); !errors.Is(err, ranjit.ErrNotFound) {
			t.Fatal("wrong target claimed", err)
		}
		if err = s.RetryWebhook(t.Context(), old.ID, testNow); !errors.Is(err, ranjit.ErrWebhookState) {
			t.Fatal(err)
		}
		s.EnableWebhooks(target)
		if _, err = s.ClaimWebhook(t.Context(), testNow.Add(29*time.Second)); !errors.Is(err, ranjit.ErrNotFound) {
			t.Fatal("active lease reclaimed", err)
		}
		now := testNow.Add(30 * time.Second)
		attempt, err := s.ClaimWebhook(t.Context(), now)
		if err != nil {
			t.Fatal(err)
		}
		if attempt.ID != old.ID || string(attempt.Payload) != string(old.Payload) || attempt.LeaseToken == old.LeaseToken || attempt.Attempts != 2 {
			t.Fatal(attempt)
		}
		if err = s.FinishWebhook(t.Context(), old, 200, now); !errors.Is(err, ranjit.ErrLease) {
			t.Fatal("stale worker accepted", err)
		}
		for {
			if err = s.FinishWebhook(t.Context(), attempt, 503, now); err != nil {
				t.Fatal(err)
			}
			page, err = s.WebhookDeliveries(t.Context(), "", 100)
			if err != nil {
				t.Fatal(err)
			}
			d := page.Data[0]
			if d.Status == "failed" {
				if d.Attempts != WebhookMaxAttempts {
					t.Fatal(d)
				}
				break
			}
			if _, err = s.ClaimWebhook(t.Context(), now); !errors.Is(err, ranjit.ErrNotFound) {
				t.Fatal("backoff ignored", err)
			}
			now = *d.NextAttemptAt
			attempt, err = s.ClaimWebhook(t.Context(), now)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err = s.RetryWebhook(t.Context(), event.ID, now); err != nil {
			t.Fatal(err)
		}
		attempt, err = s.ClaimWebhook(t.Context(), now)
		if err != nil || attempt.ID != event.ID || attempt.Attempts != 1 {
			t.Fatal(attempt, err)
		}
		if err = s.FinishWebhook(t.Context(), attempt, 204, now); err != nil {
			t.Fatal(err)
		}
		if err = s.RetryWebhook(t.Context(), event.ID, now); !errors.Is(err, ranjit.ErrWebhookState) {
			t.Fatal("delivered retry", err)
		}
		if err = s.Revoke(t.Context(), c.ID, now); err != nil {
			t.Fatal(err)
		}
		page, err = s.WebhookDeliveries(t.Context(), "", 100)
		if err != nil || len(page.Data) != 1 || page.Data[0].Event.Type != "connection.revoked" || page.Data[0].Event.Data.CustomerID != "" {
			t.Fatal("revocation retained more than its minimal event", err)
		}
	})
}

func TestWebhookFailureEventsAndOutboxRollback(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, _ string) {
		c := directGrant()
		if err := s.CreateAuthorizedConnection(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		s.EnableWebhooks("https://backend.example/webhooks")
		j, err := s.Claim(t.Context(), testNow, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Fail(t.Context(), j, "provider_unavailable", testNow); err != nil {
			t.Fatal(err)
		}
		page, err := s.WebhookDeliveries(t.Context(), "", 100)
		if err != nil || len(page.Data) != 0 {
			t.Fatal("nonterminal failure emitted", err)
		}
		j, err = s.Claim(t.Context(), testNow.Add(time.Minute), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Fail(t.Context(), j, "provider_credentials", testNow.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		page, err = s.WebhookDeliveries(t.Context(), "", 100)
		if err != nil || len(page.Data) != 1 || page.Data[0].Event.Type != "sync.failed" || page.Data[0].Event.Data.ErrorCode != "provider_credentials" {
			t.Fatal(page, err)
		}
		j = claim(t, s, c, testNow.Add(2*time.Minute))
		if _, err = s.db.ExecContext(t.Context(), `UPDATE jobs SET attempts=? WHERE id=?`, MaxAttempts, j.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Claim(t.Context(), testNow.Add(4*time.Minute), time.Minute); !errors.Is(err, ranjit.ErrNotFound) {
			t.Fatal(err)
		}
		page, err = s.WebhookDeliveries(t.Context(), "", 100)
		if err != nil || len(page.Data) != 2 {
			t.Fatal("exhausted lease missing", page, err)
		}

		// A failed outbox insert must also roll back the financial snapshot.
		ddl := `CREATE TRIGGER reject_webhook BEFORE INSERT ON webhook_deliveries BEGIN SELECT RAISE(ABORT,'test'); END`
		if s.db.postgres {
			ddl = `CREATE FUNCTION reject_webhook() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test'; END; $$; CREATE TRIGGER reject_webhook BEFORE INSERT ON webhook_deliveries FOR EACH ROW EXECUTE FUNCTION reject_webhook();`
		}
		if _, err = s.db.ExecContext(t.Context(), ddl); err != nil {
			t.Fatal(err)
		}
		now := testNow.Add(5 * time.Minute)
		j = claim(t, s, c, now)
		batch, _ := (sandbox.Source{}).Fetch(t.Context(), c)
		if err = s.Complete(t.Context(), j, batch, now); err == nil {
			t.Fatal("outbox failure ignored")
		}
		got, err := s.Job(t.Context(), j.ID)
		if err != nil || got.Status != "running" {
			t.Fatal(got, err)
		}
		var count int
		if err = s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM accounts`).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial snapshot committed", count, err)
		}
	})
}

func TestWebhookRetentionWhileDeliveryDisabled(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, _ string) {
		s.EnableWebhooks("https://backend.example/events")
		c := directGrant()
		if err := s.CreateAuthorizedConnection(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		j, err := s.Claim(t.Context(), testNow, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Fail(t.Context(), j, "provider_credentials", testNow); err != nil {
			t.Fatal(err)
		}
		s.EnableWebhooks("")
		if err = s.Expire(t.Context(), testNow.Add(WebhookRetention+time.Second)); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM webhook_deliveries`).Scan(&count); err != nil || count != 0 {
			t.Fatal("delivery metadata retained", count, err)
		}
	})
}
