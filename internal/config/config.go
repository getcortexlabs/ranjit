package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit/internal/provider/efi"
	"github.com/getcortexlabs/ranjit/internal/provider/inter"
)

type Config struct {
	Addr         string
	BaseURL      string
	APIKey       string
	Database     string
	SyncInterval time.Duration
	Provider     string
	EFI          efi.Config
	Inter        inter.Config
}

func Load() (Config, error) {
	c := Config{Addr: env("RANJIT_ADDR", "127.0.0.1:8080"), BaseURL: strings.TrimRight(env("RANJIT_BASE_URL", "http://localhost:8080"), "/"), APIKey: os.Getenv("RANJIT_API_KEY"), Database: env("RANJIT_DATABASE", "./data/ranjit.db")}
	if len(c.APIKey) < 32 || strings.ContainsAny(c.APIKey, " \t\r\n") {
		return c, fmt.Errorf("RANJIT_API_KEY must contain at least 32 bytes without whitespace; run 'ranjit init' to generate one")
	}
	c.Provider = env("RANJIT_PROVIDER", "sandbox")
	if c.Provider != "sandbox" && c.Provider != "efi" && c.Provider != "inter" {
		return c, fmt.Errorf("RANJIT_PROVIDER must be sandbox, efi or inter")
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
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return c, fmt.Errorf("RANJIT_ADDR must use host:port")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "https" && u.Scheme != "http" {
		return c, fmt.Errorf("RANJIT_BASE_URL must be an absolute http(s) origin without a path, credentials, query or fragment")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return c, fmt.Errorf("RANJIT_BASE_URL requires HTTPS outside loopback")
		}
	}
	c.SyncInterval, err = time.ParseDuration(env("RANJIT_SYNC_INTERVAL", "1h"))
	if err != nil || c.SyncInterval < time.Minute {
		return c, fmt.Errorf("RANJIT_SYNC_INTERVAL must be a duration of at least 1m")
	}
	return c, nil
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
