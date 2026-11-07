package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	BaseURL      string
	APIKey       string
	Database     string
	SyncInterval time.Duration
}

func Load() (Config, error) {
	c := Config{Addr: env("RANJIT_ADDR", "127.0.0.1:8080"), BaseURL: strings.TrimRight(env("RANJIT_BASE_URL", "http://localhost:8080"), "/"), APIKey: os.Getenv("RANJIT_API_KEY"), Database: env("RANJIT_DATABASE", "./data/ranjit.db")}
	if len(c.APIKey) < 32 || strings.ContainsAny(c.APIKey, " \t\r\n") {
		return c, fmt.Errorf("RANJIT_API_KEY must contain at least 32 bytes without whitespace; run 'ranjit init' to generate one")
	}
	if env("RANJIT_PROVIDER", "sandbox") != "sandbox" {
		return c, fmt.Errorf("RANJIT_PROVIDER: this version supports only sandbox")
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
