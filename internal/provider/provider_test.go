package provider_test

import (
	"testing"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

func TestRejectMalformedProviderBatches(t *testing.T) {
	valid := func() provider.Snapshot {
		return provider.Snapshot{
			Accounts:     []provider.Account{{ExternalID: "a", Name: "Conta", Type: "checking", Balance: ranjit.Money{AmountMinor: 100, Currency: "BRL"}}},
			Transactions: []provider.Transaction{{ExternalID: "t", AccountExternalID: "a", Description: "Compra", Amount: ranjit.Money{AmountMinor: -100, Currency: "BRL"}, PostedOn: "2026-11-01", Status: "booked"}},
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*provider.Snapshot){
		"duplicate account":     func(s *provider.Snapshot) { s.Accounts = append(s.Accounts, s.Accounts[0]) },
		"duplicate transaction": func(s *provider.Snapshot) { s.Transactions = append(s.Transactions, s.Transactions[0]) },
		"missing account":       func(s *provider.Snapshot) { s.Transactions[0].AccountExternalID = "unknown" },
		"invalid date":          func(s *provider.Snapshot) { s.Transactions[0].PostedOn = "2026-02-30" },
		"unsafe integer":        func(s *provider.Snapshot) { s.Transactions[0].Amount.AmountMinor = 9007199254740992 },
		"currency":              func(s *provider.Snapshot) { s.Accounts[0].Balance.Currency = "USD" },
		"empty id":              func(s *provider.Snapshot) { s.Transactions[0].ExternalID = "" },
		"null id":               func(s *provider.Snapshot) { s.Accounts[0].ExternalID = "a\x00b" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := valid()
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("malformed batch accepted")
			}
		})
	}
}
