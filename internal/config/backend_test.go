package config

import (
	"strings"
	"testing"
	"time"
)

func TestBackendEnvironmentValidation(t *testing.T) {
	t.Setenv("RANJIT_CONFIG", "")
	t.Setenv("RANJIT_API_KEY", strings.Repeat("k", 40))
	t.Setenv("RANJIT_PROVIDER", "sandbox")
	t.Setenv("RANJIT_WEBHOOK_URL", "https://backend.example/webhooks/ranjit")
	t.Setenv("RANJIT_WEBHOOK_SECRET", strings.Repeat("s", 40))
	t.Setenv("RANJIT_API_KEY_PREVIOUS", strings.Repeat("p", 40))
	t.Setenv("RANJIT_API_KEY_PREVIOUS_EXPIRES_AT", "2026-11-09T12:00:00Z")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cfg.APIKeys(time.Date(2026, 11, 9, 11, 59, 0, 0, time.UTC))
	if err != nil || len(keys) != 2 {
		t.Fatal(err)
	}
	keys, err = cfg.APIKeys(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))
	if err != nil || len(keys) != 1 {
		t.Fatal("expired key accepted", err)
	}
	for _, endpoint := range []string{"http://public.example/events", "https://user:password@example.com/events", "https://example.com/events?secret=bad", "https://example.com/events#fragment", "file:///tmp/events", ""} {
		t.Run(endpoint, func(t *testing.T) {
			t.Setenv("RANJIT_WEBHOOK_URL", endpoint)
			if _, err := Load(); err == nil {
				t.Fatal("invalid webhook accepted")
			}
		})
	}
	t.Setenv("RANJIT_API_KEY_PREVIOUS_EXPIRES_AT", "")
	if _, err = Load(); err == nil {
		t.Fatal("unbounded previous key accepted")
	}
}
