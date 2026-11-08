// Package ranjit defines the public API types and Go client.
package ranjit

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrUnauthorized        = errors.New("unauthorized")
	ErrRateLimited         = errors.New("rate limited")
	ErrResponseTooLarge    = errors.New("response exceeds 1 MiB")
	ErrInvalidResponse     = errors.New("invalid response")
	ErrWebhookState        = errors.New("webhook is not failed or does not belong to the configured endpoint")
	ErrIdempotency         = errors.New("idempotency key was already used with a different request")
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("connection is not active")
	ErrInvalid             = errors.New("invalid input")
	ErrConsent             = errors.New("authorization link is invalid, expired or already used")
	ErrLease               = errors.New("job lease is no longer valid")
	ErrProviderCredentials = errors.New("provider credentials are invalid, missing required scopes or have changed")
	ErrProviderUnavailable = errors.New("provider is unavailable")
)

type Institution struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Provider       string   `json:"provider"`
	Sandbox        bool     `json:"sandbox"`
	ConnectionMode string   `json:"connection_mode"`
	DataCoverage   []string `json:"data_coverage"`
	Validation     string   `json:"validation"`
	Profile        string   `json:"profile,omitempty"`
}

type Connection struct {
	ID                string     `json:"id"`
	CustomerID        string     `json:"customer_id"`
	InstitutionID     string     `json:"institution_id"`
	Provider          string     `json:"provider"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
	ConsentExpiresAt  time.Time  `json:"consent_expires_at,omitzero"`
	LastSyncedAt      *time.Time `json:"last_synced_at,omitempty"`
	AuthorizationMode string     `json:"authorization_mode"`
	CredentialID      string     `json:"-"`
}

type ConnectRequest struct {
	CustomerID    string `json:"customer_id"`
	InstitutionID string `json:"institution_id"`
}

func (c Connection) HistoryAvailable() bool {
	return c.Status == "active" || c.AuthorizationMode == "api_credentials" && (c.Status == "paused" || c.Status == "expired")
}

type ConnectResponse struct {
	Connection             Connection `json:"connection"`
	AuthorizationURL       string     `json:"authorization_url,omitempty"`
	AuthorizationExpiresAt time.Time  `json:"authorization_expires_at,omitzero"`
}

// Money uses signed minor units. BRL 12.34 is 1234; debits are negative.
// Values are restricted to the exact integer range supported by JSON clients.
type Money struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

func (m Money) Validate() error {
	if m.Currency != "BRL" || m.AmountMinor < -9007199254740991 || m.AmountMinor > 9007199254740991 {
		return fmt.Errorf("%w: unsupported money value", ErrInvalid)
	}
	return nil
}

type Account struct {
	ID           string     `json:"id"`
	ConnectionID string     `json:"connection_id"`
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	Balance      *Money     `json:"balance"`
	BalanceAt    *time.Time `json:"balance_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type Transaction struct {
	ID          string `json:"id"`
	AccountID   string `json:"account_id"`
	Description string `json:"description"`
	Amount      Money  `json:"amount"`
	PostedOn    string `json:"posted_on"`
	Status      string `json:"status"`
}

type Job struct {
	ID                string    `json:"id"`
	ConnectionID      string    `json:"connection_id"`
	Status            string    `json:"status"`
	Attempts          int       `json:"attempts"`
	ErrorCode         string    `json:"error_code,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	LeaseToken        string    `json:"-"`
	RequestsCompleted int       `json:"requests_completed"`
}

type ConnectionStatus struct {
	Connection   Connection `json:"connection"`
	LatestJob    *Job       `json:"latest_job,omitempty"`
	Accounts     int        `json:"accounts"`
	Transactions int        `json:"transactions"`
	DataFrom     string     `json:"data_from,omitempty"`
	DataThrough  string     `json:"data_through,omitempty"`
}

type ImportResult struct {
	New           int    `json:"new"`
	Updated       int    `json:"updated"`
	Unchanged     int    `json:"unchanged"`
	ConnectionID  string `json:"connection_id"`
	Accounts      int    `json:"accounts"`
	Transactions  int    `json:"transactions"`
	Duplicate     bool   `json:"duplicate"`
	Deduplication string `json:"deduplication"`
}

type Page[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func NewID(prefix string) string {
	return prefix + "_" + rand.Text()
}

func StableID(prefix, owner, externalID string) string {
	sum := sha256.Sum256([]byte(owner + "\x00" + externalID))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}

// ImportPreview describes a dry run. Its connection ID may not exist yet.
type ImportPreview struct {
	Result  ImportResult   `json:"result"`
	From    string         `json:"from"`
	Through string         `json:"through"`
	Sample  []ImportSample `json:"sample"`
}
type ImportSample struct {
	PostedOn    string `json:"posted_on"`
	Description string `json:"description"`
	Amount      Money  `json:"amount"`
}
