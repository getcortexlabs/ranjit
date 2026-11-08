package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
	"github.com/getcortexlabs/ranjit/internal/store"
)

const AuthorizationTTL = 10 * time.Minute
const ConsentTTL = 90 * 24 * time.Hour

type Service struct {
	Store   *store.Store
	Source  provider.Source
	BaseURL string
	Now     func() time.Time
}

func New(db *store.Store, source provider.Source, baseURL string) *Service {
	return &Service{Store: db, Source: source, BaseURL: baseURL, Now: time.Now}
}

func (s *Service) Connect(ctx context.Context, req ranjit.ConnectRequest) (ranjit.ConnectResponse, error) {
	return s.ConnectWithKey(ctx, req, "")
}

func (s *Service) ConnectWithKey(ctx context.Context, req ranjit.ConnectRequest, key string) (ranjit.ConnectResponse, error) {
	if key != "" && !ranjit.ValidIdempotencyKey(key) {
		return ranjit.ConnectResponse{}, fmt.Errorf("%w: Idempotency-Key must contain 16 to 128 printable ASCII characters without spaces", ranjit.ErrInvalid)
	}
	encoded, _ := json.Marshal(req)
	fingerprint := tokenHash(string(encoded))
	keyHash := ""
	if key != "" {
		keyHash = tokenHash(key)
		c, deadline, err := s.Store.ReplayConnection(ctx, keyHash, fingerprint, s.Now())
		if !errors.Is(err, ranjit.ErrNotFound) {
			return s.connectResponse(c, deadline, key, ""), err
		}
	}
	if strings.TrimSpace(req.CustomerID) != req.CustomerID || req.CustomerID == "" || len(req.CustomerID) > 128 || strings.ContainsAny(req.CustomerID, "\x00\r\n") {
		return ranjit.ConnectResponse{}, fmt.Errorf("%w: customer_id must contain 1 to 128 bytes without surrounding whitespace", ranjit.ErrInvalid)
	}
	var institution ranjit.Institution
	for _, i := range s.Source.Institutions() {
		if i.ID == req.InstitutionID {
			institution = i
			break
		}
	}
	if institution.ID == "" {
		return ranjit.ConnectResponse{}, fmt.Errorf("%w: unknown institution", ranjit.ErrInvalid)
	}
	if institution.ConnectionMode == "api_credentials" {
		selected := s.Source
		if registry, ok := selected.(*provider.Registry); ok {
			var err error
			selected, _, err = registry.Resolve(institution.ID)
			if err != nil {
				return ranjit.ConnectResponse{}, err
			}
		}
		source, ok := selected.(provider.CredentialSource)
		if !ok || source.CredentialID() == "" {
			return ranjit.ConnectResponse{}, ranjit.ErrProviderCredentials
		}
		if err := source.Verify(ctx); err != nil {
			return ranjit.ConnectResponse{}, err
		}
		now := s.Now().UTC().Truncate(time.Millisecond)
		c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: req.CustomerID, InstitutionID: institution.ID, Provider: institution.Provider, Status: "active", CreatedAt: now, ConsentExpiresAt: now.Add(ConsentTTL), AuthorizationMode: "api_credentials", CredentialID: source.CredentialID()}
		c, deadline, err := s.Store.CreateConnectionOnce(ctx, c, nil, time.Time{}, keyHash, fingerprint)
		return s.connectResponse(c, deadline, key, ""), err
	}
	if institution.Provider != "sandbox" {
		return ranjit.ConnectResponse{}, ranjit.ErrInvalid
	}
	now := s.Now().UTC().Truncate(time.Millisecond)
	token := rand.Text()
	c := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: req.CustomerID, InstitutionID: req.InstitutionID, Provider: "sandbox", Status: "pending", CreatedAt: now, ConsentExpiresAt: now.Add(ConsentTTL), AuthorizationMode: "sandbox_consent"}
	if key != "" {
		token = sandboxToken(key, c.ID)
	}
	deadline := now.Add(AuthorizationTTL)
	c, deadline, err := s.Store.CreateConnectionOnce(ctx, c, tokenHash(token), deadline, keyHash, fingerprint)
	return s.connectResponse(c, deadline, key, token), err
}

func (s *Service) Consent(ctx context.Context, token string) (ranjit.Connection, error) {
	if len(token) < 20 || len(token) > 128 {
		return ranjit.Connection{}, ranjit.ErrConsent
	}
	return s.Store.PendingConsent(ctx, tokenHash(token), s.Now())
}

func (s *Service) Decide(ctx context.Context, token, decision string) (ranjit.Connection, error) {
	if len(token) < 20 || len(token) > 128 {
		return ranjit.Connection{}, ranjit.ErrConsent
	}
	if decision != "approve" && decision != "deny" {
		return ranjit.Connection{}, fmt.Errorf("%w: decision must be approve or deny", ranjit.ErrInvalid)
	}
	return s.Store.Decide(ctx, tokenHash(token), decision == "approve", s.Now())
}

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (s *Service) Renew(ctx context.Context, id string) (ranjit.Connection, error) {
	c, err := s.Store.Connection(ctx, id, s.Now())
	if err != nil {
		return c, err
	}
	if c.AuthorizationMode != "api_credentials" || !c.HistoryAvailable() {
		return c, ranjit.ErrConflict
	}
	selected := s.Source
	if r, ok := selected.(*provider.Registry); ok {
		selected, _, err = r.Resolve(c.InstitutionID)
		if err != nil {
			return c, err
		}
	}
	bank, ok := selected.(provider.CredentialSource)
	if !ok || bank.CredentialID() != c.CredentialID {
		return c, ranjit.ErrProviderCredentials
	}
	if err = bank.Verify(ctx); err != nil {
		return c, err
	}
	return s.Store.Renew(ctx, id, c.CredentialID, s.Now().Add(ConsentTTL), s.Now())
}

// The caller supplies the replay secret; only its hash is retained. Domain
// separation keeps this derivation specific to simulated consent links.
func sandboxToken(key, connectionID string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("ranjit:sandbox-consent:v1:" + connectionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) connectResponse(c ranjit.Connection, deadline time.Time, key, token string) ranjit.ConnectResponse {
	out := ranjit.ConnectResponse{Connection: c}
	if c.Status == "pending" && c.Provider == "sandbox" && deadline.After(s.Now()) {
		if key != "" {
			token = sandboxToken(key, c.ID)
		}
		out.AuthorizationURL = s.BaseURL + "/sandbox/authorize?token=" + url.QueryEscape(token)
		out.AuthorizationExpiresAt = deadline
	}
	return out
}
