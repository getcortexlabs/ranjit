package config

import "testing"

func TestC6CredentialsAndEnvironment(t *testing.T) {
	for _, key := range []string{"RANJIT_ADDR", "RANJIT_BASE_URL", "RANJIT_DATABASE", "RANJIT_SYNC_INTERVAL", "RANJIT_C6_ENV", "RANJIT_C6_LOOKBACK_DAYS"} {
		t.Setenv(key, "")
	}
	t.Setenv("RANJIT_API_KEY", "test-key-with-at-least-32-random-bytes")
	t.Setenv("RANJIT_PROVIDER", "c6")
	for key, value := range map[string]string{
		"RANJIT_C6_CLIENT_ID":          "test-app",
		"RANJIT_C6_CLIENT_SECRET_FILE": "/local/secret",
		"RANJIT_C6_CERT_FILE":          "/local/cert.pem",
		"RANJIT_C6_KEY_FILE":           "/local/key.pem",
	} {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil || cfg.Provider != "c6" || cfg.C6.Environment != "sandbox" || cfg.C6.LookbackDays != 7 {
		t.Fatalf("unexpected C6 defaults: %+v %v", cfg.C6, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"RANJIT_C6_CLIENT_ID", ""},
		{"RANJIT_C6_CLIENT_SECRET_FILE", ""},
		{"RANJIT_C6_CERT_FILE", ""},
		{"RANJIT_C6_KEY_FILE", ""},
		{"RANJIT_C6_ENV", "unknown"},
		{"RANJIT_C6_LOOKBACK_DAYS", "0"},
		{"RANJIT_C6_LOOKBACK_DAYS", "91"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid C6 configuration accepted")
			}
		})
	}
}
