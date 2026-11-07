package sicoob

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

const maxTransactions = 10000

func (s *Source) Institutions() []ranjit.Institution {
	name := "Sicoob — saldo e extrato"
	if s.environment == "sandbox" {
		name += " (sandbox)"
	}
	return []ranjit.Institution{{ID: "sicoob", Name: name, Provider: "sicoob", Sandbox: s.environment == "sandbox", Validation: "experimental", ConnectionMode: "api_credentials", DataCoverage: []string{"available_balance", "account_statement"}}}
}

type transaction struct {
	ID          string          `json:"transactionId"`
	Type        string          `json:"tipo"`
	Value       json.RawMessage `json:"valor"`
	Date        string          `json:"data"`
	PostedDate  string          `json:"dataLote"`
	Description string          `json:"descricao"`
}

// The Swagger describes the payload directly; the bank's Postman collection
// also documents the resultado envelope. Accept both, but never a null result.
func (s *Source) data(ctx context.Context, path string, result any) error {
	var raw json.RawMessage
	if err := s.get(ctx, path, &raw); err != nil {
		return err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope == nil {
		return ranjit.ErrInvalid
	}
	if e, ok := envelope["erro"]; ok && string(e) != "null" {
		return ranjit.ErrProviderUnavailable
	}
	if nested, ok := envelope["resultado"]; ok {
		if len(nested) == 0 || nested[0] != '{' {
			return ranjit.ErrInvalid
		}
		raw = nested
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return ranjit.ErrInvalid
	}
	return nil
}

func (s *Source) balance(ctx context.Context) (ranjit.Money, error) {
	var result struct {
		Balance json.RawMessage `json:"saldo"`
	}
	query := url.Values{"numeroContaCorrente": {s.account}}
	if err := s.data(ctx, "/conta-corrente/v4/saldo?"+query.Encode(), &result); err != nil {
		return ranjit.Money{}, err
	}
	return provider.ParseBRL(result.Balance)
}

func (s *Source) Fetch(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
	if c.Provider != "sicoob" || c.InstitutionID != "sicoob" || c.AuthorizationMode != "api_credentials" || c.CredentialID != s.CredentialID() {
		return provider.Snapshot{}, ranjit.ErrProviderCredentials
	}
	if c.Status != "active" || !c.ConsentExpiresAt.After(s.now()) {
		return provider.Snapshot{}, ranjit.ErrConflict
	}
	balance, err := s.balance(ctx)
	if err != nil {
		return provider.Snapshot{}, err
	}
	batch := provider.Snapshot{Accounts: []provider.Account{{ExternalID: "own-account", Name: "Conta Sicoob", Type: "checking", Balance: balance}}}
	now := provider.SyncTime(ctx, s.now()).In(s.location)
	finish := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
	start := finish.AddDate(0, 0, 1-s.lookbackDays)
	for first := start; !first.After(finish); {
		nextMonth := time.Date(first.Year(), first.Month()+1, 1, 0, 0, 0, 0, s.location)
		last := nextMonth.AddDate(0, 0, -1)
		if last.After(finish) {
			last = finish
		}
		query := url.Values{"numeroContaCorrente": {s.account}, "agruparCNAB": {"false"}, "diaInicial": {strconv.Itoa(first.Day())}, "diaFinal": {strconv.Itoa(last.Day())}}
		path := fmt.Sprintf("/conta-corrente/v4/extrato/%d/%d?%s", first.Month(), first.Year(), query.Encode())
		var result struct {
			Transactions []transaction `json:"transacoes"`
		}
		if err := s.data(ctx, path, &result); err != nil {
			return provider.Snapshot{}, err
		}
		if result.Transactions == nil || len(batch.Transactions)+len(result.Transactions) > maxTransactions {
			return provider.Snapshot{}, ranjit.ErrInvalid
		}
		for _, entry := range result.Transactions {
			tx, err := normalize(entry)
			if err != nil {
				return provider.Snapshot{}, err
			}
			if tx.PostedOn < first.Format(time.DateOnly) || tx.PostedOn > last.Format(time.DateOnly) {
				return provider.Snapshot{}, fmt.Errorf("%w: Sicoob transaction outside requested dates", ranjit.ErrInvalid)
			}
			batch.Transactions = append(batch.Transactions, tx)
		}
		first = nextMonth
	}
	if err := batch.Validate(); err != nil {
		return provider.Snapshot{}, err
	}
	return batch, nil
}

func normalize(t transaction) (provider.Transaction, error) {
	invalid := fmt.Errorf("%w: invalid Sicoob transaction", ranjit.ErrInvalid)
	if t.ID == "" || len(t.ID) > 256 || strings.TrimSpace(t.ID) != t.ID || strings.ContainsAny(t.ID, "\x00\r\n") {
		return provider.Transaction{}, invalid
	}
	amount, err := provider.ParseBRL(t.Value)
	if err != nil {
		return provider.Transaction{}, err
	}
	if amount.AmountMinor <= 0 {
		return provider.Transaction{}, invalid
	}
	switch strings.ToUpper(t.Type) {
	case "C", "CREDITO", "CRÉDITO":
	case "D", "DEBITO", "DÉBITO":
		amount.AmountMinor = -amount.AmountMinor
	default:
		return provider.Transaction{}, invalid
	}
	value := t.PostedDate
	if value == "" {
		value = t.Date
	}
	var date string
	for _, layout := range []string{time.DateOnly, time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05", "02/01/2006", "20060102"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			date = parsed.Format(time.DateOnly)
			break
		}
	}
	if date == "" {
		return provider.Transaction{}, invalid
	}
	description := strings.TrimSpace(t.Description)
	if description == "" {
		description = "Lançamento Sicoob"
	}
	return provider.Transaction{ExternalID: t.ID, AccountExternalID: "own-account", Description: description, Amount: amount, PostedOn: date, Status: "booked"}, nil
}
