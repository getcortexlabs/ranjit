package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/getcortexlabs/ranjit"
)

const MaxAttempts = 5
const jobColumns = `id, connection_id, status, attempts, error_code, created_at, updated_at, lease_token`

func scanJob(row scanner) (ranjit.Job, error) {
	var j ranjit.Job
	var created, updated int64
	err := row.Scan(&j.ID, &j.ConnectionID, &j.Status, &j.Attempts, &j.ErrorCode, &created, &updated, &j.LeaseToken)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ranjit.ErrNotFound
	}
	j.CreatedAt, j.UpdatedAt = instant(created), instant(updated)
	return j, err
}

func (s *Store) Job(ctx context.Context, id string) (ranjit.Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
}

func enqueue(ctx context.Context, tx *sql.Tx, id string, now time.Time) (ranjit.Job, error) {
	_, err := tx.ExecContext(ctx, `INSERT INTO jobs (id, connection_id, status, created_at, updated_at, available_at)
		VALUES (?, ?, 'queued', ?, ?, ?) ON CONFLICT DO NOTHING`, ranjit.NewID("job"), id, now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return ranjit.Job{}, err
	}
	return scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE connection_id = ? AND status IN ('queued','running')`, id))
}

func (s *Store) Enqueue(ctx context.Context, id string, now time.Time) (ranjit.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ranjit.Job{}, err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, id, now); err != nil {
		return ranjit.Job{}, err
	}
	j, err := enqueue(ctx, tx, id, now)
	if err != nil {
		return j, err
	}
	return j, tx.Commit()
}

func (s *Store) Schedule(ctx context.Context, now time.Time, interval time.Duration) error {
	// Recent failed jobs also count toward the interval; exhausted retries must not spin forever.
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM connections c WHERE status = 'active' AND consent_expires_at > ?
		AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.connection_id = c.id AND (j.status IN ('queued','running') OR j.updated_at > ?))
		ORDER BY id LIMIT 100`, now.UnixMilli(), now.Add(-interval).UnixMilli())
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = s.Enqueue(ctx, id, now); err != nil && !errors.Is(err, ranjit.ErrConflict) {
			return err
		}
	}
	return nil
}

func (s *Store) Claim(ctx context.Context, now time.Time, lease time.Duration) (ranjit.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ranjit.Job{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', error_code = 'lease_exhausted', updated_at = ?, lease_token = ''
		WHERE status = 'running' AND lease_until <= ? AND attempts >= ?`, now.UnixMilli(), now.UnixMilli(), MaxAttempts); err != nil {
		return ranjit.Job{}, err
	}
	j, err := scanJob(tx.QueryRowContext(ctx, `UPDATE jobs SET status = 'running', attempts = attempts + 1,
		updated_at = ?, lease_until = ?, lease_token = ? WHERE id = (
		SELECT j.id FROM jobs j JOIN connections c ON c.id = j.connection_id
		WHERE ((j.status = 'queued' AND j.available_at <= ?) OR (j.status = 'running' AND j.lease_until <= ?))
		AND j.attempts < ? AND c.status = 'active' AND c.consent_expires_at > ?
		ORDER BY j.available_at, j.id LIMIT 1) RETURNING `+jobColumns,
		now.UnixMilli(), now.Add(lease).UnixMilli(), ranjit.NewID("lease"), now.UnixMilli(), now.UnixMilli(), MaxAttempts, now.UnixMilli()))
	if err != nil && !errors.Is(err, ranjit.ErrNotFound) {
		return j, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return j, commitErr
	}
	return j, err
}

func (s *Store) Fail(ctx context.Context, j ranjit.Job, code string, now time.Time) error {
	return s.FailAfter(ctx, j, code, now, 0)
}

func (s *Store) FailAfter(ctx context.Context, j ranjit.Job, code string, now time.Time, retryAfter time.Duration) error {
	status := "queued"
	if j.Attempts >= MaxAttempts || code == "invalid_provider_data" || code == "provider_credentials" {
		status = "failed"
	}
	delay := time.Duration(1<<min(j.Attempts, MaxAttempts)) * time.Second
	delay = max(delay, min(retryAfter, 5*time.Minute))
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, error_code = ?, updated_at = ?, available_at = ?, lease_token = '', lease_until = 0
		WHERE id = ? AND status = 'running' AND lease_token = ? AND lease_until > ?`, status, code, now.UnixMilli(), now.Add(delay).UnixMilli(), j.ID, j.LeaseToken, now.UnixMilli())
	return changed(result, err, ranjit.ErrLease)
}
