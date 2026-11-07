package config

import "testing"

func TestConfiguration(t *testing.T) {
	for _, key := range []string{"RANJIT_ADDR", "RANJIT_BASE_URL", "RANJIT_API_KEY", "RANJIT_DATABASE", "RANJIT_PROVIDER", "RANJIT_SYNC_INTERVAL"} {
		t.Setenv(key, "")
	}
	if _, err := Load(); err == nil {
		t.Fatal("missing API key accepted")
	}
	t.Setenv("RANJIT_API_KEY", "test-key-with-at-least-32-random-bytes")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"RANJIT_BASE_URL", "http://example.com"},
		{"RANJIT_BASE_URL", "https://example.com/callback"},
		{"RANJIT_BASE_URL", "https://user:secret@example.com"},
		{"RANJIT_BASE_URL", "https://example.com?token=x"},
		{"RANJIT_PROVIDER", "nubank"},
		{"RANJIT_SYNC_INTERVAL", "0s"},
		{"RANJIT_ADDR", "8080"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
