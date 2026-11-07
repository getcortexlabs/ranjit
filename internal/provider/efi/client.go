// Package efi connects to the operator's own Efí account through the official Pix API.
// It reads balances, received Pix and their completed refunds; it is not a full statement.
package efi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

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
	lookback     time.Duration
	now          func() time.Time
	mu           sync.Mutex
	token        string
	expiresAt    time.Time
}

func New(cfg Config) (*Source, error) {
	base := "https://pix-h.api.efipay.com.br"
	if cfg.Environment == "production" {
		base = "https://pix.api.efipay.com.br"
	} else if cfg.Environment != "homologation" {
		return nil, errors.New("Efí environment must be production or homologation")
	}
	if cfg.ClientID == "" || len(cfg.ClientID) > 512 || strings.ContainsAny(cfg.ClientID, ": \t\r\n") {
		return nil, errors.New("Efí client ID is invalid")
	}
	if cfg.LookbackDays < 1 || cfg.LookbackDays > 90 {
		return nil, errors.New("Efí lookback must be between 1 and 90 days")
	}
	value, err := provider.ReadSecretFile(cfg.ClientSecretFile)
	if err != nil {
		return nil, err
	}
	client, err := provider.NewMTLSClient(cfg.CertificateFile, cfg.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(cfg.Environment + "\x00" + cfg.ClientID))
	return &Source{
		client:  client,
		baseURL: base, clientID: cfg.ClientID, clientSecret: value, credentialID: hex.EncodeToString(hash[:]),
		environment: cfg.Environment, lookback: time.Duration(cfg.LookbackDays) * 24 * time.Hour, now: time.Now,
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/oauth/token", strings.NewReader(`{"grant_type":"client_credentials"}`))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(s.clientID, s.clientSecret)
	req.Header.Set("Content-Type", "application/json")
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
	scopes := strings.Fields(result.Scope)
	for _, required := range []string{"gn.balance.read", "pix.read"} {
		found := false
		for _, scope := range scopes {
			if scope == required {
				found = true
			}
		}
		if !found {
			return "", ranjit.ErrProviderCredentials
		}
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
	if resp.StatusCode != http.StatusOK {
		// Upstream error bodies may contain account data. Do not expose or persist them.
		return resp.StatusCode, fmt.Errorf("%w: Efí HTTP %d", ranjit.ErrProviderUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return resp.StatusCode, ranjit.ErrProviderUnavailable
	}
	if err = json.Unmarshal(body, result); err != nil {
		return resp.StatusCode, fmt.Errorf("%w: malformed Efí response", ranjit.ErrInvalid)
	}
	return resp.StatusCode, nil
}
