package httpapi_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/httpapi"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
	"github.com/getcortexlabs/ranjit/internal/service"
	"github.com/getcortexlabs/ranjit/internal/store"
	"github.com/getcortexlabs/ranjit/internal/worker"
)

const key = "test-key-for-ranjit-with-at-least-32-bytes"

func setup(t *testing.T) (*ranjit.Client, *service.Service, *worker.Worker, *bytes.Buffer) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	source := sandbox.Source{}
	s := service.New(db, source, "")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	s.BaseURL = "http://localhost:8080"
	client, err := ranjit.NewClient(s.BaseURL, key)
	if err != nil {
		t.Fatal(err)
	}
	client.HTTPClient.Transport = handlerTransport{handler: httpapi.New(s, key, logger)}
	w := worker.New(db, source, time.Hour, logger)
	w.Now = s.Now
	return client, s, w, logs
}

func connect(t *testing.T, c *ranjit.Client, customer string) ranjit.ConnectResponse {
	t.Helper()
	result, err := c.Connect(t.Context(), ranjit.ConnectRequest{CustomerID: customer, InstitutionID: "sandbox-bank"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func decide(t *testing.T, client *ranjit.Client, result ranjit.ConnectResponse, decision, origin string) (int, string) {
	t.Helper()
	u, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"token": {u.Query().Get("token")}, "decision": {decision}}
	u.RawQuery = ""
	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func TestConsentSyncPaginationAndRevocation(t *testing.T) {
	c, s, w, logs := setup(t)
	result := connect(t, c, "customer-secret-123")
	if _, err := c.Accounts(t.Context(), result.Connection.ID, "", 50); err == nil {
		t.Fatal("pending connection exposed data")
	}
	if status, body := decide(t, c, result, "approve", s.BaseURL); status != http.StatusOK {
		t.Fatalf("approve %d: %s", status, body)
	}
	if status, _ := decide(t, c, result, "approve", s.BaseURL); status != http.StatusGone {
		t.Fatalf("token replay = %d", status)
	}
	job, err := c.Sync(t.Context(), result.Connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	job, err = c.Job(t.Context(), job.ID)
	if err != nil || job.Status != "succeeded" {
		t.Fatalf("job = %+v, %v", job, err)
	}
	first, err := c.Accounts(t.Context(), result.Connection.ID, "", 1)
	if err != nil || len(first.Data) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, err := c.Accounts(t.Context(), result.Connection.ID, first.NextCursor, 1)
	if err != nil || len(second.Data) != 1 || second.NextCursor != "" || first.Data[0].ID == second.Data[0].ID {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	count := 0
	for _, a := range append(first.Data, second.Data...) {
		cursor := ""
		for {
			page, err := c.Transactions(t.Context(), a.ID, cursor, 1)
			if err != nil {
				t.Fatal(err)
			}
			count += len(page.Data)
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
	}
	if count != 4 {
		t.Fatalf("transactions = %d", count)
	}
	// Same upstream IDs in a different customer's connection must remain independent.
	other := connect(t, c, "customer-2")
	if status, _ := decide(t, c, other, "approve", s.BaseURL); status != 200 {
		t.Fatal(status)
	}
	if err = w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	otherAccounts, err := c.Accounts(t.Context(), other.Connection.ID, "", 50)
	if err != nil || otherAccounts.Data[0].ID == first.Data[0].ID {
		t.Fatalf("connection IDs collided: %v", err)
	}
	if err = c.Revoke(t.Context(), result.Connection.ID); err != nil {
		t.Fatal(err)
	}
	if err = c.Revoke(t.Context(), result.Connection.ID); err != nil {
		t.Fatalf("revocation is not idempotent: %v", err)
	}
	if _, err = c.Transactions(t.Context(), first.Data[0].ID, "", 50); err == nil {
		t.Fatal("revoked data still accessible")
	}
	if _, err = c.Sync(t.Context(), result.Connection.ID); err == nil {
		t.Fatal("revoked sync accepted")
	}
	if _, err = c.Accounts(t.Context(), other.Connection.ID, "", 50); err != nil {
		t.Fatalf("revocation affected another connection: %v", err)
	}
	u, _ := url.Parse(result.AuthorizationURL)
	for _, secret := range []string{key, "customer-secret-123", u.Query().Get("token")} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("request logs leaked a secret")
		}
	}
}

func TestConsentPageIsEscapedAndGetDoesNotAuthorize(t *testing.T) {
	c, s, _, _ := setup(t)
	result := connect(t, c, "<script>alert(1)</script>")
	resp, err := c.HTTPClient.Get(result.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "<script>") || !strings.Contains(string(body), "&lt;script&gt;") {
		t.Fatalf("unsafe template: %s", body)
	}
	if resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("token may leak in referrer")
	}
	connection, err := c.Connection(t.Context(), result.Connection.ID)
	if err != nil || connection.Status != "pending" {
		t.Fatalf("GET authorized connection: %+v %v", connection, err)
	}
	if status, _ := decide(t, c, result, "approve", "https://attacker.example"); status != 403 {
		t.Fatal(status)
	}
	if status, _ := decide(t, c, result, "deny", s.BaseURL); status != 200 {
		t.Fatal(status)
	}
	connection, err = c.Connection(t.Context(), result.Connection.ID)
	if err != nil || connection.Status != "denied" {
		t.Fatalf("denial failed: %+v %v", connection, err)
	}
}

func TestExpiredAuthorizationCannotBeConsumed(t *testing.T) {
	c, s, _, _ := setup(t)
	result := connect(t, c, "customer-1")
	s.Now = func() time.Time { return result.AuthorizationExpiresAt }
	if status, _ := decide(t, c, result, "approve", s.BaseURL); status != http.StatusGone {
		t.Fatalf("expired token accepted: %d", status)
	}
}

func TestAuthenticationAndStrictInput(t *testing.T) {
	c, s, _, _ := setup(t)
	wrong, _ := ranjit.NewClient(s.BaseURL, "wrong-key")
	wrong.HTTPClient = c.HTTPClient
	_, err := wrong.Institutions(t.Context())
	var apiErr *ranjit.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 || apiErr.RequestID == "" {
		t.Fatalf("auth = %v", err)
	}
	for _, input := range []string{
		`{"customer_id":"x","institution_id":"sandbox-bank","unknown":true}`,
		`{"customer_id":"x","institution_id":"sandbox-bank"} {}`,
		`{"customer_id":"x","institution_id":"real-bank"}`,
		`{"customer_id":" ","institution_id":"sandbox-bank"}`,
		`null`,
		`{"customer_id":"` + strings.Repeat("a", 20<<10) + `","institution_id":"sandbox-bank"}`,
	} {
		req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/v1/connections", strings.NewReader(input))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("invalid input accepted: %d", resp.StatusCode)
		}
	}
	if _, err = c.Connections(t.Context(), "customer", "", -1); err == nil {
		t.Fatal("invalid pagination accepted")
	}
}

// Run the real HTTP handler and database without binding a machine-local port.
type handlerTransport struct{ handler http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		defer req.Body.Close()
	}
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	response := recorder.Result()
	response.Request = req
	return response, nil
}
