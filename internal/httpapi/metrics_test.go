package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/httpapi"
)

func TestOperationsAuthenticationAndBoundedMetrics(t *testing.T) {
	client, service, _, _ := setup(t)
	now := service.Now()
	due := now.Add(-30 * time.Second)
	op := &ranjit.OperationsStatus{Database: "ready", ObservedAt: now,
		Workers:   map[string]ranjit.WorkerStatus{"sync": {State: "idle", Healthy: true, Steps: 42}, "webhook": {State: "stalled"}},
		SyncQueue: ranjit.QueueStatus{Counts: map[string]int64{"queued": 2, "private-customer": 7}, OldestDueAt: &due, RecentFailed: 1},
	}
	handler := httpapi.New(service, key, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Operations(func(context.Context) ranjit.Diagnostics {
		return ranjit.Diagnostics{Version: "v0.4.0", Operations: op, UnavailableProfiles: []string{"private-profile"}}
	}))
	client.HTTPClient.Transport = handlerTransport{handler: handler}
	for _, path := range []string{"/metrics", "/v1/diagnostics"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatal(path, response.Code)
		}
	}
	diagnostics, err := client.Diagnostics(t.Context())
	if err != nil || diagnostics.Operations.SyncQueue.Counts["queued"] != 2 || diagnostics.Version != "v0.4.0" {
		t.Fatal(diagnostics, err)
	}
	request := httptest.NewRequest("GET", "/metrics", nil)
	request.Header.Set("Authorization", "Bearer "+key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatal(response.Code, response.Header())
	}
	for _, want := range []string{`ranjit_worker_healthy{worker="sync"} 1`, `ranjit_worker_healthy{worker="webhook"} 0`, `ranjit_worker_steps_total{worker="sync"} 42`, `ranjit_queue_oldest_due_seconds{queue="sync"} 30`} {
		if !strings.Contains(body, want) {
			t.Fatal("missing metric", want)
		}
	}
	for _, secret := range []string{key, "private-customer", "private-profile"} {
		if strings.Contains(body, secret) {
			t.Fatal("unbounded or sensitive metric label")
		}
	}
	op.Database = "unavailable"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 503 || strings.Contains(response.Body.String(), "ranjit_queue_items") {
		t.Fatal("unavailable database exported misleading metrics")
	}
}
