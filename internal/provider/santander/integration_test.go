package santander

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/httpapi"
	"github.com/getcortexlabs/ranjit/internal/service"
	"github.com/getcortexlabs/ranjit/internal/store"
	"github.com/getcortexlabs/ranjit/internal/worker"
)

func TestAPIConnectSyncAndRevokeWithDirectCredentials(t *testing.T) {
	source := sourceFixture(t)
	source.client.Transport = bankTransport(t)
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := service.New(db, source, "http://localhost:8080")
	svc.Now = source.now
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	const apiKey = "ranjit-api-key-for-integration-tests"
	handler := httpapi.New(svc, apiKey, logger)
	client, err := ranjit.NewClient("http://localhost:8080", apiKey)
	if err != nil {
		t.Fatal(err)
	}
	client.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out.Result(), nil
	})
	institutions, err := client.Institutions(t.Context())
	if err != nil || len(institutions.Data) != 1 || institutions.Data[0].ConnectionMode != "api_credentials" {
		t.Fatalf("institutions = %+v %v", institutions, err)
	}
	source.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(401, `{"message":"private upstream details"}`), nil
	})
	_, err = client.Connect(t.Context(), ranjit.ConnectRequest{CustomerID: "local-user", InstitutionID: "santander"})
	var apiError *ranjit.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 502 || apiError.Code != "provider_credentials" || apiError.Message != ranjit.ErrProviderCredentials.Error() {
		t.Fatalf("credential rejection = %v", err)
	}
	connections, err := client.Connections(t.Context(), "local-user", "", 100)
	if err != nil || len(connections.Data) != 0 {
		t.Fatalf("failed verification created a connection: %+v %v", connections, err)
	}
	source.client.Transport = bankTransport(t)
	result, err := client.Connect(t.Context(), ranjit.ConnectRequest{CustomerID: "local-user", InstitutionID: "santander"})
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationURL != "" || !result.AuthorizationExpiresAt.IsZero() || result.Connection.Status != "active" || result.Connection.AuthorizationMode != "api_credentials" || result.Connection.CredentialID != "" {
		t.Fatalf("unexpected direct connection: %+v", result)
	}
	w := worker.New(db, source, time.Hour, logger)
	w.Now = source.now
	for range 2 {
		job, err := client.Sync(t.Context(), result.Connection.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err = w.Step(t.Context()); err != nil {
			t.Fatal(err)
		}
		job, err = client.Job(t.Context(), job.ID)
		if err != nil || job.Status != "succeeded" {
			t.Fatalf("job = %+v %v", job, err)
		}
		accounts, err := client.Accounts(t.Context(), result.Connection.ID, "", 100)
		if err != nil || len(accounts.Data) != 1 {
			t.Fatalf("accounts = %+v %v", accounts, err)
		}
		txs, err := client.Transactions(t.Context(), accounts.Data[0].ID, "", 100)
		if err != nil || len(txs.Data) != 3 {
			t.Fatalf("transactions = %+v %v", txs, err)
		}
	}
	if err = client.Revoke(t.Context(), result.Connection.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Sync(t.Context(), result.Connection.ID); err == nil {
		t.Fatal("revoked account synchronized")
	}
}
