// Package efi connects to the operator's own Efí account through the official Pix API.
// It reads balances, received Pix and their completed refunds; it is not a full statement.
package efi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

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
	auth         provider.OAuth
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

func (s *Source) get(ctx context.Context, path string, result any) error {
	return s.auth.Get(ctx, provider.OAuthOptions{TokenURL: s.baseURL + "/oauth/token", ClientID: s.clientID, ClientSecret: s.clientSecret, Scopes: []string{"gn.balance.read", "pix.read"}, JSONBasic: true, Now: s.now, Request: s.request}, s.baseURL+path, nil, result)
}

func (s *Source) request(req *http.Request, result any) (int, error) {

	return provider.RequestJSON(s.client, req, result, s.now())
}
