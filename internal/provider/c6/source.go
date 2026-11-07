package c6

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

const maxTransactions = 10000

func (s *Source) Institutions() []ranjit.Institution {
	name := "C6 Empresas — saldo e extrato"
	if s.environment == "sandbox" {
		name += " (sandbox)"
	}
	return []ranjit.Institution{{ID: "c6", Name: name, Provider: "c6", Sandbox: s.environment == "sandbox", Validation: "experimental", ConnectionMode: "api_credentials", DataCoverage: []string{"available_balance", "account_statement"}}}
}

type transaction struct {
	Reference   string          `json:"reference"`
	Date        string          `json:"entry_date"`
	Operation   string          `json:"operation_type"`
	Value       json.RawMessage `json:"amount"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
}

func (s *Source) balance(ctx context.Context) (ranjit.Money, error) {
	var result struct {
		Available json.RawMessage `json:"available"`
		Currency  string          `json:"currency_code"`
	}
	if err := s.get(ctx, "/v1/statement/balance", &result); err != nil {
		return ranjit.Money{}, err
	}
	if result.Currency != "BRL" {
		return ranjit.Money{}, ranjit.ErrInvalid
	}
	return provider.ParseBRL(result.Available)
}

func (s *Source) statement(ctx context.Context, start, end string) ([]provider.Transaction, error) {
	query := url.Values{"start_date": {start}, "end_date": {end}}
	var result struct {
		Entries []transaction `json:"entries"`
	}
	if err := s.get(ctx, "/v1/statement/?"+query.Encode(), &result); err != nil {
		return nil, err
	}
	if result.Entries == nil || len(result.Entries) > maxTransactions {
		return nil, ranjit.ErrInvalid
	}
	entries := make([]provider.Transaction, 0, len(result.Entries))
	for _, entry := range result.Entries {
		tx, err := normalize(entry)
		if err != nil {
			return nil, err
		}
		if tx.PostedOn < start || tx.PostedOn > end {
			return nil, fmt.Errorf("%w: C6 transaction outside requested dates", ranjit.ErrInvalid)
		}
		entries = append(entries, tx)
	}
	batch := provider.Snapshot{Accounts: []provider.Account{{ExternalID: "own-account", Name: "Conta C6 Empresas", Type: "checking", Balance: ranjit.Money{Currency: "BRL"}}}, Transactions: entries}
	if err := batch.Validate(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Source) Fetch(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
	if c.Provider != "c6" || c.InstitutionID != "c6" || c.AuthorizationMode != "api_credentials" || c.CredentialID != s.CredentialID() {
		return provider.Snapshot{}, ranjit.ErrProviderCredentials
	}
	if c.Status != "active" || !c.ConsentExpiresAt.After(s.now()) {
		return provider.Snapshot{}, ranjit.ErrConflict
	}
	balance, err := s.balance(ctx)
	if err != nil {
		return provider.Snapshot{}, err
	}
	batch := provider.Snapshot{Accounts: []provider.Account{{ExternalID: "own-account", Name: "Conta C6 Empresas", Type: "checking", Balance: balance}}}
	now := provider.SyncTime(ctx, s.now()).In(s.location)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
	for first := end.AddDate(0, 0, 1-s.lookbackDays); !first.After(end); {
		last := first.AddDate(0, 0, 29)
		if last.After(end) {
			last = end
		}
		entries, err := s.statement(ctx, first.Format(time.DateOnly), last.Format(time.DateOnly))
		if err != nil {
			return provider.Snapshot{}, err
		}
		if len(batch.Transactions)+len(entries) > maxTransactions {
			return provider.Snapshot{}, ranjit.ErrInvalid
		}
		batch.Transactions = append(batch.Transactions, entries...)
		first = last.AddDate(0, 0, 1)
	}
	if err := batch.Validate(); err != nil {
		return provider.Snapshot{}, err
	}
	return batch, nil
}

func normalize(t transaction) (provider.Transaction, error) {
	invalid := fmt.Errorf("%w: invalid C6 transaction", ranjit.ErrInvalid)
	// The bank specifies reference as the stable identifier for reconciliation.
	id := t.Reference
	if id == "" || len(id) > 256 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\x00\r\n") {
		return provider.Transaction{}, invalid
	}
	amount, err := provider.ParseBRL(t.Value)
	if err != nil {
		return provider.Transaction{}, err
	}
	if amount.AmountMinor <= 0 {
		return provider.Transaction{}, invalid
	}
	switch t.Operation {
	case "INCOMING":
	case "OUTGOING":
		amount.AmountMinor = -amount.AmountMinor
	default:
		return provider.Transaction{}, invalid
	}
	if _, err := time.Parse(time.DateOnly, t.Date); err != nil {
		return provider.Transaction{}, invalid
	}
	description := strings.TrimSpace(t.Description)
	if description == "" {
		description = strings.TrimSpace(t.Title)
	}
	if description == "" {
		description = "Lançamento C6"
	}
	return provider.Transaction{ExternalID: id, AccountExternalID: "own-account", Description: description, Amount: amount, PostedOn: t.Date, Status: "booked"}, nil
}
