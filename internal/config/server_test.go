package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestServerConfiguration(t *testing.T) {
	t.Setenv("RANJIT_CONFIG", "")
	t.Setenv("RANJIT_API_KEY", strings.Repeat("k", 32))
	t.Setenv("RANJIT_PROVIDER", "")
	t.Setenv("RANJIT_DATABASE", "/data/ranjit.db") // Docker's default must not override PostgreSQL.
	t.Setenv("RANJIT_DATABASE_URL", "postgres://app:secret@db/ranjit")
	t.Setenv("RANJIT_PROFILES_JSON", `[{"name":"test","provider":"sandbox"}]`)
	c, err := Load()
	if err != nil || c.Database != "postgres://app:secret@db/ranjit" || c.Provider != "profiles" || len(c.Profiles) != 1 {
		t.Fatal("server configuration not loaded", err)
	}
	for _, tc := range []struct{ key, value string }{
		{"RANJIT_CONFIG", "/some/config.json"},
		{"RANJIT_API_KEY", ""},
		{"RANJIT_DATABASE_URL", "postgres://user:never-print-me@localhost:bad/db"},
		{"RANJIT_DATABASE_URL", "sqlite:///data.db"},
		{"RANJIT_PROVIDER", "sandbox"},
		{"RANJIT_PROFILES_JSON", `[{"name":"one","provider":"sandbox","typo":true}]`},
		{"RANJIT_PROFILES_JSON", `[{"name":"one","provider":"sandbox"},{"name":"one","provider":"sandbox"}]`},
		{"RANJIT_PROFILES_JSON", `[] {}`},
		{"RANJIT_PROFILES_JSON", `null`},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || strings.Contains(err.Error(), "never-print-me") {
				t.Fatal("invalid server configuration accepted or credential exposed")
			}
		})
	}
	t.Setenv("RANJIT_PROFILES_JSON", `[]`)
	c, err = Load()
	if err != nil || c.Provider != "profiles" || len(c.Profiles) != 0 {
		t.Fatal("file-only installation failed", err)
	}
}

func TestEnvironmentReferencesRemainReferences(t *testing.T) {
	c := NewPersonal()
	c.Profiles = []Profile{{Name: "bank", Provider: "efi", LookbackDays: 7, ClientSecretFile: "env:BANK_SECRET", CertificateFile: "env:BANK_CERT", PrivateKeyFile: "key.pem"}}
	runtime := c.Runtime(filepath.Join(t.TempDir(), "config.json"))
	p := runtime.Profiles[0]
	if p.ClientSecretFile != "env:BANK_SECRET" || p.CertificateFile != "env:BANK_CERT" || !filepath.IsAbs(p.PrivateKeyFile) {
		t.Fatal("credential resolution changed source type")
	}
}
