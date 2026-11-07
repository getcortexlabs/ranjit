package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
)

const MaxAttempts = 5
const jobColumns = `id, connection_id, status, attempts, error_code, created_at, updated_at, lease_token, requests_completed`

func scanJob(row scanner) (ranjit.Job, error) {
	var j ranjit.Job
	var created, updated int64
	err := row.Scan(&j.ID, &j.ConnectionID, &j.Status, &j.Attempts, &j.ErrorCode, &created, &updated, &j.LeaseToken, &j.RequestsCompleted)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ranjit.ErrNotFound
	}
	j.CreatedAt, j.UpdatedAt = instant(created), instant(updated)
	return j, err
}

func (s *Store) Job(ctx context.Context, id string) (ranjit.Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
}

func enqueue(ctx context.Context, tx *transaction, id string, now time.Time) (ranjit.Job, error) {
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
	var source string
	if err = tx.QueryRowContext(ctx, `SELECT provider FROM connections WHERE id = ?`, id).Scan(&source); err != nil {
		return ranjit.Job{}, err
	}
	if source == "file" {
		return ranjit.Job{}, ranjit.ErrConflict
	}
	j, err := enqueue(ctx, tx, id, now)
	if err != nil {
		return j, err
	}
	return j, tx.Commit()
}

func (s *Store) Schedule(ctx context.Context, now time.Time, interval time.Duration) error {
	return s.ScheduleSelected(ctx, now, interval, nil)
}

func (s *Store) ScheduleSelected(ctx context.Context, now time.Time, interval time.Duration, selected map[string]bool) error {
	filter := ""
	args := []any{now.UnixMilli(), now.Add(-interval).UnixMilli()}
	if selected != nil {
		if len(selected) == 0 {
			return nil
		}
		marks := []string{}
		for id := range selected {
			marks = append(marks, "?")
			args = append(args, id)
		}
		filter = " AND c.id IN (" + strings.Join(marks, ",") + ")"
	}
	// Recent failed jobs also count toward the interval; exhausted retries must not spin forever.
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM connections c WHERE provider <> 'file' AND status = 'active' AND consent_expires_at > ?
		AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.connection_id = c.id AND (j.status IN ('queued','running') OR j.updated_at > ?))
		`+filter+` ORDER BY id LIMIT 100`, args...)
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
	return s.ClaimSelected(ctx, now, lease, nil)
}

func (s *Store) ClaimSelected(ctx context.Context, now time.Time, lease time.Duration, selected map[string]bool) (ranjit.Job, error) {
	filter := ""
	args := []any{now.UnixMilli(), now.Add(lease).UnixMilli(), ranjit.NewID("lease"), now.UnixMilli(), now.UnixMilli(), MaxAttempts, now.UnixMilli()}
	if selected != nil {
		if len(selected) == 0 {
			return ranjit.Job{}, ranjit.ErrNotFound
		}
		marks := []string{}
		for id := range selected {
			marks = append(marks, "?")
			args = append(args, id)
		}
		filter = " AND c.id IN (" + strings.Join(marks, ",") + ")"
	}
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
		`+filter+` ORDER BY j.available_at, j.id LIMIT 1) RETURNING `+jobColumns, args...))
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

// ConfiguredConnections excludes profiles absent from this process. A separate
// CLI process may add a profile while the server still has its old configuration.
func (s *Store) ConfiguredConnections(ctx context.Context, institutions []string) (map[string]bool, error) {
	result := map[string]bool{}
	if len(institutions) == 0 {
		return result, nil
	}
	marks := []string{}
	args := []any{}
	for _, id := range institutions {
		marks = append(marks, "?")
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM connections WHERE institution_id IN (`+strings.Join(marks, ",")+`) AND status='active'`, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return result, err
		}
		result[id] = true
	}
	return result, rows.Err()
}
