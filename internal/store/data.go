package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

func (s *Store) Complete(ctx context.Context, j ranjit.Job, batch provider.Snapshot, now time.Time) error {
	if err := batch.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, j.ConnectionID, now); err != nil {
		return err
	}
	var receivedAt int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(received_at),?) FROM sync_responses WHERE job_id=?`, now.UnixMilli(), j.ID).Scan(&receivedAt); err != nil {
		return err
	}
	for i := range batch.Accounts {
		if batch.Accounts[i].BalanceAt.IsZero() {
			batch.Accounts[i].BalanceAt = instant(receivedAt)
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'succeeded', error_code = '', updated_at = ?, lease_token = '', lease_until = 0
		WHERE id = ? AND connection_id = ? AND status = 'running' AND lease_token = ? AND lease_until > ?`, now.UnixMilli(), j.ID, j.ConnectionID, j.LeaseToken, now.UnixMilli())
	if err = changed(result, err, ranjit.ErrLease); err != nil {
		return err
	}
	if err = writeSnapshot(ctx, tx, j.ConnectionID, batch, now); err != nil {
		return err
	}
	return tx.Commit()
}

func writeSnapshot(ctx context.Context, tx *sql.Tx, connectionID string, batch provider.Snapshot, now time.Time) error {
	for _, a := range batch.Accounts {
		id := ranjit.StableID("acc", connectionID, a.ExternalID)
		at := a.BalanceAt
		if at.IsZero() {
			at = now
		}
		if a.BalanceUnknown {
			at = time.UnixMilli(0)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO accounts (id, connection_id, external_id, name, type, balance_minor, currency, updated_at, balance_known, balance_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (connection_id, external_id) DO UPDATE SET
			name = excluded.name, type = excluded.type, updated_at = excluded.updated_at,
			balance_minor = CASE WHEN excluded.balance_known AND excluded.balance_at >= accounts.balance_at THEN excluded.balance_minor ELSE accounts.balance_minor END,
			currency = CASE WHEN excluded.balance_known AND excluded.balance_at >= accounts.balance_at THEN excluded.currency ELSE accounts.currency END,
			balance_known = MAX(accounts.balance_known, excluded.balance_known),
			balance_at = CASE WHEN excluded.balance_known THEN MAX(accounts.balance_at, excluded.balance_at) ELSE accounts.balance_at END`,
			id, connectionID, a.ExternalID, a.Name, a.Type, a.Balance.AmountMinor, a.Balance.Currency, now.UnixMilli(), !a.BalanceUnknown, at.UnixMilli())
		if err != nil {
			return err
		}
	}
	for _, t := range batch.Transactions {
		accountID := ranjit.StableID("acc", connectionID, t.AccountExternalID)
		id := ranjit.StableID("txn", accountID, t.ExternalID)
		_, err := tx.ExecContext(ctx, `INSERT INTO transactions (id, account_id, external_id, description, amount_minor, currency, posted_on, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (account_id, external_id) DO UPDATE SET
			description = excluded.description, amount_minor = excluded.amount_minor, currency = excluded.currency, posted_on = excluded.posted_on, status = excluded.status`,
			id, accountID, t.ExternalID, t.Description, t.Amount.AmountMinor, t.Amount.Currency, t.PostedOn, t.Status)
		if err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE connections SET last_synced_at = ? WHERE id = ?`, now.UnixMilli(), connectionID)
	return err
}

func (s *Store) Accounts(ctx context.Context, id, cursor string, limit int, now time.Time) (ranjit.Page[ranjit.Account], error) {
	page := ranjit.Page[ranjit.Account]{Data: []ranjit.Account{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, id, now); err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, connection_id, name, type, balance_minor, currency, updated_at, balance_known, balance_at FROM accounts
		WHERE connection_id = ? AND id > ? ORDER BY id LIMIT ?`, id, cursor, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var a ranjit.Account
		var updated, balanceAt int64
		var known bool
		var balance ranjit.Money
		if err = rows.Scan(&a.ID, &a.ConnectionID, &a.Name, &a.Type, &balance.AmountMinor, &balance.Currency, &updated, &known, &balanceAt); err != nil {
			return page, err
		}
		a.UpdatedAt = instant(updated)
		if known {
			a.Balance = &balance
			at := instant(balanceAt)
			a.BalanceAt = &at
		}
		page.Data = append(page.Data, a)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		page.NextCursor = page.Data[limit-1].ID
	}
	return page, nil
}

func (s *Store) Transactions(ctx context.Context, accountID, cursor string, limit int, now time.Time) (ranjit.Page[ranjit.Transaction], error) {
	page := ranjit.Page[ranjit.Transaction]{Data: []ranjit.Transaction{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	var connectionID string
	err = tx.QueryRowContext(ctx, `SELECT connection_id FROM accounts WHERE id = ?`, accountID).Scan(&connectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return page, ranjit.ErrNotFound
	}
	if err != nil {
		return page, err
	}
	if err = active(ctx, tx, connectionID, now); err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, account_id, description, amount_minor, currency, posted_on, status FROM transactions
		WHERE account_id = ? AND id > ? ORDER BY id LIMIT ?`, accountID, cursor, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var t ranjit.Transaction
		if err = rows.Scan(&t.ID, &t.AccountID, &t.Description, &t.Amount.AmountMinor, &t.Amount.Currency, &t.PostedOn, &t.Status); err != nil {
			return page, err
		}
		page.Data = append(page.Data, t)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		page.NextCursor = page.Data[limit-1].ID
	}
	return page, nil
}
