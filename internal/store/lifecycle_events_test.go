package store

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
)

func lifecycleTypes(t *testing.T, s *Store) map[string]int {
	t.Helper()
	page, err := s.WebhookDeliveries(t.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, d := range page.Data {
		counts[d.Event.Type]++
		if strings.HasPrefix(d.Event.Type, "connection.") && d.Event.Data.JobID != "" {
			t.Fatal("lifecycle event has job")
		}
	}
	return counts
}

func TestLifecycleEventsAreAtomicAndNoopsDoNotDuplicate(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, path string) {
		target := "https://backend.example/events"
		s.EnableWebhooks(target)
		c := directGrant()
		c.ConsentExpiresAt = testNow.Add(10 * time.Minute)
		for range 2 {
			if _, _, err := s.CreateConnectionOnce(t.Context(), c, nil, time.Time{}, "key", "body"); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() { errs <- s.Pause(t.Context(), c.ID, testNow) })
		}
		wg.Wait()
		for range 8 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		for range 2 {
			if _, err := s.Renew(t.Context(), c.ID, c.CredentialID, testNow.Add(time.Hour), testNow); err != nil {
				t.Fatal(err)
			}
		}
		for range 2 {
			if err := s.Expire(t.Context(), testNow.Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		counts := lifecycleTypes(t, s)
		for _, kind := range []string{"connection.activated", "connection.paused", "connection.renewed", "connection.expired"} {
			if counts[kind] != 1 {
				t.Fatal(kind, counts)
			}
		}
		inFlight, err := s.ClaimWebhook(t.Context(), testNow.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Revoke(t.Context(), c.ID, testNow.Add(2*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err = s.FinishWebhook(t.Context(), inFlight, 204, testNow.Add(2*time.Hour)); !errors.Is(err, ranjit.ErrLease) {
			t.Fatal("revoked delivery acknowledged", err)
		}
		first, err := s.WebhookDeliveries(t.Context(), "", 100)
		if err != nil || len(first.Data) != 1 {
			t.Fatal(first, err)
		}
		event := first.Data[0].Event
		if event.Type != "connection.revoked" || event.Data.CustomerID != "" || event.Data.JobID != "" || event.Data.Status != "revoked" {
			t.Fatal(event)
		}
		if err = s.Revoke(t.Context(), c.ID, testNow.Add(2*time.Hour)); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err = Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		s.EnableWebhooks(target)
		next, err := s.WebhookDeliveries(t.Context(), "", 100)
		if err != nil || len(next.Data) != 1 || next.Data[0].Event.ID != event.ID {
			t.Fatal("revocation replay changed event", next, err)
		}
		if !s.db.postgres {
			backup := filepath.Join(t.TempDir(), "backup.db")
			if err = s.Backup(t.Context(), backup); err != nil {
				t.Fatal(err)
			}
			restoredPath := filepath.Join(t.TempDir(), "restored.db")
			if _, err = Restore(t.Context(), backup, restoredPath, false); err != nil {
				t.Fatal(err)
			}
			restored, err := Open(t.Context(), restoredPath)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			restored.EnableWebhooks(target)
			if counts := lifecycleTypes(t, restored); len(counts) != 0 {
				t.Fatal("restore emitted old events", counts)
			}
		}
	})
}

func TestSandboxDecisionAndExpiryEvents(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, _ string) {
		s.EnableWebhooks("https://backend.example/events")
		for _, decision := range []string{"approve", "deny", "expire"} {
			c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "customer", InstitutionID: "sandbox-bank", Provider: "sandbox", Status: "pending", CreatedAt: testNow, ConsentExpiresAt: testNow.Add(time.Hour)}
			if _, _, err := s.CreateConnectionOnce(t.Context(), c, decision, testNow.Add(time.Minute), "", ""); err != nil {
				t.Fatal(err)
			}
			if decision != "expire" {
				if _, err := s.Decide(t.Context(), decision, decision == "approve", testNow); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Decide(t.Context(), decision, decision == "approve", testNow); !errors.Is(err, ranjit.ErrConsent) {
					t.Fatal(err)
				}
			}
		}
		if err := s.Expire(t.Context(), testNow.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		counts := lifecycleTypes(t, s)
		for _, kind := range []string{"connection.activated", "connection.denied", "connection.expired"} {
			if counts[kind] != 1 {
				t.Fatal(counts)
			}
		}
	})
}

func TestLifecycleOutboxFailureRollsBackStateAndDeletion(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, _ string) {
		c := directGrant()
		c.ConsentExpiresAt = testNow.Add(time.Hour)
		if err := s.CreateAuthorizedConnection(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		j := claim(t, s, c, testNow)
		batch, err := (sandbox.Source{}).Fetch(t.Context(), ranjit.Connection{Provider: "sandbox", InstitutionID: "sandbox-bank"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Complete(t.Context(), j, batch, testNow); err != nil {
			t.Fatal(err)
		}
		s.EnableWebhooks("https://backend.example/events")
		ddl := `CREATE TRIGGER reject_lifecycle BEFORE INSERT ON webhook_deliveries BEGIN SELECT RAISE(ABORT,'test'); END`
		if s.db.postgres {
			ddl = `CREATE FUNCTION reject_lifecycle() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test'; END; $$; CREATE TRIGGER reject_lifecycle BEFORE INSERT ON webhook_deliveries FOR EACH ROW EXECUTE FUNCTION reject_lifecycle();`
		}
		if _, err := s.db.ExecContext(t.Context(), ddl); err != nil {
			t.Fatal(err)
		}
		if err := s.Pause(t.Context(), c.ID, testNow); err == nil {
			t.Fatal("pause accepted without event")
		}
		if _, err := s.Renew(t.Context(), c.ID, c.CredentialID, testNow.Add(2*time.Hour), testNow); err == nil {
			t.Fatal("renew accepted without event")
		}
		if err := s.Expire(t.Context(), testNow.Add(2*time.Hour)); err == nil {
			t.Fatal("expiry accepted without event")
		}
		if err := s.Revoke(t.Context(), c.ID, testNow); err == nil {
			t.Fatal("revocation accepted without event")
		}
		got, err := s.Connection(t.Context(), c.ID, testNow)
		if err != nil || got.Status != "active" || !got.ConsentExpiresAt.Equal(c.ConsentExpiresAt) {
			t.Fatal(got, err)
		}
		page, err := s.Accounts(t.Context(), c.ID, "", 100, testNow)
		if err != nil || len(page.Data) != 2 {
			t.Fatal("financial data deleted after rollback", err)
		}
	})
}

func TestLifecycleMigrationPreservesSyncDeliveries(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, path string) {
		c := directGrant()
		if err := s.CreateAuthorizedConnection(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		if s.db.postgres {
			if _, err := s.db.ExecContext(t.Context(), `ALTER TABLE webhook_deliveries ALTER COLUMN job_id SET NOT NULL; DELETE FROM schema_migrations WHERE name='003_connection_events.sql'`); err != nil {
				t.Fatal(err)
			}
		} else {
			ddl, _ := migrations.ReadFile("migrations/007_backend.sql")
			old := string(ddl)
			old = old[strings.Index(old, "CREATE TABLE webhook_deliveries"):]
			if _, err := s.db.ExecContext(t.Context(), `DROP TABLE webhook_deliveries; DELETE FROM schema_migrations WHERE name='008_connection_events.sql';`+old); err != nil {
				t.Fatal(err)
			}
		}
		s.EnableWebhooks("https://backend.example/events")
		j := claim(t, s, c, testNow)
		if err := s.Fail(t.Context(), j, "provider_credentials", testNow); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		s.EnableWebhooks("https://backend.example/events")
		if err = s.Pause(t.Context(), c.ID, testNow); err != nil {
			t.Fatal(err)
		}
		counts := lifecycleTypes(t, s)
		if counts["sync.failed"] != 1 || counts["connection.paused"] != 1 {
			t.Fatal(counts)
		}
	})
}
