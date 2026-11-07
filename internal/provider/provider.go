// Package provider is the boundary for fetching normalized financial data.
// Consent protocols belong to each integration; this interface does not implement OAuth.
package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
)

type Source interface {
	Institutions() []ranjit.Institution
	Fetch(context.Context, ranjit.Connection) (Snapshot, error)
}

type Account struct {
	ExternalID string
	Name       string
	Type       string
	Balance    ranjit.Money
}

type Transaction struct {
	ExternalID        string
	AccountExternalID string
	Description       string
	Amount            ranjit.Money
	PostedOn          string
	Status            string
}

type Snapshot struct {
	Accounts     []Account
	Transactions []Transaction
}

// Validate rejects a whole batch before any write. A partial import must not advance a sync.
func (s Snapshot) Validate() error {
	accounts := make(map[string]bool, len(s.Accounts))
	for _, a := range s.Accounts {
		if !validID(a.ExternalID) || accounts[a.ExternalID] || strings.TrimSpace(a.Name) == "" || len(a.Name) > 256 || a.Type != "checking" && a.Type != "savings" {
			return fmt.Errorf("%w: malformed account", ranjit.ErrInvalid)
		}
		if err := a.Balance.Validate(); err != nil {
			return err
		}
		accounts[a.ExternalID] = true
	}
	seen := make(map[string]bool, len(s.Transactions))
	for _, t := range s.Transactions {
		key := t.AccountExternalID + "\x00" + t.ExternalID
		_, dateErr := time.Parse(time.DateOnly, t.PostedOn)
		if !validID(t.ExternalID) || !accounts[t.AccountExternalID] || seen[key] || dateErr != nil || strings.TrimSpace(t.Description) == "" || len(t.Description) > 1024 || t.Status != "booked" && t.Status != "pending" {
			return fmt.Errorf("%w: malformed transaction", ranjit.ErrInvalid)
		}
		if err := t.Amount.Validate(); err != nil {
			return err
		}
		seen[key] = true
	}
	return nil
}

func validID(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 256 && !strings.ContainsRune(s, '\x00')
}
