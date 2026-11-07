package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/getcortexlabs/ranjit"
)

func (s *Store) CreateConnection(ctx context.Context, c ranjit.Connection, tokenHash string, deadline time.Time) error {
	return insertConnection(ctx, s.db, c, tokenHash, deadline)
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertConnection(ctx context.Context, db executor, c ranjit.Connection, tokenHash any, deadline time.Time) error {
	if c.AuthorizationMode == "" && c.Provider == "sandbox" {
		c.AuthorizationMode = "sandbox_consent"
	}
	_, err := db.ExecContext(ctx, `INSERT INTO connections
		(id, customer_id, institution_id, provider, status, created_at, consent_expires_at, token_hash, authorization_expires_at, authorization_mode, credential_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, c.CustomerID, c.InstitutionID, c.Provider, c.Status, c.CreatedAt.UnixMilli(), c.ConsentExpiresAt.UnixMilli(), tokenHash, deadline.UnixMilli(), c.AuthorizationMode, c.CredentialID)
	return err
}

// CreateAuthorizedConnection persists the operator's local grant and first job atomically.
func (s *Store) CreateAuthorizedConnection(ctx context.Context, c ranjit.Connection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = insertConnection(ctx, tx, c, nil, time.Time{}); err != nil {
		return err
	}
	if _, err = enqueue(ctx, tx, c.ID, c.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Connection(ctx context.Context, id string, now time.Time) (ranjit.Connection, error) {
	c, err := scanConnection(s.db.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM connections WHERE id = ?`, id))
	if err == nil && c.Provider != "file" && c.Status == "active" && !c.ConsentExpiresAt.After(now) {
		c.Status = "expired"
	}
	return c, err
}

func (s *Store) Connections(ctx context.Context, customer, cursor string, limit int, now time.Time) (ranjit.Page[ranjit.Connection], error) {
	page := ranjit.Page[ranjit.Connection]{Data: []ranjit.Connection{}}
	rows, err := s.db.QueryContext(ctx, `SELECT `+connectionColumns+` FROM connections WHERE customer_id = ? AND id > ? ORDER BY id LIMIT ?`, customer, cursor, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return page, err
		}
		if c.Provider != "file" && c.Status == "active" && !c.ConsentExpiresAt.After(now) {
			c.Status = "expired"
		}
		page.Data = append(page.Data, c)
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		page.NextCursor = page.Data[limit-1].ID
	}
	return page, rows.Err()
}

func (s *Store) PendingConsent(ctx context.Context, hash string, now time.Time) (ranjit.Connection, error) {
	c, err := scanConnection(s.db.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM connections
		WHERE token_hash = ? AND status = 'pending' AND authorization_expires_at > ? AND consent_expires_at > ?`, hash, now.UnixMilli(), now.UnixMilli()))
	if errors.Is(err, ranjit.ErrNotFound) {
		return c, ranjit.ErrConsent
	}
	return c, err
}

// Decide consumes the authorization token and queues the first sync in the same transaction.
func (s *Store) Decide(ctx context.Context, hash string, approve bool, now time.Time) (ranjit.Connection, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ranjit.Connection{}, err
	}
	defer tx.Rollback()
	status := "denied"
	if approve {
		status = "active"
	}
	c, err := scanConnection(tx.QueryRowContext(ctx, `UPDATE connections SET status = ?, token_hash = NULL
		WHERE token_hash = ? AND status = 'pending' AND authorization_expires_at > ? AND consent_expires_at > ? RETURNING `+connectionColumns,
		status, hash, now.UnixMilli(), now.UnixMilli()))
	if errors.Is(err, ranjit.ErrNotFound) {
		return c, ranjit.ErrConsent
	}
	if err != nil {
		return c, err
	}
	if approve {
		if _, err = enqueue(ctx, tx, c.ID, now); err != nil {
			return c, err
		}
	}
	return c, tx.Commit()
}

// Revoke removes normalized data before returning. The connection record is retained.
func (s *Store) Revoke(ctx context.Context, id string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE connections SET status = 'revoked', token_hash = NULL WHERE id = ?`, id)
	if err = changed(result, err, ranjit.ErrNotFound); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM accounts WHERE connection_id = ?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status = 'cancelled', updated_at = ?, lease_token = '', lease_until = 0
		WHERE connection_id = ? AND status IN ('queued','running')`, now.UnixMilli(), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Expire(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE connections SET status = 'expired', token_hash = NULL
		WHERE (provider <> 'file' AND status = 'active' AND consent_expires_at <= ?) OR (status = 'pending' AND authorization_expires_at <= ?)`, now.UnixMilli(), now.UnixMilli()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM accounts WHERE connection_id IN (SELECT id FROM connections WHERE status = 'expired')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status = 'cancelled', updated_at = ?, lease_token = '', lease_until = 0
		WHERE status IN ('queued','running') AND connection_id IN (SELECT id FROM connections WHERE status = 'expired')`, now.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func active(ctx context.Context, tx *sql.Tx, id string, now time.Time) error {
	var status, source string
	var expires int64
	err := tx.QueryRowContext(ctx, `SELECT status, consent_expires_at, provider FROM connections WHERE id = ?`, id).Scan(&status, &expires, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return ranjit.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "active" || source != "file" && expires <= now.UnixMilli() {
		return ranjit.ErrConflict
	}
	return nil
}
