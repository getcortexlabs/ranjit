// Package validation checks a connector without adding data to the personal database.
package validation

import (
	"context"
	"errors"
	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
	"github.com/getcortexlabs/ranjit/internal/store"
	"os"
	"path/filepath"
	"time"
)

type Report struct {
	Version     string            `json:"version"`
	Provider    string            `json:"provider"`
	Environment string            `json:"environment"`
	CheckedAt   time.Time         `json:"checked_at"`
	Result      string            `json:"result"`
	Checks      map[string]string `json:"checks"`
	ErrorCode   string            `json:"error_code,omitempty"`
}

func Run(ctx context.Context, s provider.Source, version, environment string) (report Report, err error) {
	report = Report{Version: version, Environment: environment, CheckedAt: time.Now().UTC(), Result: "failed", Checks: map[string]string{"api_access": "pending", "snapshot": "pending", "repeat_import": "pending", "manual_statement_review": "pending"}}
	defer func() {
		if err != nil {
			report.ErrorCode = "check_failed"
			if errors.Is(err, ranjit.ErrProviderCredentials) {
				report.ErrorCode = "provider_credentials"
			}
			if errors.Is(err, ranjit.ErrProviderUnavailable) {
				report.ErrorCode = "provider_unavailable"
			}
			if errors.Is(err, context.DeadlineExceeded) {
				report.ErrorCode = "timeout"
			}
		}
	}()
	institutions := s.Institutions()
	if len(institutions) == 0 {
		return report, ranjit.ErrInvalid
	}
	institution := institutions[0]
	report.Provider = institution.Provider
	now := time.Now()
	connection := ranjit.Connection{ID: ranjit.NewID("con"), CustomerID: "validation", InstitutionID: institution.ID, Provider: institution.Provider, Status: "active", CreatedAt: now, ConsentExpiresAt: now.Add(time.Hour), AuthorizationMode: "sandbox_consent"}
	if bank, ok := s.(provider.CredentialSource); ok {
		connection.AuthorizationMode = "api_credentials"
		connection.CredentialID = bank.CredentialID()
		if err = bank.Verify(ctx); err != nil {
			return report, err
		}
		report.Checks["api_access"] = "passed"
	} else {
		report.Checks["api_access"] = "simulated"
	}
	snapshot, err := s.Fetch(ctx, connection)
	if err != nil {
		return report, err
	}
	if err = snapshot.Validate(); err != nil {
		return report, err
	}
	if len(snapshot.Accounts) == 0 {
		return report, errors.New("connector returned no accounts")
	}
	report.Checks["snapshot"] = "passed"
	// Replaying the same accepted response checks storage idempotency. It does not
	// claim that bank data matches an official statement or that IDs never change.
	dir, err := os.MkdirTemp("", "ranjit-validation-*")
	if err != nil {
		return report, err
	}
	defer os.RemoveAll(dir)
	db, err := store.Open(ctx, filepath.Join(dir, "check.db"))
	if err != nil {
		return report, err
	}
	defer db.Close()
	if err = db.CreateConnection(ctx, connection, "", time.Time{}); err != nil {
		return report, err
	}
	for range 2 {
		if _, err = db.Enqueue(ctx, connection.ID, time.Now()); err != nil {
			return report, err
		}
		job, claimErr := db.Claim(ctx, time.Now(), time.Minute)
		if claimErr != nil {
			return report, claimErr
		}
		if err = db.Complete(ctx, job, snapshot, time.Now()); err != nil {
			return report, err
		}
	}
	status, err := db.Status(ctx, connection.ID, time.Now())
	if err != nil {
		return report, err
	}
	if status.Transactions != len(snapshot.Transactions) {
		return report, errors.New("repeat import changed transaction count")
	}
	report.Checks["repeat_import"] = "passed"
	if institution.Provider == "sandbox" {
		report.Result = "simulated"
	} else if environment != "production" {
		report.Result = "test_environment_checked"
	} else {
		report.Result = "api_checked"
	}
	return report, nil
}
