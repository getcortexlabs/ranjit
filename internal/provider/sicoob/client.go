// Package sicoob reads the operator's own Sicoob account using the Conta Corrente API.
package sicoob

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
	Environment     string
	ClientID        string
	AccessTokenFile string
	CertificateFile string
	PrivateKeyFile  string
	Account         string
	LookbackDays    int
}

type Source struct {
	client          *http.Client
	baseURL         string
	clientID        string
	sandboxToken    string
	tokenURL        string
	requestMu       sync.Mutex
	lastRequest     time.Time
	requestInterval time.Duration
	credentialID    string
	environment     string
	account         string
	lookbackDays    int
	location        *time.Location
	now             func() time.Time
	mu              sync.Mutex
	token           string
	expiresAt       time.Time
}

func New(cfg Config) (*Source, error) {
	base := "https://sandbox.sicoob.com.br/sicoob/sandbox"
	if cfg.Environment == "production" {
		base = "https://api.sicoob.com.br"
	} else if cfg.Environment != "sandbox" {
		return nil, errors.New("Sicoob environment must be production or sandbox")
	}
	if cfg.ClientID == "" || len(cfg.ClientID) > 512 || strings.ContainsAny(cfg.ClientID, ": \t\r\n") {
		return nil, errors.New("Sicoob client ID is invalid")
	}
	if cfg.LookbackDays < 1 || cfg.LookbackDays > 90 {
		return nil, errors.New("Sicoob lookback must be between 1 and 90 days")
	}
	account, err := strconv.ParseInt(cfg.Account, 10, 64)
	if err != nil || account <= 0 || strings.IndexFunc(cfg.Account, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return nil, errors.New("Sicoob account must be a positive integer including the check digit")
	}
	cfg.Account = strconv.FormatInt(account, 10)
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return nil, errors.New("cannot load bank time zone")
	}
	var client *http.Client
	var token string
	if cfg.Environment == "production" {
		if cfg.AccessTokenFile != "" {
			return nil, errors.New("Sicoob production requires mTLS, not a sandbox token")
		}
		client, err = provider.NewMTLSClient(cfg.CertificateFile, cfg.PrivateKeyFile)
	} else {
		token, err = provider.ReadSecretFile(cfg.AccessTokenFile)
		if err == nil && strings.ContainsAny(token, " \t\r\n") {
			return nil, errors.New("invalid Sicoob sandbox token")
		}
		client = provider.NewTLSClient()
	}
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte("sicoob\x00" + cfg.Environment + "\x00" + cfg.ClientID + "\x00" + cfg.Account))
	return &Source{
		client:  client,
		baseURL: base, clientID: cfg.ClientID, sandboxToken: token, tokenURL: "https://auth.sicoob.com.br/auth/realms/cooperado/protocol/openid-connect/token", requestInterval: 550 * time.Millisecond, credentialID: hex.EncodeToString(hash[:]),
		environment: cfg.Environment, account: cfg.Account, lookbackDays: cfg.LookbackDays, location: location, now: time.Now,
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
	if s.environment == "sandbox" {
		return s.sandboxToken, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.now().Before(s.expiresAt) {
		return s.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {s.clientID}, "scope": {"cco_consulta"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
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
	scopes := strings.Fields(result.Scope)
	if result.Scope != "" {
		for _, required := range []string{"cco_consulta"} {
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
		req.Header.Set("client_id", s.clientID)
		status, err := s.request(req, result)
		if status == http.StatusUnauthorized && s.environment != "sandbox" {
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
	// Conta Corrente permits two requests per second. Keep a shared pace even
	// when credential verification and a worker run concurrently.
	if req.Method == http.MethodGet {
		s.requestMu.Lock()
		delay := time.Until(s.lastRequest.Add(s.requestInterval))
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-req.Context().Done():
				timer.Stop()
				s.requestMu.Unlock()
				return 0, req.Context().Err()
			case <-timer.C:
			}
		}
		s.lastRequest = time.Now()
		s.requestMu.Unlock()
	}

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
		return resp.StatusCode, fmt.Errorf("%w: Sicoob HTTP %d", ranjit.ErrProviderUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return resp.StatusCode, ranjit.ErrProviderUnavailable
	}
	if err = json.Unmarshal(body, result); err != nil {
		return resp.StatusCode, fmt.Errorf("%w: malformed Sicoob response", ranjit.ErrInvalid)
	}
	return resp.StatusCode, nil
}
