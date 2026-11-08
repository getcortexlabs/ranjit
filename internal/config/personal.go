package config

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Profile struct {
	Name             string `json:"name"`
	Provider         string `json:"provider"`
	Environment      string `json:"environment,omitempty"`
	ClientID         string `json:"client_id,omitempty"`
	ClientSecretFile string `json:"client_secret_file,omitempty"`
	CertificateFile  string `json:"certificate_file,omitempty"`
	PrivateKeyFile   string `json:"private_key_file,omitempty"`
	AccessTokenFile  string `json:"access_token_file,omitempty"`
	Account          string `json:"account,omitempty"`
	LookbackDays     int    `json:"lookback_days,omitempty"`
}

type Personal struct {
	PreviousAPIKey          string    `json:"previous_api_key,omitempty"`
	PreviousAPIKeyExpiresAt time.Time `json:"previous_api_key_expires_at,omitzero"`
	WebhookURL              string    `json:"webhook_url,omitempty"`
	WebhookSecret           string    `json:"webhook_secret,omitempty"`
	Version                 int       `json:"version"`
	Database                string    `json:"database"`
	APIKey                  string    `json:"api_key"`
	Addr                    string    `json:"addr"`
	BaseURL                 string    `json:"base_url"`
	SyncInterval            string    `json:"sync_interval"`
	Profiles                []Profile `json:"profiles"`
}

var profileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func ValidName(name string) bool { return profileName.MatchString(name) }

func PersonalPath() (string, error) {
	if path := os.Getenv("RANJIT_CONFIG"); path != "" {
		return filepath.Abs(path)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ranjit", "config.json"), nil
}

func NewPersonal() Personal {
	return Personal{Version: 1, Database: env("RANJIT_DATABASE", "data/ranjit.db"), APIKey: rand.Text() + rand.Text(), Addr: env("RANJIT_ADDR", "127.0.0.1:8080"), BaseURL: env("RANJIT_BASE_URL", "http://localhost:8080"), SyncInterval: "1h", Profiles: []Profile{}}
}

func ReadPersonal(path string) (Personal, error) {
	var c Personal
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return c, errors.New("cannot read configuration (maximum 1 MiB)")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, errors.New("invalid config.json; check field names and JSON syntax")
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("unexpected content after configuration")
	}
	return c, c.Validate()
}

func (c Personal) Validate() error {
	if c.Version != 1 {
		return errors.New("unsupported configuration version")
	}
	if strings.Contains(c.Database, "://") {
		return errors.New("config.json requires a SQLite path; use RANJIT_DATABASE_URL and RANJIT_API_KEY for a PostgreSQL server")
	}
	if c.Database == "" {
		return errors.New("database path is required")
	}
	if err := validateServer(c.Addr, c.BaseURL, c.APIKey); err != nil {
		return err
	}
	if d, err := time.ParseDuration(c.SyncInterval); err != nil || d < time.Minute {
		return errors.New("sync_interval must be at least 1m")
	}
	if err := validatePrevious(c.APIKey, c.PreviousAPIKey, c.PreviousAPIKeyExpiresAt); err != nil {
		return err
	}
	if err := validateWebhook(c.WebhookURL, c.WebhookSecret); err != nil {
		return err
	}
	return validateProfiles(c.Profiles)
}

func validateProfiles(profiles []Profile) error {
	seen := map[string]bool{}
	for _, p := range profiles {
		if !ValidName(p.Name) || seen[p.Name] {
			return errors.New("profile names must be unique and contain letters, numbers, _ or - (maximum 64 characters)")
		}
		seen[p.Name] = true
		switch p.Provider {
		case "sandbox", "efi", "inter", "sicoob", "c6", "santander":
		default:
			return fmt.Errorf("profile %s: unknown provider", p.Name)
		}
		if p.Provider != "sandbox" && (p.LookbackDays < 1 || p.LookbackDays > 90) {
			return fmt.Errorf("profile %s: lookback_days must be between 1 and 90", p.Name)
		}
	}
	return nil
}

// SavePersonal replaces only complete, validated files. Never writes secret values to stdout.
func SavePersonal(path string, c Personal) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ranjit-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (c Personal) Runtime(path string) Config {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "env:") {
			return p
		}
		return filepath.Join(filepath.Dir(path), p)
	}
	profiles := append([]Profile(nil), c.Profiles...)
	for i := range profiles {
		p := &profiles[i]
		p.ClientSecretFile = resolve(p.ClientSecretFile)
		p.CertificateFile = resolve(p.CertificateFile)
		p.PrivateKeyFile = resolve(p.PrivateKeyFile)
		p.AccessTokenFile = resolve(p.AccessTokenFile)
	}
	interval, _ := time.ParseDuration(c.SyncInterval)
	return Config{PersonalFile: path, PreviousAPIKey: c.PreviousAPIKey, PreviousAPIKeyExpiresAt: c.PreviousAPIKeyExpiresAt, WebhookURL: c.WebhookURL, WebhookSecret: c.WebhookSecret, Addr: c.Addr, BaseURL: c.BaseURL, APIKey: c.APIKey, Database: resolve(c.Database), SyncInterval: interval, Provider: "profiles", Profiles: profiles}
}
