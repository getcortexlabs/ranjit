package sandbox

import (
	"context"
	"fmt"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

type Source struct{}

func (Source) Institutions() []ranjit.Institution {
	return []ranjit.Institution{
		{ID: "sandbox-bank", Name: "Banco de teste", Provider: "sandbox", Sandbox: true, ConnectionMode: "sandbox_consent", DataCoverage: []string{"synthetic_accounts", "synthetic_transactions"}},
		{ID: "sandbox-credit-union", Name: "Cooperativa de teste", Provider: "sandbox", Sandbox: true, ConnectionMode: "sandbox_consent", DataCoverage: []string{"synthetic_accounts", "synthetic_transactions"}},
	}
}

func (s Source) Fetch(ctx context.Context, c ranjit.Connection) (provider.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return provider.Snapshot{}, err
	}
	found := false
	for _, i := range s.Institutions() {
		if i.ID == c.InstitutionID {
			found = true
		}
	}
	if !found || c.Provider != "sandbox" {
		return provider.Snapshot{}, fmt.Errorf("%w: unknown institution", ranjit.ErrInvalid)
	}
	// Fixed dates and upstream IDs make retries and fixtures reproducible.
	return provider.Snapshot{
		Accounts: []provider.Account{
			{ExternalID: "checking-1", Name: "Conta corrente", Type: "checking", Balance: ranjit.Money{AmountMinor: 482135, Currency: "BRL"}},
			{ExternalID: "savings-1", Name: "Reserva", Type: "savings", Balance: ranjit.Money{AmountMinor: 1250000, Currency: "BRL"}},
		},
		Transactions: []provider.Transaction{
			{ExternalID: "tx-001", AccountExternalID: "checking-1", Description: "Salário", Amount: ranjit.Money{AmountMinor: 650000, Currency: "BRL"}, PostedOn: "2026-11-02", Status: "booked"},
			{ExternalID: "tx-002", AccountExternalID: "checking-1", Description: "Mercado", Amount: ranjit.Money{AmountMinor: -32745, Currency: "BRL"}, PostedOn: "2026-11-03", Status: "booked"},
			{ExternalID: "tx-003", AccountExternalID: "checking-1", Description: "Aluguel", Amount: ranjit.Money{AmountMinor: -135120, Currency: "BRL"}, PostedOn: "2026-11-04", Status: "booked"},
			{ExternalID: "tx-004", AccountExternalID: "savings-1", Description: "Depósito", Amount: ranjit.Money{AmountMinor: 1250000, Currency: "BRL"}, PostedOn: "2026-11-01", Status: "booked"},
		},
	}, nil
}
