package config

import "testing"

func TestSicoobAuthenticationModes(t *testing.T) {
	for _, key := range []string{"RANJIT_ADDR", "RANJIT_BASE_URL", "RANJIT_DATABASE", "RANJIT_SYNC_INTERVAL", "RANJIT_SICOOB_ENV", "RANJIT_SICOOB_LOOKBACK_DAYS", "RANJIT_SICOOB_CERT_FILE", "RANJIT_SICOOB_KEY_FILE"} {
		t.Setenv(key, "")
	}
	t.Setenv("RANJIT_API_KEY", "test-key-with-at-least-32-random-bytes")
	t.Setenv("RANJIT_PROVIDER", "sicoob")
	t.Setenv("RANJIT_SICOOB_CLIENT_ID", "test-app")
	t.Setenv("RANJIT_SICOOB_ACCOUNT", "123456")
	t.Setenv("RANJIT_SICOOB_ACCESS_TOKEN_FILE", "/local/token")
	cfg, err := Load()
	if err != nil || cfg.Sicoob.Environment != "sandbox" || cfg.Sicoob.LookbackDays != 7 {
		t.Fatal(cfg.Sicoob, err)
	}
	t.Setenv("RANJIT_SICOOB_ENV", "production")
	if _, err := Load(); err == nil {
		t.Fatal("production accepted sandbox token")
	}
	t.Setenv("RANJIT_SICOOB_ACCESS_TOKEN_FILE", "")
	t.Setenv("RANJIT_SICOOB_CERT_FILE", "/local/cert.pem")
	t.Setenv("RANJIT_SICOOB_KEY_FILE", "/local/key.pem")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"RANJIT_SICOOB_ENV": "unknown", "RANJIT_SICOOB_LOOKBACK_DAYS": "91", "RANJIT_SICOOB_ACCOUNT": "", "RANJIT_SICOOB_CLIENT_ID": "", "RANJIT_SICOOB_CERT_FILE": "", "RANJIT_SICOOB_KEY_FILE": ""} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid Sicoob configuration accepted")
			}
		})
	}
}
