package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
)

var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T) (*Store, ranjit.Connection, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ranjit.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "customer-1", InstitutionID: "sandbox-bank", Provider: "sandbox", Status: "active", CreatedAt: testNow, ConsentExpiresAt: testNow.Add(time.Hour)}
	if err = s.CreateConnection(t.Context(), c, "unused-token-hash", testNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	return s, c, path
}

func claim(t *testing.T, s *Store, c ranjit.Connection, now time.Time) ranjit.Job {
	t.Helper()
	if _, err := s.Enqueue(t.Context(), c.ID, now); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(t.Context(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestSyncUpsertsWithoutDuplicatingAndRollsBackOnFailure(t *testing.T) {
	s, c, _ := fixture(t)
	batch, err := (sandbox.Source{}).Fetch(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		j := claim(t, s, c, testNow)
		if err = s.Complete(t.Context(), j, batch, testNow); err != nil {
			t.Fatal(err)
		}
	}
	var accounts, transactions int
	if err = s.db.QueryRow(`SELECT count(*) FROM accounts`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM transactions`).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if accounts != 2 || transactions != 4 {
		t.Fatalf("got %d accounts and %d transactions", accounts, transactions)
	}
	// A database failure after accounts were written must roll back balances, job and sync timestamp.
	if _, err = s.db.Exec(`CREATE TRIGGER reject_transaction BEFORE UPDATE ON transactions BEGIN SELECT RAISE(ABORT, 'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	j := claim(t, s, c, testNow.Add(time.Second))
	batch.Accounts[0].Balance.AmountMinor = 1
	if err = s.Complete(t.Context(), j, batch, testNow.Add(time.Second)); err == nil {
		t.Fatal("expected write failure")
	}
	var balance int64
	if err = s.db.QueryRow(`SELECT balance_minor FROM accounts WHERE external_id = 'checking-1'`).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 482135 {
		t.Fatalf("partially committed balance: %d", balance)
	}
	got, err := s.Job(t.Context(), j.ID)
	if err != nil || got.Status != "running" {
		t.Fatalf("job was committed: %+v, %v", got, err)
	}
	connection, err := s.Connection(t.Context(), c.ID, testNow)
	if err != nil || connection.LastSyncedAt == nil || !connection.LastSyncedAt.Equal(testNow) {
		t.Fatalf("sync timestamp advanced: %+v %v", connection, err)
	}
}

func TestLeaseSurvivesRestartAndFencesOldWorker(t *testing.T) {
	s, c, path := fixture(t)
	old := claim(t, s, c, testNow)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Claim(t.Context(), testNow.Add(30*time.Second), time.Minute); !errors.Is(err, ranjit.ErrNotFound) {
		t.Fatalf("unexpired lease reclaimed: %v", err)
	}
	now := testNow.Add(time.Minute)
	next, err := s.Claim(t.Context(), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != old.ID || next.Attempts != 2 || next.LeaseToken == old.LeaseToken {
		t.Fatalf("bad recovered job: %+v", next)
	}
	batch, _ := (sandbox.Source{}).Fetch(t.Context(), c)
	if err = s.Complete(t.Context(), old, batch, now); !errors.Is(err, ranjit.ErrLease) {
		t.Fatalf("old worker accepted: %v", err)
	}
	if err = s.Fail(t.Context(), old, "provider_unavailable", now); !errors.Is(err, ranjit.ErrLease) {
		t.Fatalf("old failure accepted: %v", err)
	}
	if err = s.Complete(t.Context(), next, batch, now); err != nil {
		t.Fatal(err)
	}
}

func TestRevocationPreventsInFlightImport(t *testing.T) {
	s, c, _ := fixture(t)
	j := claim(t, s, c, testNow)
	batch, _ := (sandbox.Source{}).Fetch(t.Context(), c)
	if err := s.Revoke(t.Context(), c.ID, testNow); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(t.Context(), j, batch, testNow); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatalf("revoked import accepted: %v", err)
	}
	job, err := s.Job(t.Context(), j.ID)
	if err != nil || job.Status != "cancelled" {
		t.Fatalf("job = %+v, %v", job, err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("data remains: %d, %v", count, err)
	}
}

func TestExpiredConsentBlocksReadsBeforeCleanup(t *testing.T) {
	s, c, _ := fixture(t)
	j := claim(t, s, c, testNow)
	batch, _ := (sandbox.Source{}).Fetch(t.Context(), c)
	if err := s.Complete(t.Context(), j, batch, testNow); err != nil {
		t.Fatal(err)
	}
	page, err := s.Accounts(t.Context(), c.ID, "", 100, testNow)
	if err != nil {
		t.Fatal(err)
	}
	now := c.ConsentExpiresAt
	if _, err = s.Accounts(t.Context(), c.ID, "", 100, now); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatalf("expired accounts readable: %v", err)
	}
	if _, err = s.Transactions(t.Context(), page.Data[0].ID, "", 100, now); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatalf("expired transactions readable: %v", err)
	}
	if _, err = s.Enqueue(t.Context(), c.ID, now); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatalf("expired sync accepted: %v", err)
	}
	if err = s.Expire(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM transactions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("transactions remain: %d, %v", count, err)
	}
}

func TestConcurrentEnqueueReturnsOneJob(t *testing.T) {
	s, c, _ := fixture(t)
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	errs := make(chan error, 16)
	for range 16 {
		wg.Go(func() { j, err := s.Enqueue(context.Background(), c.ID, testNow); ids <- j.ID; errs <- err })
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("duplicate job: %s != %s", id, first)
		}
	}
}

func TestFailedJobsBackOffAndStopAfterFiveAttempts(t *testing.T) {
	s, c, _ := fixture(t)
	now := testNow
	if _, err := s.Enqueue(t.Context(), c.ID, now); err != nil {
		t.Fatal(err)
	}
	var last ranjit.Job
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		j, err := s.Claim(t.Context(), now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if j.Attempts != attempt {
			t.Fatalf("attempt = %d", j.Attempts)
		}
		if err = s.Fail(t.Context(), j, "provider_unavailable", now); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Claim(t.Context(), now, time.Minute); !errors.Is(err, ranjit.ErrNotFound) {
			t.Fatalf("backoff bypassed: %v", err)
		}
		last = j
		now = now.Add(time.Duration(1<<attempt) * time.Second)
	}
	j, err := s.Job(t.Context(), last.ID)
	if err != nil || j.Status != "failed" {
		t.Fatalf("job = %+v, %v", j, err)
	}
	if err = s.Schedule(t.Context(), now, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(t.Context(), now, time.Minute); !errors.Is(err, ranjit.ErrNotFound) {
		t.Fatalf("scheduler reset exhausted attempts immediately: %v", err)
	}
}
