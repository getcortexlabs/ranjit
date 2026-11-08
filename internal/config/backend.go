package config

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

func validatePrevious(current, previous string, expires time.Time) error {
	if previous == "" && expires.IsZero() {
		return nil
	}
	if len(previous) < 32 || strings.ContainsAny(previous, " \t\r\n") || previous == current || expires.IsZero() {
		return errors.New("previous API key requires a distinct key of at least 32 bytes and an RFC3339 expiration")
	}
	return nil
}

// APIKeys reads personal configuration on each authentication so atomic CLI
// rotations take effect without restarting. Read errors deliberately fail closed.
func (c Config) APIKeys(now time.Time) ([]string, error) {
	current, previous, expires := c.APIKey, c.PreviousAPIKey, c.PreviousAPIKeyExpiresAt
	if c.PersonalFile != "" {
		p, err := ReadPersonal(c.PersonalFile)
		if err != nil {
			return nil, errors.New("cannot read API key configuration")
		}
		current, previous, expires = p.APIKey, p.PreviousAPIKey, p.PreviousAPIKeyExpiresAt
	}
	keys := []string{current}
	if previous != "" && expires.After(now) {
		keys = append(keys, previous)
	}
	return keys, nil
}

func validateWebhook(destination, secret string) error {
	if destination == "" && secret == "" {
		return nil
	}
	if len(secret) < 32 || strings.ContainsAny(secret, " \t\r\n") {
		return errors.New("webhook configuration requires a signing secret of at least 32 bytes without whitespace")
	}
	u, err := url.Parse(destination)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("webhook URL must use HTTPS without credentials, query or fragment (HTTP is allowed on loopback)")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("webhook URL requires HTTPS outside loopback")
		}
	}
	return nil
}
