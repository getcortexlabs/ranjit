// Package inter reads the operator's own Inter Empresas account using the Banking API.
package inter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

const maxResponseBytes = provider.MaxResponseBytes

type Config struct {
	Environment      string
	ClientID         string
	ClientSecretFile string
	CertificateFile  string
	PrivateKeyFile   string
	Account          string
	LookbackDays     int
}

type Source struct {
	client       *http.Client
	baseURL      string
	clientID     string
	clientSecret string
	credentialID string
	environment  string
	account      string
	lookbackDays int
	location     *time.Location
	now          func() time.Time
	auth         provider.OAuth
}

func New(cfg Config) (*Source, error) {
	base := "https://cdpj-sandbox.partners.uatinter.co"
	if cfg.Environment == "production" {
		base = "https://cdpj.partners.bancointer.com.br"
	} else if cfg.Environment != "sandbox" {
		return nil, errors.New("Inter environment must be production or sandbox")
	}
	if cfg.ClientID == "" || len(cfg.ClientID) > 512 || strings.ContainsAny(cfg.ClientID, ": \t\r\n") {
		return nil, errors.New("Inter client ID is invalid")
	}
	if cfg.LookbackDays < 1 || cfg.LookbackDays > 90 {
		return nil, errors.New("Inter lookback must be between 1 and 90 days")
	}
	if cfg.Account == "" || len(cfg.Account) > 20 {
		return nil, errors.New("Inter account number is required")
	}
	for _, c := range cfg.Account {
		if c < '0' || c > '9' {
			return nil, errors.New("Inter account must contain only digits, including the check digit")
		}
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
	hash := sha256.Sum256([]byte("inter\x00" + cfg.Environment + "\x00" + cfg.ClientID + "\x00" + cfg.Account))
	return &Source{
		client:  client,
		baseURL: base, clientID: cfg.ClientID, clientSecret: value, credentialID: hex.EncodeToString(hash[:]),
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
	return s.auth.Get(ctx, provider.OAuthOptions{TokenURL: s.baseURL + "/oauth/v2/token", ClientID: s.clientID, ClientSecret: s.clientSecret, Scopes: []string{"extrato.read"}, Now: s.now, Request: s.request}, s.baseURL+path, http.Header{"X-Conta-Corrente": {s.account}}, result)
}

func (s *Source) request(req *http.Request, result any) (int, error) {

	return provider.RequestJSON(s.client, req, result, s.now())
}
