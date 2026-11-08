package webhook

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
	"github.com/getcortexlabs/ranjit/internal/service"
	"github.com/getcortexlabs/ranjit/internal/store"
	syncworker "github.com/getcortexlabs/ranjit/internal/worker"
)

func TestSignedDeliveryRetriesAndRefusesRedirects(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	const secret = "webhook-test-secret-at-least-32-bytes"
	var bodies, signatures []string
	var unexpected int
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { unexpected++; w.WriteHeader(204) }))
	defer sink.Close()
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		event, err := ranjit.VerifyWebhook(body, r.Header.Get("Ranjit-Signature"), secret, now)
		if err != nil || event.Type != "sync.succeeded" || r.Header.Get("Ranjit-Event-ID") != event.ID {
			t.Error("invalid delivered signature", err)
		}
		if strings.Contains(string(body), "amount_minor") || strings.Contains(string(body), "balance") {
			t.Error("financial payload leaked")
		}
		bodies = append(bodies, string(body))
		signatures = append(signatures, r.Header.Get("Ranjit-Signature"))
		switch len(bodies) {
		case 1:
			http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer receiver.Close()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.EnableWebhooks(receiver.URL)
	source := sandbox.Source{}
	svc := service.New(db, source, "http://localhost:8080")
	svc.Now = func() time.Time { return now }
	c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "test-customer", InstitutionID: "sandbox-bank", Provider: "sandbox", Status: "active", CreatedAt: now, ConsentExpiresAt: now.Add(time.Hour)}
	if err = db.CreateConnection(t.Context(), c, "unused", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	syncer := syncworker.New(db, source, time.Hour, logger)
	syncer.Now = svc.Now
	if err = syncer.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := New(db, receiver.URL, secret, logger)
	w.Now = svc.Now
	defer w.Client.CloseIdleConnections()
	for range 3 {
		if err = w.Step(t.Context()); err != nil {
			t.Fatal(err)
		}
		page, err := db.WebhookDeliveries(t.Context(), "", 100)
		if err != nil || len(page.Data) != 1 {
			t.Fatal(page, err)
		}
		if page.Data[0].NextAttemptAt != nil {
			now = *page.Data[0].NextAttemptAt
		}
	}
	if unexpected != 0 || len(bodies) != 3 || bodies[0] != bodies[2] || signatures[0] == signatures[2] {
		t.Fatal("redirect followed, event changed or signature not renewed")
	}
	page, err := db.WebhookDeliveries(t.Context(), "", 100)
	if err != nil || page.Data[0].Status != "delivered" || page.Data[0].Attempts != 3 {
		t.Fatal(page, err)
	}
}
