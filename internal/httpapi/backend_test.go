package httpapi_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/config"
	"github.com/getcortexlabs/ranjit/internal/httpapi"
)

func TestSDKConnectReplayDoesNotReopenConsent(t *testing.T) {
	client, s, _, logs := setup(t)
	input := ranjit.ConnectRequest{CustomerID: "private-customer", InstitutionID: "sandbox-bank"}
	key := "persistent-operation-1234"
	first, err := client.ConnectWithKey(t.Context(), input, key)
	if err != nil {
		t.Fatal(err)
	}
	next, err := client.ConnectWithKey(t.Context(), input, key)
	if err != nil || next.Connection.ID != first.Connection.ID || next.AuthorizationURL != first.AuthorizationURL {
		t.Fatal(next, err)
	}
	input.CustomerID = "another-customer"
	_, err = client.ConnectWithKey(t.Context(), input, key)
	var apiError *ranjit.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 409 || apiError.Code != "idempotency_conflict" {
		t.Fatal(err)
	}
	if status, _ := decide(t, client, first, "approve", s.BaseURL); status != 200 {
		t.Fatal(status)
	}
	input.CustomerID = "private-customer"
	next, err = client.ConnectWithKey(t.Context(), input, key)
	if err != nil || next.Connection.Status != "active" || next.AuthorizationURL != "" {
		t.Fatal("consumed authorization replayed", next, err)
	}
	if strings.Contains(logs.String(), key) || strings.Contains(logs.String(), input.CustomerID) {
		t.Fatal("request metadata leaked")
	}
}

func TestAPIKeysReloadAndExpireWithoutRestart(t *testing.T) {
	_, s, _, _ := setup(t)
	p := config.NewPersonal()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.SavePersonal(path, p); err != nil {
		t.Fatal(err)
	}
	cfg := p.Runtime(path)
	transport := handlerTransport{handler: httpapi.New(s, p.APIKey, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.APIKeys(cfg.APIKeys))}
	check := func(key string, want int) {
		t.Helper()
		req, _ := http.NewRequest("GET", "http://localhost:8080/v1/institutions", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("authentication status %d, want %d", resp.StatusCode, want)
		}
	}
	old := p.APIKey
	check(old, 200)
	p.PreviousAPIKey, p.PreviousAPIKeyExpiresAt = old, s.Now().Add(time.Minute)
	p.APIKey = strings.Repeat("n", 40)
	if err := config.SavePersonal(path, p); err != nil {
		t.Fatal(err)
	}
	check(old, 200)
	check(p.APIKey, 200)
	now := s.Now().Add(time.Minute)
	s.Now = func() time.Time { return now }
	check(old, 401)
	check(p.APIKey, 200)
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	check(p.APIKey, 401)
	check(old, 401)
}

func TestWebhookDeliverySDKAndAuthentication(t *testing.T) {
	client, s, w, _ := setup(t)
	s.Store.EnableWebhooks("https://backend.example/events")
	result := connect(t, client, "customer")
	if code, _ := decide(t, client, result, "approve", s.BaseURL); code != 200 {
		t.Fatal(code)
	}
	if err := w.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	page, err := client.WebhookDeliveries(t.Context(), "", 1)
	if err != nil || len(page.Data) != 1 || page.Data[0].Event.Type != "sync.succeeded" {
		t.Fatal(page, err)
	}
	err = client.RetryWebhook(t.Context(), page.Data[0].Event.ID)
	var apiError *ranjit.APIError
	if !errors.As(err, &apiError) || apiError.Code != "webhook_not_retryable" {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", s.BaseURL+"/v1/webhooks/deliveries", nil)
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("delivery metadata unauthenticated", resp.StatusCode)
	}
}
