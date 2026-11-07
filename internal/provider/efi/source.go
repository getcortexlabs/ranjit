package efi

import (
	"context"
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
	name := "Efí — saldo e Pix recebidos"
	if s.environment == "homologation" {
		name += " (homologação)"
	}
	return []ranjit.Institution{{ID: "efi", Name: name, Provider: "efi", Sandbox: s.environment == "homologation", ConnectionMode: "api_credentials", DataCoverage: []string{"balance", "pix_received", "pix_received_refunds"}}}
}

type receipt struct {
	EndToEndID  string `json:"endToEndId"`
	Value       string `json:"valor"`
	Time        string `json:"horario"`
	Description string `json:"infoPagador"`
	Refunds     []struct {
		ID     string `json:"id"`
		Value  string `json:"valor"`
		Status string `json:"status"`
		Time   struct {
			Settled string `json:"liquidacao"`
		} `json:"horario"`
	} `json:"devolucoes"`
}

type receiptPage struct {
	Parameters struct {
		Pagination *struct {
			Current *int `json:"paginaAtual"`
			Pages   *int `json:"quantidadeDePaginas"`
			Total   *int `json:"quantidadeTotalDeItens"`
		} `json:"paginacao"`
	} `json:"parametros"`
	Pix []receipt `json:"pix"`
}

func (s *Source) balance(ctx context.Context) (ranjit.Money, error) {
	var data struct {
		Balance string `json:"saldo"`
	}
	if err := s.get(ctx, "/v2/gn/saldo", &data); err != nil {
		return ranjit.Money{}, err
	}
	return money(data.Balance)
}

func (s *Source) Fetch(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
	if c.Provider != "efi" || c.InstitutionID != "efi" || c.AuthorizationMode != "api_credentials" || c.CredentialID != s.CredentialID() {
		return provider.Snapshot{}, ranjit.ErrProviderCredentials
	}
	if c.Status != "active" || !c.ConsentExpiresAt.After(s.now()) {
		return provider.Snapshot{}, ranjit.ErrConflict
	}
	balance, err := s.balance(ctx)
	if err != nil {
		return provider.Snapshot{}, err
	}
	batch := provider.Snapshot{Accounts: []provider.Account{{ExternalID: "own-account", Name: "Conta Efí", Type: "payment", Balance: balance}}}
	end := s.now().UTC().Truncate(time.Second)
	query := url.Values{"inicio": {end.Add(-s.lookback).Format(time.RFC3339)}, "fim": {end.Format(time.RFC3339)}, "paginacao.itensPorPagina": {"100"}}
	pages, total, received := -1, -1, 0
	for page := 0; page < maxPages; page++ {
		query.Set("paginacao.paginaAtual", strconv.Itoa(page))
		var data receiptPage
		if err := s.get(ctx, "/v2/pix?"+query.Encode(), &data); err != nil {
			return provider.Snapshot{}, err
		}
		p := data.Parameters.Pagination
		if p == nil || p.Current == nil || p.Pages == nil || p.Total == nil || data.Pix == nil || *p.Current != page || *p.Pages < 0 || *p.Pages > maxPages || *p.Total < 0 || *p.Total > maxPages*100 {
			return provider.Snapshot{}, fmt.Errorf("%w: invalid or oversized Efí pagination", ranjit.ErrInvalid)
		}
		if page == 0 {
			pages, total = *p.Pages, *p.Total
		}
		if *p.Pages != pages || *p.Total != total {
			return provider.Snapshot{}, ranjit.ErrProviderUnavailable
		}
		for _, pix := range data.Pix {
			transactions, err := normalize(pix)
			if err != nil {
				return provider.Snapshot{}, err
			}
			batch.Transactions = append(batch.Transactions, transactions...)
		}
		received += len(data.Pix)
		if page+1 >= pages {
			if received != total || pages == 0 && received != 0 {
				return provider.Snapshot{}, ranjit.ErrProviderUnavailable
			}
			if err := batch.Validate(); err != nil {
				return provider.Snapshot{}, err
			}
			return batch, nil
		}
		if len(data.Pix) == 0 {
			return provider.Snapshot{}, ranjit.ErrProviderUnavailable
		}
	}
	return provider.Snapshot{}, ranjit.ErrProviderUnavailable
}

func normalize(p receipt) ([]provider.Transaction, error) {
	if p.EndToEndID == "" || len(p.EndToEndID) > 64 || strings.ContainsAny(p.EndToEndID, "/\x00 \r\n\t") {
		return nil, fmt.Errorf("%w: missing or invalid Pix identifier", ranjit.ErrInvalid)
	}
	amount, err := money(p.Value)
	if err != nil {
		return nil, err
	}
	if amount.AmountMinor <= 0 {
		return nil, fmt.Errorf("%w: received Pix must be positive", ranjit.ErrInvalid)
	}
	posted, err := postedOn(p.Time)
	if err != nil {
		return nil, err
	}
	description := strings.TrimSpace(p.Description)
	if description == "" {
		description = "Pix recebido"
	}
	transactions := []provider.Transaction{{ExternalID: p.EndToEndID, AccountExternalID: "own-account", Description: description, Amount: amount, PostedOn: posted, Status: "booked"}}
	var refunded int64
	for _, refund := range p.Refunds {
		if refund.Status == "EM_PROCESSAMENTO" || refund.Status == "NAO_REALIZADO" {
			continue
		}
		if refund.Status != "DEVOLVIDO" || refund.ID == "" || len(refund.ID) > 64 || strings.ContainsAny(refund.ID, "/\x00 \r\n\t") {
			return nil, fmt.Errorf("%w: invalid Pix refund", ranjit.ErrInvalid)
		}
		value, err := money(refund.Value)
		if err != nil {
			return nil, err
		}
		if value.AmountMinor <= 0 || value.AmountMinor > amount.AmountMinor-refunded {
			return nil, fmt.Errorf("%w: invalid Pix refund amount", ranjit.ErrInvalid)
		}
		refunded += value.AmountMinor
		date, err := postedOn(refund.Time.Settled)
		if err != nil {
			return nil, err
		}
		value.AmountMinor = -value.AmountMinor
		transactions = append(transactions, provider.Transaction{ExternalID: p.EndToEndID + "/refund/" + refund.ID, AccountExternalID: "own-account", Description: "Devolução de Pix recebido", Amount: value, PostedOn: date, Status: "booked"})
	}
	return transactions, nil
}

func postedOn(value string) (string, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", fmt.Errorf("%w: invalid Efí timestamp", ranjit.ErrInvalid)
	}
	return t.UTC().Format(time.DateOnly), nil
}

// Decimal strings are converted without floating point or rounding.
func money(value string) (ranjit.Money, error) {
	invalid := fmt.Errorf("%w: invalid Efí amount", ranjit.ErrInvalid)
	negative := strings.HasPrefix(value, "-")
	digits := strings.TrimPrefix(value, "-")
	whole, cents, ok := strings.Cut(digits, ".")
	if !ok || whole == "" || len(whole) > 14 || len(cents) != 2 {
		return ranjit.Money{}, invalid
	}
	for _, c := range whole + cents {
		if c < '0' || c > '9' {
			return ranjit.Money{}, invalid
		}
	}
	minor, err := strconv.ParseInt(whole+cents, 10, 64)
	if err != nil {
		return ranjit.Money{}, invalid
	}
	if negative {
		minor = -minor
	}
	result := ranjit.Money{AmountMinor: minor, Currency: "BRL"}
	if err := result.Validate(); err != nil {
		return ranjit.Money{}, invalid
	}
	return result, nil
}
