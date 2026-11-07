package worker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
	"github.com/getcortexlabs/ranjit/internal/store"
)

type testSource struct {
	fetch func(context.Context, ranjit.Connection) (provider.Snapshot, error)
}

func (s testSource) Institutions() []ranjit.Institution { return (sandbox.Source{}).Institutions() }
func (s testSource) Fetch(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
	return s.fetch(ctx, c)
}

func TestProviderFailureRetriesWithoutPersistingSensitiveError(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "test", InstitutionID: "sandbox-bank", Provider: "sandbox", Status: "active", CreatedAt: now, ConsentExpiresAt: now.Add(time.Hour)}
	if err = db.CreateConnection(t.Context(), c, "unused", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	job, err := db.Enqueue(t.Context(), c.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	source := testSource{fetch: func(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
		calls++
		if calls == 1 {
			return provider.Snapshot{}, errors.New("upstream response included secret-bank-token")
		}
		return (sandbox.Source{}).Fetch(ctx, c)
	}}
	logs := &bytes.Buffer{}
	w := New(db, source, time.Hour, slog.New(slog.NewJSONHandler(logs, nil)))
	w.Now = func() time.Time { return now }
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	failed, err := db.Job(t.Context(), job.ID)
	if err != nil || failed.Status != "queued" || failed.ErrorCode != "provider_unavailable" {
		t.Fatalf("job = %+v, %v", failed, err)
	}
	if strings.Contains(logs.String(), "secret-bank-token") {
		t.Fatal("raw provider error leaked to logs")
	}
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("retry ignored backoff")
	}
	now = now.Add(2 * time.Second)
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	done, err := db.Job(t.Context(), job.ID)
	if err != nil || done.Status != "succeeded" || done.Attempts != 2 || done.ErrorCode != "" {
		t.Fatalf("job = %+v, %v", done, err)
	}
}

func TestRateLimitDelaySurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "test", InstitutionID: "sandbox-bank", Provider: "sandbox", Status: "active", CreatedAt: now, ConsentExpiresAt: now.Add(time.Hour)}
	if err = db.CreateConnection(t.Context(), c, "unused", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	job, err := db.Enqueue(t.Context(), c.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	source := testSource{fetch: func(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
		calls++
		if calls == 1 {
			return provider.Snapshot{}, &provider.RetryError{After: 2 * time.Minute}
		}
		return (sandbox.Source{}).Fetch(ctx, c)
	}}
	w := New(db, source, time.Hour, slog.Default())
	w.Now = func() time.Time { return now }
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	w.Store = db
	now = now.Add(119 * time.Second)
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("restart lost the bank's retry delay")
	}
	now = now.Add(time.Second)
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	done, err := db.Job(t.Context(), job.ID)
	if err != nil || done.Status != "succeeded" || done.Attempts != 2 {
		t.Fatalf("job = %+v %v", done, err)
	}
}
