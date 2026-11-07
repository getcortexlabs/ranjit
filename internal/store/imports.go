package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/statement"
)

func (s *Store) Import(ctx context.Context, customer, account string, input statement.Statement, now time.Time) (ranjit.ImportResult, error) {
	result := ranjit.ImportResult{Accounts: len(input.Snapshot.Accounts), Transactions: len(input.Snapshot.Transactions), Deduplication: "transaction_id"}
	if !input.StableIDs {
		result.Deduplication = "exact_file"
	}
	if customer == "" || len(customer) > 128 || strings.ContainsAny(customer, "\x00\r\n") || len(account) > 64 || strings.ContainsAny(account, "\x00\r\n") {
		return result, ranjit.ErrInvalid
	}
	if err := input.Snapshot.Validate(); err != nil {
		return result, err
	}
	if len(input.Fingerprint) != 64 || len(input.FileHash) != 64 || len(input.Snapshot.Accounts) == 0 {
		return result, ranjit.ErrInvalid
	}
	if account == "" {
		account = "ofx-" + input.Fingerprint[:12]
	}
	id := ranjit.StableID("con", customer, "file:"+account)
	result.ConnectionID = id
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	c, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM connections WHERE id = ?`, id))
	if errors.Is(err, ranjit.ErrNotFound) {
		c = ranjit.Connection{ID: id, CustomerID: customer, InstitutionID: "file:" + account, Provider: "file", Status: "active", CreatedAt: now, AuthorizationMode: "file_import", CredentialID: input.Fingerprint}
		if err = insertConnection(ctx, tx, c, nil, time.Time{}); err != nil {
			return result, err
		}
	} else if err != nil {
		return result, err
	}
	if c.Status != "active" {
		return result, ranjit.ErrConflict
	}
	if c.CredentialID != input.Fingerprint {
		return result, fmt.Errorf("%w: this account name belongs to another account or file format; choose another --account", ranjit.ErrInvalid)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM imports WHERE connection_id=? AND file_hash=?`, id, input.FileHash).Scan(&count); err != nil {
		return result, err
	}
	if count > 0 {
		result.Duplicate = true
		return result, nil
	}
	// Without stable bank IDs, a second overlapping export cannot be reconciled
	// safely. Equal dates/amounts/descriptions can be separate real transactions.
	if input.From != "" {
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM imports WHERE connection_id=? AND date_from<>'' AND date_from<=? AND date_through>=? AND (stable_ids=0 OR ?=0)`, id, input.Through, input.From, input.StableIDs).Scan(&count); err != nil {
			return result, err
		}
		if count > 0 {
			return result, fmt.Errorf("%w: overlapping CSV exports need stable transaction IDs; include an id column or export a non-overlapping period", ranjit.ErrInvalid)
		}
	}
	for i := range input.Snapshot.Accounts {
		if len(input.Snapshot.Accounts) == 1 {
			input.Snapshot.Accounts[i].Name = account
		}
	}
	if err = writeSnapshot(ctx, tx, id, input.Snapshot, now); err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO imports (connection_id,file_hash,date_from,date_through,stable_ids,imported_at) VALUES (?,?,?,?,?,?)`, id, input.FileHash, input.From, input.Through, input.StableIDs, now.UnixMilli())
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (s *Store) Status(ctx context.Context, id string, now time.Time) (ranjit.ConnectionStatus, error) {
	var result ranjit.ConnectionStatus
	c, err := s.Connection(ctx, id, now)
	if err != nil {
		return result, err
	}
	result.Connection = c
	j, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE connection_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, id))
	if err == nil {
		result.LatestJob = &j
	} else if !errors.Is(err, ranjit.ErrNotFound) {
		return result, err
	}
	if c.Status != "active" {
		return result, nil
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE connection_id=?`, id).Scan(&result.Accounts)
	if err != nil {
		return result, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MIN(posted_on),''),COALESCE(MAX(posted_on),'') FROM transactions WHERE account_id IN (SELECT id FROM accounts WHERE connection_id=?)`, id).Scan(&result.Transactions, &result.DataFrom, &result.DataThrough)
	return result, err
}
