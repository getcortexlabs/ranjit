package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit/internal/provider/c6"
	"github.com/getcortexlabs/ranjit/internal/provider/efi"
	"github.com/getcortexlabs/ranjit/internal/provider/inter"
	"github.com/getcortexlabs/ranjit/internal/provider/santander"
	"github.com/getcortexlabs/ranjit/internal/provider/sicoob"
)

type Config struct {
	PreviousAPIKey          string
	PreviousAPIKeyExpiresAt time.Time
	PersonalFile            string
	WebhookURL              string
	WebhookSecret           string
	Profiles                []Profile
	Addr                    string
	BaseURL                 string
	APIKey                  string
	Database                string
	SyncInterval            time.Duration
	Provider                string
	EFI                     efi.Config
	Inter                   inter.Config
	Sicoob                  sicoob.Config
	Santander               santander.Config
	C6                      c6.Config
}

func Load() (Config, error) {
	serverURL := os.Getenv("RANJIT_DATABASE_URL")
	profilesJSON, hasProfiles := os.LookupEnv("RANJIT_PROFILES_JSON")
	if serverURL != "" || hasProfiles {
		if os.Getenv("RANJIT_CONFIG") != "" || os.Getenv("RANJIT_API_KEY") == "" {
			return Config{}, fmt.Errorf("server environment configuration requires RANJIT_API_KEY and cannot be combined with RANJIT_CONFIG")
		}
	}

	if os.Getenv("RANJIT_CONFIG") != "" || os.Getenv("RANJIT_API_KEY") == "" {
		path, err := PersonalPath()
		if err != nil {
			return Config{}, err
		}
		personal, err := ReadPersonal(path)
		if err == nil {
			return personal.Runtime(path), nil
		}
		if os.Getenv("RANJIT_CONFIG") != "" || !os.IsNotExist(err) {
			return Config{}, err
		}
	}
	c := Config{Addr: env("RANJIT_ADDR", "127.0.0.1:8080"), BaseURL: strings.TrimRight(env("RANJIT_BASE_URL", "http://localhost:8080"), "/"), APIKey: os.Getenv("RANJIT_API_KEY"), Database: env("RANJIT_DATABASE", "./data/ranjit.db")}
	if len(c.APIKey) < 32 || strings.ContainsAny(c.APIKey, " \t\r\n") {
		return c, fmt.Errorf("RANJIT_API_KEY must contain at least 32 bytes without whitespace; run 'ranjit init' to generate one")
	}
	if serverURL != "" {
		u, err := url.Parse(serverURL)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Fragment != "" {
			return c, fmt.Errorf("RANJIT_DATABASE_URL must be a PostgreSQL URL")
		}
		c.Database = serverURL
	} else if strings.Contains(c.Database, "://") {
		return c, fmt.Errorf("use RANJIT_DATABASE_URL for PostgreSQL; RANJIT_DATABASE is a SQLite file path")
	}
	c.PreviousAPIKey = os.Getenv("RANJIT_API_KEY_PREVIOUS")
	if value := os.Getenv("RANJIT_API_KEY_PREVIOUS_EXPIRES_AT"); value != "" {
		var err error
		c.PreviousAPIKeyExpiresAt, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return c, fmt.Errorf("RANJIT_API_KEY_PREVIOUS_EXPIRES_AT must use RFC3339")
		}
	}
	if err := validatePrevious(c.APIKey, c.PreviousAPIKey, c.PreviousAPIKeyExpiresAt); err != nil {
		return c, err
	}
	c.WebhookURL, c.WebhookSecret = os.Getenv("RANJIT_WEBHOOK_URL"), os.Getenv("RANJIT_WEBHOOK_SECRET")
	if err := validateWebhook(c.WebhookURL, c.WebhookSecret); err != nil {
		return c, err
	}
	c.Provider = env("RANJIT_PROVIDER", "sandbox")
	if hasProfiles {
		if os.Getenv("RANJIT_PROVIDER") != "" {
			return c, fmt.Errorf("use either RANJIT_PROFILES_JSON or RANJIT_PROVIDER")
		}
		if len(profilesJSON) > 1<<20 || !strings.HasPrefix(strings.TrimSpace(profilesJSON), "[") {
			return c, fmt.Errorf("RANJIT_PROFILES_JSON must be a JSON array (maximum 1 MiB)")
		}
		d := json.NewDecoder(strings.NewReader(profilesJSON))
		d.DisallowUnknownFields()
		if err := d.Decode(&c.Profiles); err != nil || d.Decode(new(any)) != io.EOF {
			return c, fmt.Errorf("invalid RANJIT_PROFILES_JSON; check field names and JSON syntax")
		}
		if err := validateProfiles(c.Profiles); err != nil {
			return c, err
		}
		c.Provider = "profiles"
	}
	if c.Provider != "profiles" && c.Provider != "sandbox" && c.Provider != "efi" && c.Provider != "inter" && c.Provider != "sicoob" && c.Provider != "c6" && c.Provider != "santander" {
		return c, fmt.Errorf("RANJIT_PROVIDER must be sandbox, efi, inter, sicoob, c6 or santander")
	}
	if c.Provider == "efi" {
		days, err := strconv.Atoi(env("RANJIT_EFI_LOOKBACK_DAYS", "90"))
		if err != nil || days < 1 || days > 90 {
			return c, fmt.Errorf("RANJIT_EFI_LOOKBACK_DAYS must be between 1 and 90")
		}
		c.EFI = efi.Config{Environment: env("RANJIT_EFI_ENV", "homologation"), ClientID: os.Getenv("RANJIT_EFI_CLIENT_ID"), ClientSecretFile: os.Getenv("RANJIT_EFI_CLIENT_SECRET_FILE"), CertificateFile: os.Getenv("RANJIT_EFI_CERT_FILE"), PrivateKeyFile: os.Getenv("RANJIT_EFI_KEY_FILE"), LookbackDays: days}
		if c.EFI.Environment != "homologation" && c.EFI.Environment != "production" {
			return c, fmt.Errorf("RANJIT_EFI_ENV must be homologation or production")
		}
		if c.EFI.ClientID == "" || c.EFI.ClientSecretFile == "" || c.EFI.CertificateFile == "" || c.EFI.PrivateKeyFile == "" {
			return c, fmt.Errorf("Efí requires RANJIT_EFI_CLIENT_ID, RANJIT_EFI_CLIENT_SECRET_FILE, RANJIT_EFI_CERT_FILE and RANJIT_EFI_KEY_FILE")
		}
	}
	if c.Provider == "inter" {
		days, err := strconv.Atoi(env("RANJIT_INTER_LOOKBACK_DAYS", "7"))
		if err != nil || days < 1 || days > 90 {
			return c, fmt.Errorf("RANJIT_INTER_LOOKBACK_DAYS must be between 1 and 90")
		}
		c.Inter = inter.Config{Environment: env("RANJIT_INTER_ENV", "sandbox"), ClientID: os.Getenv("RANJIT_INTER_CLIENT_ID"), ClientSecretFile: os.Getenv("RANJIT_INTER_CLIENT_SECRET_FILE"), CertificateFile: os.Getenv("RANJIT_INTER_CERT_FILE"), PrivateKeyFile: os.Getenv("RANJIT_INTER_KEY_FILE"), Account: os.Getenv("RANJIT_INTER_ACCOUNT"), LookbackDays: days}
		if c.Inter.Environment != "sandbox" && c.Inter.Environment != "production" {
			return c, fmt.Errorf("RANJIT_INTER_ENV must be sandbox or production")
		}
		if c.Inter.ClientID == "" || c.Inter.ClientSecretFile == "" || c.Inter.CertificateFile == "" || c.Inter.PrivateKeyFile == "" || c.Inter.Account == "" {
			return c, fmt.Errorf("Inter requires RANJIT_INTER_CLIENT_ID, RANJIT_INTER_CLIENT_SECRET_FILE, RANJIT_INTER_CERT_FILE, RANJIT_INTER_KEY_FILE and RANJIT_INTER_ACCOUNT")
		}
	}
	if c.Provider == "santander" {
		days, err := strconv.Atoi(env("RANJIT_SANTANDER_LOOKBACK_DAYS", "7"))
		if err != nil || days < 1 || days > 90 {
			return c, fmt.Errorf("RANJIT_SANTANDER_LOOKBACK_DAYS must be between 1 and 90")
		}
		c.Santander = santander.Config{Environment: env("RANJIT_SANTANDER_ENV", "sandbox"), ClientID: os.Getenv("RANJIT_SANTANDER_CLIENT_ID"), ClientSecretFile: os.Getenv("RANJIT_SANTANDER_CLIENT_SECRET_FILE"), CertificateFile: os.Getenv("RANJIT_SANTANDER_CERT_FILE"), PrivateKeyFile: os.Getenv("RANJIT_SANTANDER_KEY_FILE"), Account: os.Getenv("RANJIT_SANTANDER_ACCOUNT"), LookbackDays: days}
		if c.Santander.Environment != "sandbox" && c.Santander.Environment != "production" {
			return c, fmt.Errorf("RANJIT_SANTANDER_ENV must be sandbox or production")
		}
		if c.Santander.ClientID == "" || c.Santander.ClientSecretFile == "" || c.Santander.CertificateFile == "" || c.Santander.PrivateKeyFile == "" || c.Santander.Account == "" {
			return c, fmt.Errorf("Santander requires RANJIT_SANTANDER_CLIENT_ID, RANJIT_SANTANDER_CLIENT_SECRET_FILE, RANJIT_SANTANDER_CERT_FILE, RANJIT_SANTANDER_KEY_FILE and RANJIT_SANTANDER_ACCOUNT")
		}
	}
	if c.Provider == "sicoob" {
		days, err := strconv.Atoi(env("RANJIT_SICOOB_LOOKBACK_DAYS", "7"))
		if err != nil || days < 1 || days > 90 {
			return c, fmt.Errorf("RANJIT_SICOOB_LOOKBACK_DAYS must be between 1 and 90")
		}
		c.Sicoob = sicoob.Config{Environment: env("RANJIT_SICOOB_ENV", "sandbox"), ClientID: os.Getenv("RANJIT_SICOOB_CLIENT_ID"), AccessTokenFile: os.Getenv("RANJIT_SICOOB_ACCESS_TOKEN_FILE"), CertificateFile: os.Getenv("RANJIT_SICOOB_CERT_FILE"), PrivateKeyFile: os.Getenv("RANJIT_SICOOB_KEY_FILE"), Account: os.Getenv("RANJIT_SICOOB_ACCOUNT"), LookbackDays: days}
		if c.Sicoob.Environment != "sandbox" && c.Sicoob.Environment != "production" {
			return c, fmt.Errorf("RANJIT_SICOOB_ENV must be sandbox or production")
		}
		if c.Sicoob.ClientID == "" || c.Sicoob.Account == "" {
			return c, fmt.Errorf("Sicoob requires RANJIT_SICOOB_CLIENT_ID and RANJIT_SICOOB_ACCOUNT")
		}
		if c.Sicoob.Environment == "sandbox" && c.Sicoob.AccessTokenFile == "" {
			return c, fmt.Errorf("Sicoob sandbox requires RANJIT_SICOOB_ACCESS_TOKEN_FILE")
		}
		if c.Sicoob.Environment == "production" && (c.Sicoob.CertificateFile == "" || c.Sicoob.PrivateKeyFile == "" || c.Sicoob.AccessTokenFile != "") {
			return c, fmt.Errorf("Sicoob production requires RANJIT_SICOOB_CERT_FILE and RANJIT_SICOOB_KEY_FILE; unset RANJIT_SICOOB_ACCESS_TOKEN_FILE")
		}
	}
	if c.Provider == "c6" {
		days, err := strconv.Atoi(env("RANJIT_C6_LOOKBACK_DAYS", "7"))
		if err != nil || days < 1 || days > 90 {
			return c, fmt.Errorf("RANJIT_C6_LOOKBACK_DAYS must be between 1 and 90")
		}
		c.C6 = c6.Config{Environment: env("RANJIT_C6_ENV", "sandbox"), ClientID: os.Getenv("RANJIT_C6_CLIENT_ID"), ClientSecretFile: os.Getenv("RANJIT_C6_CLIENT_SECRET_FILE"), CertificateFile: os.Getenv("RANJIT_C6_CERT_FILE"), PrivateKeyFile: os.Getenv("RANJIT_C6_KEY_FILE"), LookbackDays: days}
		if c.C6.Environment != "sandbox" && c.C6.Environment != "production" {
			return c, fmt.Errorf("RANJIT_C6_ENV must be sandbox or production")
		}
		if c.C6.ClientID == "" || c.C6.ClientSecretFile == "" || c.C6.CertificateFile == "" || c.C6.PrivateKeyFile == "" {
			return c, fmt.Errorf("C6 requires RANJIT_C6_CLIENT_ID, RANJIT_C6_CLIENT_SECRET_FILE, RANJIT_C6_CERT_FILE and RANJIT_C6_KEY_FILE")
		}
	}
	if err := validateServer(c.Addr, c.BaseURL, c.APIKey); err != nil {
		return c, err
	}
	var err error
	c.SyncInterval, err = time.ParseDuration(env("RANJIT_SYNC_INTERVAL", "1h"))
	if err != nil || c.SyncInterval < time.Minute {
		return c, fmt.Errorf("RANJIT_SYNC_INTERVAL must be a duration of at least 1m")
	}
	return c, nil
}

func validateServer(addr, baseURL, apiKey string) error {
	if len(apiKey) < 32 || strings.ContainsAny(apiKey, " \t\r\n") {
		return fmt.Errorf("API key must contain at least 32 bytes without whitespace")
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("RANJIT_ADDR must use host:port")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("RANJIT_BASE_URL must be an absolute http(s) origin without a path, credentials, query or fragment")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("RANJIT_BASE_URL requires HTTPS outside loopback")
		}
	}
	return nil
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
