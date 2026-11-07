package inter

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit/internal/store"
	"github.com/getcortexlabs/ranjit/internal/worker"
)

func TestResumeAfterRestartKeepsWindowAndDoesNotRepeatSuccessfulPages(t *testing.T) {
	s := sourceFixture(t)
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	clock := now
	c := connection(s)
	c.ConsentExpiresAt = now.Add(48 * time.Hour)
	if err = db.CreateAuthorizedConnection(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	balanceCalls, pageZeroCalls, pageOneCalls := 0, 0, 0
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return response(200, authorized), nil
		}
		if r.URL.Path == "/banking/v2/saldo" {
			balanceCalls++
			return response(200, `{"disponivel":"100.01"}`), nil
		}
		if r.URL.Query().Get("dataFim") != "2026-11-07" {
			t.Fatal("window moved during resume")
		}
		if r.URL.Query().Get("pagina") == "0" {
			pageZeroCalls++
			return response(200, pageJSON(0)), nil
		}
		pageOneCalls++
		if pageOneCalls == 1 {
			return response(503, `{"private":"not retained"}`), nil
		}
		return response(200, pageJSON(1)), nil
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := worker.New(db, s, time.Hour, logger)
	w.Now = func() time.Time { return clock }
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := db.Status(t.Context(), c.ID, clock)
	if err != nil || status.Accounts != 0 || status.LatestJob.RequestsCompleted != 2 {
		t.Fatalf("partial publish or missing progress: %+v %v", status, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(24 * time.Hour)
	s.now = func() time.Time { return clock }
	w = worker.New(db, s, time.Hour, logger)
	w.Now = func() time.Time { return clock }
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err = db.Status(t.Context(), c.ID, clock)
	if err != nil || status.Transactions != 3 || status.LatestJob.Status != "succeeded" || status.LatestJob.RequestsCompleted != 3 {
		t.Fatalf("resume: %+v %v", status, err)
	}
	if balanceCalls != 1 || pageZeroCalls != 1 || pageOneCalls != 2 {
		t.Fatalf("calls: balance=%d page0=%d page1=%d", balanceCalls, pageZeroCalls, pageOneCalls)
	}
}
