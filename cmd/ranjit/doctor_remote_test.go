package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteDoctorDoesNotOpenLocalConfiguration(t *testing.T) {
	for _, scenario := range []string{"healthy", "unauthorized", "database", "worker", "legacy", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/healthz":
					w.Write([]byte(`{"status":"ok"}`))
				case "/readyz":
					if scenario == "database" {
						http.Error(w, "do-not-print-database-secret", 503)
					} else {
						w.Write([]byte(`{"status":"ready"}`))
					}
				case "/v1/institutions":
					if scenario == "unauthorized" {
						http.Error(w, "do-not-print-key", 401)
					} else {
						w.Write([]byte(`{"data":[]}`))
					}
				case "/v1/diagnostics":
					if scenario == "missing" {
						http.NotFound(w, r)
						return
					}
					if scenario == "legacy" {
						w.Write([]byte(`{"version":"v0.3.0"}`))
						return
					}
					healthy := scenario != "worker"
					json.NewEncoder(w).Encode(map[string]any{"version": "v0.4.0", "operations": map[string]any{"database": "ready", "workers": map[string]any{"sync": map[string]any{"state": "idle", "healthy": healthy}, "webhook": map[string]any{"state": "disabled", "healthy": true}}}})
				}
			}))
			defer server.Close()
			absent := filepath.Join(t.TempDir(), "must-not-create", "config.json")
			t.Setenv("RANJIT_CONFIG", absent)
			t.Setenv("RANJIT_URL", server.URL)
			t.Setenv("RANJIT_API_KEY", "private-test-secret")
			var out bytes.Buffer
			err := (cli{out: &out}).doctor(t.Context(), []string{"--remote", "--json"})
			var report doctorReport
			if e := json.Unmarshal(out.Bytes(), &report); e != nil {
				t.Fatal(e, out.String())
			}
			failed := scenario == "unauthorized" || scenario == "database" || scenario == "worker"
			if (err != nil) != failed || report.SchemaVersion != 1 {
				t.Fatal(report, err)
			}
			if strings.Contains(out.String(), "do-not-print") || strings.Contains(out.String(), "private-test-secret") {
				t.Fatal("secret leaked")
			}
			if _, err := os.Stat(filepath.Dir(absent)); !os.IsNotExist(err) {
				t.Fatal("remote doctor touched local config", err)
			}
		})
	}
}

func TestRemoteDoctorTimeoutIsBoundedAndRedacted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	var out bytes.Buffer
	err := (cli{out: &out}).remoteDoctor(t.Context(), server.URL, "private-test-secret", 20*time.Millisecond, true)
	if err == nil || strings.Contains(out.String(), server.URL) || strings.Contains(out.String(), "private-test-secret") {
		t.Fatal(err, out.String())
	}
	var report doctorReport
	if err = json.Unmarshal(out.Bytes(), &report); err != nil || report.Checks[0].ID != "reachability" {
		t.Fatal(report, err)
	}
}
