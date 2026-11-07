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

func TestEFICredentialsAndEnvironment(t *testing.T) {
	for _, key := range []string{"RANJIT_ADDR", "RANJIT_BASE_URL", "RANJIT_DATABASE", "RANJIT_SYNC_INTERVAL", "RANJIT_EFI_ENV", "RANJIT_EFI_LOOKBACK_DAYS"} {
		t.Setenv(key, "")
	}
	t.Setenv("RANJIT_API_KEY", "test-key-with-at-least-32-random-bytes")
	t.Setenv("RANJIT_PROVIDER", "efi")
	for key, value := range map[string]string{
		"RANJIT_EFI_CLIENT_ID":          "test-app",
		"RANJIT_EFI_CLIENT_SECRET_FILE": "/local/secret",
		"RANJIT_EFI_CERT_FILE":          "/local/cert.pem",
		"RANJIT_EFI_KEY_FILE":           "/local/key.pem",
	} {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil || cfg.Provider != "efi" || cfg.EFI.Environment != "homologation" || cfg.EFI.LookbackDays != 90 {
		t.Fatalf("unexpected Efí defaults: %+v %v", cfg.EFI, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"RANJIT_EFI_CLIENT_ID", ""},
		{"RANJIT_EFI_CLIENT_SECRET_FILE", ""},
		{"RANJIT_EFI_CERT_FILE", ""},
		{"RANJIT_EFI_KEY_FILE", ""},
		{"RANJIT_EFI_ENV", "unknown"},
		{"RANJIT_EFI_LOOKBACK_DAYS", "0"},
		{"RANJIT_EFI_LOOKBACK_DAYS", "91"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid Efí configuration accepted")
			}
		})
	}
}

func TestInterCredentialsAndEnvironment(t *testing.T) {
	for _, key := range []string{"RANJIT_ADDR", "RANJIT_BASE_URL", "RANJIT_DATABASE", "RANJIT_SYNC_INTERVAL", "RANJIT_INTER_ENV", "RANJIT_INTER_LOOKBACK_DAYS"} {
		t.Setenv(key, "")
	}
	t.Setenv("RANJIT_API_KEY", "test-key-with-at-least-32-random-bytes")
	t.Setenv("RANJIT_PROVIDER", "inter")
	for key, value := range map[string]string{
		"RANJIT_INTER_ACCOUNT":            "123456",
		"RANJIT_INTER_CLIENT_ID":          "test-app",
		"RANJIT_INTER_CLIENT_SECRET_FILE": "/local/secret",
		"RANJIT_INTER_CERT_FILE":          "/local/cert.pem",
		"RANJIT_INTER_KEY_FILE":           "/local/key.pem",
	} {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil || cfg.Provider != "inter" || cfg.Inter.Environment != "sandbox" || cfg.Inter.LookbackDays != 7 {
		t.Fatalf("unexpected Inter defaults: %+v %v", cfg.Inter, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"RANJIT_INTER_CLIENT_ID", ""},
		{"RANJIT_INTER_ACCOUNT", ""},
		{"RANJIT_INTER_CLIENT_SECRET_FILE", ""},
		{"RANJIT_INTER_CERT_FILE", ""},
		{"RANJIT_INTER_KEY_FILE", ""},
		{"RANJIT_INTER_ENV", "unknown"},
		{"RANJIT_INTER_LOOKBACK_DAYS", "0"},
		{"RANJIT_INTER_LOOKBACK_DAYS", "91"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid Inter configuration accepted")
			}
		})
	}
}
