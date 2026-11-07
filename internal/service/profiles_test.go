package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
	"github.com/getcortexlabs/ranjit/internal/store"
)

type profileSource struct {
	key   string
	calls int
}

func (s *profileSource) CredentialID() string         { return s.key }
func (s *profileSource) Verify(context.Context) error { s.calls++; return nil }
func (s *profileSource) Institutions() []ranjit.Institution {
	return []ranjit.Institution{{ID: "inter", Provider: "inter", ConnectionMode: "api_credentials"}}
}
func (s *profileSource) Fetch(_ context.Context, c ranjit.Connection) (provider.Snapshot, error) {
	if c.InstitutionID != "inter" || c.CredentialID != s.key {
		return provider.Snapshot{}, ranjit.ErrProviderCredentials
	}
	return provider.Snapshot{}, nil
}

func TestProfilesKeepCredentialBindings(t *testing.T) {
	a, b := &profileSource{key: "account-a"}, &profileSource{key: "account-b"}
	registry := provider.NewRegistry()
	if err := registry.Add("a", a); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add("b", b); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db, registry, "http://localhost:8080")
	response, err := svc.Connect(t.Context(), ranjit.ConnectRequest{CustomerID: "test", InstitutionID: "a:inter"})
	if err != nil {
		t.Fatal(err)
	}
	if a.calls != 1 || b.calls != 0 {
		t.Fatal("verified wrong account")
	}
	if _, err = registry.Fetch(t.Context(), response.Connection); err != nil {
		t.Fatal(err)
	}
	response.Connection.InstitutionID = "b:inter"
	if _, err = registry.Fetch(t.Context(), response.Connection); !errors.Is(err, ranjit.ErrProviderCredentials) {
		t.Fatal("cross-profile binding accepted", err)
	}
	stored, err := db.Connection(t.Context(), response.Connection.ID, time.Now())
	if err != nil || stored.CredentialID != "account-a" {
		t.Fatal("lost binding", err)
	}
}
