package santander

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

const bankPath = "/bank_account_information/v1/banks/90400888000142"
const maxPages = 100
const pageSize = 50

func (s *Source) Institutions() []ranjit.Institution {
	name := "Santander Empresas — saldo e extrato"
	if s.environment == "sandbox" {
		name += " (sandbox)"
	}
	return []ranjit.Institution{{ID: "santander", Name: name, Provider: "santander", Sandbox: s.environment == "sandbox", Validation: "experimental", ConnectionMode: "api_credentials", DataCoverage: []string{"available_balance", "account_statement"}}}
}

type transaction struct {
	ID          string          `json:"transactionId"`
	Status      string          `json:"completedAuthorisedPaymentType"`
	Operation   string          `json:"creditDebitType"`
	Description string          `json:"transactionName"`
	Value       json.RawMessage `json:"amount"`
	Currency    string          `json:"transactionCurrency"`
	Date        string          `json:"transactionDate"`
}

func (s *Source) balance(ctx context.Context) (ranjit.Money, error) {
	var result struct {
		Available json.RawMessage `json:"availableAmount"`
		Currency  string          `json:"availableAmountCurrency"`
	}
	if err := s.get(ctx, bankPath+"/balances/"+s.account, &result); err != nil {
		return ranjit.Money{}, err
	}
	if result.Currency != "BRL" {
		return ranjit.Money{}, ranjit.ErrInvalid
	}
	return provider.ParseBRL(result.Available)
}

func (s *Source) Fetch(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
	if c.Provider != "santander" || c.InstitutionID != "santander" || c.AuthorizationMode != "api_credentials" || c.CredentialID != s.CredentialID() {
		return provider.Snapshot{}, ranjit.ErrProviderCredentials
	}
	if c.Status != "active" || !c.ConsentExpiresAt.After(s.now()) {
		return provider.Snapshot{}, ranjit.ErrConflict
	}
	balance, err := s.balance(ctx)
	if err != nil {
		return provider.Snapshot{}, err
	}
	batch := provider.Snapshot{Accounts: []provider.Account{{ExternalID: "own-account", Name: "Conta Santander Empresas", Type: "checking", Balance: balance}}}
	now := provider.SyncTime(ctx, s.now()).In(s.location)
	first, last := now.AddDate(0, 0, 1-s.lookbackDays).Format(time.DateOnly), now.Format(time.DateOnly)
	query := url.Values{"initialDate": {first}, "finalDate": {last}, "_limit": {strconv.Itoa(pageSize)}}
	seen := make(map[string]bool)
	offset := 1
	for page := 0; page < maxPages; page++ {
		query.Set("_offset", strconv.Itoa(offset))
		var result struct {
			Entries  []transaction `json:"_content"`
			Pageable *struct {
				More *bool `json:"_moreElements"`
			} `json:"_pageable"`
		}
		// Use the account-specific route and offset contract published in the
		// bank's Postman collection. Never follow a URL supplied in a response.
		if err := s.get(ctx, bankPath+"/statements/"+s.account+"?"+query.Encode(), &result); err != nil {
			return provider.Snapshot{}, err
		}
		if result.Entries == nil || result.Pageable == nil || result.Pageable.More == nil || len(result.Entries) > pageSize {
			return provider.Snapshot{}, ranjit.ErrInvalid
		}
		for _, entry := range result.Entries {
			tx, err := normalize(entry)
			if err != nil {
				return provider.Snapshot{}, err
			}
			if tx.PostedOn < first || tx.PostedOn > last {
				return provider.Snapshot{}, fmt.Errorf("%w: Santander transaction outside requested dates", ranjit.ErrInvalid)
			}
			if seen[tx.ExternalID] {
				return provider.Snapshot{}, fmt.Errorf("%w: repeated Santander transaction", ranjit.ErrInvalid)
			}
			seen[tx.ExternalID] = true
			batch.Transactions = append(batch.Transactions, tx)
		}
		if !*result.Pageable.More {
			if err := batch.Validate(); err != nil {
				return provider.Snapshot{}, err
			}
			return batch, nil
		}
		if len(result.Entries) == 0 {
			return provider.Snapshot{}, ranjit.ErrProviderUnavailable
		}
		offset += len(result.Entries)
	}
	return provider.Snapshot{}, ranjit.ErrProviderUnavailable
}

func normalize(t transaction) (provider.Transaction, error) {
	invalid := fmt.Errorf("%w: invalid Santander transaction", ranjit.ErrInvalid)
	if t.ID == "" || len(t.ID) > 256 || strings.TrimSpace(t.ID) != t.ID || strings.ContainsAny(t.ID, "\x00\r\n") || t.Currency != "BRL" {
		return provider.Transaction{}, invalid
	}
	amount, err := provider.ParseBRL(t.Value)
	if err != nil {
		return provider.Transaction{}, err
	}
	if amount.AmountMinor == 0 {
		return provider.Transaction{}, invalid
	}
	switch t.Operation {
	case "CREDITO":
		if amount.AmountMinor < 0 {
			return provider.Transaction{}, invalid
		}
	case "DEBITO":
		if amount.AmountMinor > 0 {
			amount.AmountMinor = -amount.AmountMinor
		}
	default:
		return provider.Transaction{}, invalid
	}
	// The statement schema identifies settled entries explicitly. Do not label
	// an unknown settlement state as booked or invent a pending lifecycle.
	if t.Status != "TRANSACAO_EFETIVADA" {
		return provider.Transaction{}, invalid
	}
	if _, err := time.Parse(time.DateOnly, t.Date); err != nil {
		return provider.Transaction{}, invalid
	}
	description := strings.TrimSpace(t.Description)
	if description == "" {
		description = "Lançamento Santander"
	}
	return provider.Transaction{ExternalID: t.ID, AccountExternalID: "own-account", Description: description, Amount: amount, PostedOn: t.Date, Status: "booked"}, nil
}
