// Package c6 reads the operator's own C6 Empresas statement using the bank's own API.
package c6

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

const maxResponseBytes = 2 << 20

type Config struct {
	Environment      string
	ClientID         string
	ClientSecretFile string
	CertificateFile  string
	PrivateKeyFile   string
	LookbackDays     int
}

type Source struct {
	client       *http.Client
	baseURL      string
	clientID     string
	clientSecret string
	credentialID string
	environment  string
	lookbackDays int
	location     *time.Location
	now          func() time.Time
	mu           sync.Mutex
	token        string
	expiresAt    time.Time
}

func New(cfg Config) (*Source, error) {
	base := "https://baas-api-sandbox.c6bank.info"
	if cfg.Environment == "production" {
		base = "https://baas-api.c6bank.info"
	} else if cfg.Environment != "sandbox" {
		return nil, errors.New("C6 environment must be production or sandbox")
	}
	if cfg.ClientID == "" || len(cfg.ClientID) > 512 || strings.ContainsAny(cfg.ClientID, ": \t\r\n") {
		return nil, errors.New("C6 client ID is invalid")
	}
	if cfg.LookbackDays < 1 || cfg.LookbackDays > 90 {
		return nil, errors.New("C6 lookback must be between 1 and 90 days")
	}
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return nil, errors.New("cannot load bank time zone")
	}
	value, err := provider.ReadSecretFile(cfg.ClientSecretFile)
	if err != nil {
		return nil, err
	}
	client, err := provider.NewMTLSClient(cfg.CertificateFile, cfg.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte("c6\x00" + cfg.Environment + "\x00" + cfg.ClientID))
	return &Source{
		client:  client,
		baseURL: base, clientID: cfg.ClientID, clientSecret: value, credentialID: hex.EncodeToString(hash[:]),
		environment: cfg.Environment, lookbackDays: cfg.LookbackDays, location: location, now: time.Now,
	}, nil
}

func (s *Source) CredentialID() string { return s.credentialID }
func (s *Source) Close()               { s.client.CloseIdleConnections() }

func (s *Source) Verify(ctx context.Context) error {
	_, err := s.balance(ctx)
	if errors.Is(err, ranjit.ErrInvalid) {
		return ranjit.ErrProviderUnavailable
	}
	return err
}

func (s *Source) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.now().Before(s.expiresAt) {
		return s.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {s.clientID}, "client_secret": {s.clientSecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v1/auth/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	started := s.now()
	status, err := s.request(req, &result)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "", ranjit.ErrProviderCredentials
	}
	if err != nil {
		return "", err
	}
	if result.AccessToken == "" || len(result.AccessToken) > 8192 || strings.ContainsAny(result.AccessToken, " \r\n\t") || !strings.EqualFold(result.TokenType, "Bearer") || result.ExpiresIn <= 0 || result.ExpiresIn > 86400 {
		return "", ranjit.ErrProviderUnavailable
	}
	lifetime := time.Duration(result.ExpiresIn) * time.Second
	s.token = result.AccessToken
	s.expiresAt = started.Add(lifetime - min(30*time.Second, lifetime/10))
	return s.token, nil
}

func (s *Source) get(ctx context.Context, path string, result any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.accessToken(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		status, err := s.request(req, result)
		if status == http.StatusUnauthorized {
			s.mu.Lock()
			if s.token == token {
				s.token = ""
				s.expiresAt = time.Time{}
			}
			s.mu.Unlock()
			if attempt == 0 {
				continue
			}
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return ranjit.ErrProviderCredentials
		}
		return err
	}
	return ranjit.ErrProviderCredentials
}

func (s *Source) request(req *http.Request, result any) (int, error) {
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return 0, req.Context().Err()
		}
		return 0, ranjit.ErrProviderUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		delay := time.Minute
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
			delay = time.Duration(min(seconds, 300)) * time.Second
		} else if date, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && date.After(s.now()) {
			delay = min(date.Sub(s.now()), 5*time.Minute)
		}
		return resp.StatusCode, &provider.RetryError{After: delay}
	}
	if resp.StatusCode != http.StatusOK {
		// Upstream error bodies may contain account data. Do not expose or persist them.
		return resp.StatusCode, fmt.Errorf("%w: C6 HTTP %d", ranjit.ErrProviderUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return resp.StatusCode, ranjit.ErrProviderUnavailable
	}
	if err = json.Unmarshal(body, result); err != nil {
		return resp.StatusCode, fmt.Errorf("%w: malformed C6 response", ranjit.ErrInvalid)
	}
	return resp.StatusCode, nil
}
