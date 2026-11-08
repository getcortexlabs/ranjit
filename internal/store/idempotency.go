package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/getcortexlabs/ranjit"
)

const IdempotencyTTL = 24 * time.Hour

func replayConnection(ctx context.Context, tx *transaction, key, fingerprint string, now time.Time) (ranjit.Connection, time.Time, error) {
	var id, stored string
	err := tx.QueryRowContext(ctx, `SELECT connection_id, request_hash FROM connection_requests WHERE key_hash=? AND expires_at>?`, key, now.UnixMilli()).Scan(&id, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return ranjit.Connection{}, time.Time{}, ranjit.ErrNotFound
	}
	if err != nil {
		return ranjit.Connection{}, time.Time{}, err
	}
	if stored != fingerprint {
		return ranjit.Connection{}, time.Time{}, ranjit.ErrIdempotency
	}
	c, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM connections WHERE id=?`, id))
	if err != nil {
		return c, time.Time{}, err
	}
	var deadline int64
	err = tx.QueryRowContext(ctx, `SELECT authorization_expires_at FROM connections WHERE id=?`, id).Scan(&deadline)
	if c.Status == "active" && !c.ConsentExpiresAt.After(now) || c.Status == "pending" && deadline <= now.UnixMilli() {
		c.Status = "expired"
	}
	return c, instant(deadline), err
}

func (s *Store) ReplayConnection(ctx context.Context, key, fingerprint string, now time.Time) (ranjit.Connection, time.Time, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ranjit.Connection{}, time.Time{}, err
	}
	defer tx.Rollback()
	return replayConnection(ctx, tx, key, fingerprint, now)
}

// CreateConnectionOnce commits the request identity, grant and initial job together.
// Concurrent callers may verify credentials, but only the winning grant is persisted.
func (s *Store) CreateConnectionOnce(ctx context.Context, c ranjit.Connection, tokenHash any, deadline time.Time, key, fingerprint string) (ranjit.Connection, time.Time, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, deadline, err
	}
	defer tx.Rollback()
	if key != "" {
		prior, until, err := replayConnection(ctx, tx, key, fingerprint, c.CreatedAt)
		if !errors.Is(err, ranjit.ErrNotFound) {
			return prior, until, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM connection_requests WHERE expires_at<=?`, c.CreatedAt.UnixMilli()); err != nil {
			return c, deadline, err
		}
	}
	if err = insertConnection(ctx, tx, c, tokenHash, deadline); err != nil {
		return c, deadline, err
	}
	if c.Status == "active" {
		if _, err = enqueue(ctx, tx, c.ID, c.CreatedAt); err != nil {
			return c, deadline, err
		}
	}
	if key != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO connection_requests(key_hash,request_hash,connection_id,expires_at) VALUES (?,?,?,?)`, key, fingerprint, c.ID, c.CreatedAt.Add(IdempotencyTTL).UnixMilli()); err != nil {
			return c, deadline, err
		}
	}
	return c, deadline, tx.Commit()
}
