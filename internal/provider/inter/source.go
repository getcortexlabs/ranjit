package inter

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

const maxPages = 100

func (s *Source) Institutions() []ranjit.Institution {
	name := "Inter Empresas — saldo e extrato"
	if s.environment == "sandbox" {
		name += " (sandbox)"
	}
	return []ranjit.Institution{{ID: "inter", Name: name, Provider: "inter", Sandbox: s.environment == "sandbox", Validation: "experimental", ConnectionMode: "api_credentials", DataCoverage: []string{"available_balance", "account_statement"}}}
}

type transaction struct {
	ID          string          `json:"idTransacao"`
	Date        string          `json:"dataTransacao"`
	Operation   string          `json:"tipoOperacao"`
	Value       json.RawMessage `json:"valor"`
	Title       string          `json:"titulo"`
	Description string          `json:"descricao"`
}

type statementPage struct {
	Pages        *int          `json:"totalPaginas"`
	Total        *int          `json:"totalElementos"`
	First        *bool         `json:"primeiraPagina"`
	Last         *bool         `json:"ultimaPagina"`
	Count        *int          `json:"numeroDeElementos"`
	Transactions []transaction `json:"transacoes"`
}

func (s *Source) balance(ctx context.Context) (ranjit.Money, error) {
	var result struct {
		Available json.RawMessage `json:"disponivel"`
	}
	if err := s.get(ctx, "/banking/v2/saldo", &result); err != nil {
		return ranjit.Money{}, err
	}
	return money(result.Available)
}

func (s *Source) Fetch(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
	if c.Provider != "inter" || c.InstitutionID != "inter" || c.AuthorizationMode != "api_credentials" || c.CredentialID != s.CredentialID() {
		return provider.Snapshot{}, ranjit.ErrProviderCredentials
	}
	if c.Status != "active" || !c.ConsentExpiresAt.After(s.now()) {
		return provider.Snapshot{}, ranjit.ErrConflict
	}
	balance, err := s.balance(ctx)
	if err != nil {
		return provider.Snapshot{}, err
	}
	batch := provider.Snapshot{Accounts: []provider.Account{{ExternalID: "own-account", Name: "Conta Inter Empresas", Type: "checking", Balance: balance}}}
	end := provider.SyncTime(ctx, s.now()).In(s.location)
	start := end.AddDate(0, 0, 1-s.lookbackDays).Format(time.DateOnly)
	finish := end.Format(time.DateOnly)
	query := url.Values{"dataInicio": {start}, "dataFim": {finish}, "tamanhoPagina": {"100"}}
	pages, total := -1, -1
	for page := 0; page < maxPages; page++ {
		query.Set("pagina", strconv.Itoa(page))
		var data statementPage
		if err := s.get(ctx, "/banking/v2/extrato/completo?"+query.Encode(), &data); err != nil {
			return provider.Snapshot{}, err
		}
		if data.Pages == nil || data.Total == nil || data.First == nil || data.Last == nil || data.Count == nil || data.Transactions == nil || *data.Pages < 0 || *data.Pages > maxPages || *data.Total < 0 || *data.Total > maxPages*100 || *data.Count != len(data.Transactions) || len(data.Transactions) > 100 || *data.First != (page == 0) {
			return provider.Snapshot{}, fmt.Errorf("%w: invalid or oversized Inter pagination", ranjit.ErrInvalid)
		}
		if page == 0 {
			pages, total = *data.Pages, *data.Total
		}
		if *data.Pages != pages || *data.Total != total || *data.Last != (page+1 >= pages) {
			return provider.Snapshot{}, ranjit.ErrProviderUnavailable
		}
		for _, entry := range data.Transactions {
			tx, err := normalize(entry)
			if err != nil {
				return provider.Snapshot{}, err
			}
			if tx.PostedOn < start || tx.PostedOn > finish {
				return provider.Snapshot{}, fmt.Errorf("%w: Inter transaction outside requested dates", ranjit.ErrInvalid)
			}
			batch.Transactions = append(batch.Transactions, tx)
		}
		if page+1 >= pages {
			if len(batch.Transactions) != total || pages == 0 && total != 0 {
				return provider.Snapshot{}, ranjit.ErrProviderUnavailable
			}
			if err := batch.Validate(); err != nil {
				return provider.Snapshot{}, err
			}
			return batch, nil
		}
		if len(data.Transactions) == 0 {
			return provider.Snapshot{}, ranjit.ErrProviderUnavailable
		}
	}
	return provider.Snapshot{}, ranjit.ErrProviderUnavailable
}

func normalize(t transaction) (provider.Transaction, error) {
	invalid := fmt.Errorf("%w: invalid Inter transaction", ranjit.ErrInvalid)
	if t.ID == "" || len(t.ID) > 256 || strings.TrimSpace(t.ID) != t.ID || strings.ContainsAny(t.ID, "\x00\r\n") {
		return provider.Transaction{}, invalid
	}
	amount, err := money(t.Value)
	if err != nil {
		return provider.Transaction{}, err
	}
	if amount.AmountMinor <= 0 || t.Operation != "C" && t.Operation != "D" {
		return provider.Transaction{}, invalid
	}
	if t.Operation == "D" {
		amount.AmountMinor = -amount.AmountMinor
	}
	var date string
	for _, layout := range []string{time.DateOnly, time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if parsed, err := time.Parse(layout, t.Date); err == nil {
			date = parsed.Format(time.DateOnly)
			break
		}
	}
	if date == "" {
		return provider.Transaction{}, invalid
	}
	description := strings.TrimSpace(t.Description)
	if description == "" {
		description = strings.TrimSpace(t.Title)
	}
	if description == "" {
		description = "Lançamento Inter"
	}
	return provider.Transaction{ExternalID: t.ID, AccountExternalID: "own-account", Description: description, Amount: amount, PostedOn: date, Status: "booked"}, nil
}

func money(raw json.RawMessage) (ranjit.Money, error) {
	return provider.ParseBRL(raw)
}
