package config

import "testing"

func TestSantanderCredentialsAndEnvironment(t *testing.T) {
	for _, key := range []string{"RANJIT_ADDR", "RANJIT_BASE_URL", "RANJIT_DATABASE", "RANJIT_SYNC_INTERVAL", "RANJIT_SANTANDER_ENV", "RANJIT_SANTANDER_LOOKBACK_DAYS"} {
		t.Setenv(key, "")
	}
	t.Setenv("RANJIT_API_KEY", "test-key-with-at-least-32-random-bytes")
	t.Setenv("RANJIT_PROVIDER", "santander")
	for key, value := range map[string]string{
		"RANJIT_SANTANDER_ACCOUNT":            "0001.000000123456",
		"RANJIT_SANTANDER_CLIENT_ID":          "test-app",
		"RANJIT_SANTANDER_CLIENT_SECRET_FILE": "/local/secret",
		"RANJIT_SANTANDER_CERT_FILE":          "/local/cert.pem",
		"RANJIT_SANTANDER_KEY_FILE":           "/local/key.pem",
	} {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil || cfg.Provider != "santander" || cfg.Santander.Environment != "sandbox" || cfg.Santander.LookbackDays != 7 {
		t.Fatalf("unexpected Santander defaults: %+v %v", cfg.Santander, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"RANJIT_SANTANDER_CLIENT_ID", ""},
		{"RANJIT_SANTANDER_ACCOUNT", ""},
		{"RANJIT_SANTANDER_CLIENT_SECRET_FILE", ""},
		{"RANJIT_SANTANDER_CERT_FILE", ""},
		{"RANJIT_SANTANDER_KEY_FILE", ""},
		{"RANJIT_SANTANDER_ENV", "unknown"},
		{"RANJIT_SANTANDER_LOOKBACK_DAYS", "0"},
		{"RANJIT_SANTANDER_LOOKBACK_DAYS", "91"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid Santander configuration accepted")
			}
		})
	}
}
