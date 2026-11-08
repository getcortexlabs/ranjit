package service

import (
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
	"github.com/getcortexlabs/ranjit/internal/store"
)

func TestDirectReplayDoesNotVerifyAgain(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bank := &profileSource{key: "binding"}
	svc := New(db, bank, "http://localhost:8080")
	input := ranjit.ConnectRequest{CustomerID: "customer", InstitutionID: "inter"}
	first, err := svc.ConnectWithKey(t.Context(), input, "operation-key-1234")
	if err != nil {
		t.Fatal(err)
	}
	// Even if the profile becomes unavailable, replay returns the existing grant.
	svc.Source = sandbox.Source{}
	again, err := svc.ConnectWithKey(t.Context(), input, "operation-key-1234")
	if err != nil || again.Connection.ID != first.Connection.ID || bank.calls != 1 {
		t.Fatal(again, err, bank.calls)
	}
}

func TestSandboxReplaySurvivesRestartButNotConsentExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	svc := New(db, sandbox.Source{}, "http://localhost:8080")
	svc.Now = func() time.Time { return now }
	input := ranjit.ConnectRequest{CustomerID: "customer", InstitutionID: "sandbox-bank"}
	key := "operation-key-1234"
	first, err := svc.ConnectWithKey(t.Context(), input, key)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc = New(db, sandbox.Source{}, "http://localhost:8080")
	svc.Now = func() time.Time { return now }
	next, err := svc.ConnectWithKey(t.Context(), input, key)
	if err != nil || next.AuthorizationURL != first.AuthorizationURL {
		t.Fatal(next, err)
	}
	link, _ := url.Parse(next.AuthorizationURL)
	if _, err = svc.Consent(t.Context(), link.Query().Get("token")); err != nil {
		t.Fatal("recovered token rejected", err)
	}
	now = now.Add(AuthorizationTTL)
	next, err = svc.ConnectWithKey(t.Context(), input, key)
	if err != nil || next.AuthorizationURL != "" || next.Connection.Status != "expired" || next.Connection.ID != first.Connection.ID {
		t.Fatal(next, err)
	}
}
