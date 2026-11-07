package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

type Checkpoint struct {
	Store *Store
	Job   ranjit.Job
	Now   func() time.Time
}

func requestHash(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:])
}

func (c Checkpoint) Load(ctx context.Context, endpoint string) ([]byte, error) {
	var valid int
	if err := c.Store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE id=? AND status='running' AND lease_token=? AND lease_until>?`, c.Job.ID, c.Job.LeaseToken, c.Now().UnixMilli()).Scan(&valid); err != nil {
		return nil, err
	}
	if valid != 1 {
		return nil, ranjit.ErrLease
	}
	var body []byte
	err := c.Store.db.QueryRowContext(ctx, `SELECT body FROM sync_responses r JOIN jobs j ON j.id=r.job_id WHERE job_id=? AND request_hash=? AND j.status='running' AND j.lease_token=? AND j.lease_until>?`, c.Job.ID, requestHash(endpoint), c.Job.LeaseToken, c.Now().UnixMilli()).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return body, err
}

func (c Checkpoint) Save(ctx context.Context, endpoint string, body []byte) error {
	if len(body) > provider.MaxResponseBytes {
		return ranjit.ErrInvalid
	}
	tx, err := c.Store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, c.Job.ConnectionID, c.Now()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE jobs SET requests_completed=requests_completed+1,updated_at=? WHERE id=? AND status='running' AND lease_token=? AND lease_until>?`, c.Now().UnixMilli(), c.Job.ID, c.Job.LeaseToken, c.Now().UnixMilli())
	if err = changed(result, err, ranjit.ErrLease); err != nil {
		return err
	}
	var size, count int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(LENGTH(body)),0),COUNT(*) FROM sync_responses WHERE job_id=?`, c.Job.ID).Scan(&size, &count); err != nil {
		return err
	}
	if size+len(body) > 32<<20 || count >= 110 {
		return ranjit.ErrInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_responses(job_id,request_hash,body,received_at) VALUES(?,?,?,?)`, c.Job.ID, requestHash(endpoint), body, c.Now().UnixMilli())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Yield continues a healthy, long sync without spending a failure attempt.
func (s *Store) Yield(ctx context.Context, j ranjit.Job, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET status='queued',attempts=attempts-1,updated_at=?,available_at=?,lease_token='',lease_until=0 WHERE id=? AND status='running' AND lease_token=? AND lease_until>? AND requests_completed>?`, now.UnixMilli(), now.UnixMilli(), j.ID, j.LeaseToken, now.UnixMilli(), j.RequestsCompleted)
	return changed(result, err, ranjit.ErrLease)
}
