// Package sicoob reads the operator's own Sicoob account using the Conta Corrente API.
package sicoob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

const maxResponseBytes = provider.MaxResponseBytes

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
	auth            provider.OAuth
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

func (s *Source) get(ctx context.Context, path string, result any) error {
	return s.auth.Get(ctx, provider.OAuthOptions{TokenURL: s.tokenURL, ClientID: s.clientID, Scopes: []string{"cco_consulta"}, OptionalScope: true, StaticToken: s.sandboxToken, Now: s.now, Request: s.request}, s.baseURL+path, http.Header{"Client_id": {s.clientID}}, result)
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

	return provider.RequestJSON(s.client, req, result, s.now())
}
