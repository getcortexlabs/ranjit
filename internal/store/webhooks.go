package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/getcortexlabs/ranjit"
)

const WebhookMaxAttempts = 8
const WebhookRetention = 7 * 24 * time.Hour

// EnableWebhooks must be called before starting workers. A changed destination
// does not inherit deliveries containing another endpoint's customer metadata.
func (s *Store) EnableWebhooks(destination string) {
	s.webhookTarget = ""
	if destination != "" {
		hash := sha256.Sum256([]byte(destination))
		s.webhookTarget = hex.EncodeToString(hash[:])
	}
}

func (s *Store) queueWebhook(ctx context.Context, tx *transaction, j ranjit.Job, eventType string, now time.Time) error {
	if s.webhookTarget == "" {
		return nil
	}
	event := ranjit.WebhookEvent{ID: ranjit.NewID("evt"), Type: eventType, CreatedAt: now.UTC(), Data: ranjit.WebhookData{JobID: j.ID, ConnectionID: j.ConnectionID, ErrorCode: j.ErrorCode}}
	if err := tx.QueryRowContext(ctx, `SELECT customer_id FROM connections WHERE id=?`, j.ConnectionID).Scan(&event.Data.CustomerID); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO webhook_deliveries(id,job_id,connection_id,target_hash,event_type,payload,status,created_at,updated_at,available_at)
        VALUES (?,?,?,?,?,?,'queued',?,?,?) ON CONFLICT(job_id,event_type) DO NOTHING`, event.ID, j.ID, j.ConnectionID, s.webhookTarget, eventType, string(data), now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	return err
}

type WebhookAttempt struct {
	ID         string
	Payload    []byte
	Attempts   int
	LeaseToken string
}

func (s *Store) ClaimWebhook(ctx context.Context, now time.Time) (WebhookAttempt, error) {
	var out WebhookAttempt
	if s.webhookTarget == "" {
		return out, ranjit.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE updated_at<?`, now.Add(-WebhookRetention).UnixMilli()); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE webhook_deliveries SET status='failed',error_code='lease_exhausted',lease_token='',lease_until=0,updated_at=? WHERE target_hash=? AND status='running' AND lease_until<=? AND attempts>=?`, now.UnixMilli(), s.webhookTarget, now.UnixMilli(), WebhookMaxAttempts); err != nil {
		return out, err
	}
	var payload string
	err = tx.QueryRowContext(ctx, `UPDATE webhook_deliveries SET status='running',attempts=attempts+1,lease_until=?,lease_token=?,updated_at=? WHERE id=(
        SELECT id FROM webhook_deliveries WHERE target_hash=? AND attempts<? AND ((status='queued' AND available_at<=?) OR (status='running' AND lease_until<=?)) ORDER BY available_at,id LIMIT 1)
        RETURNING id,payload,attempts,lease_token`, now.Add(30*time.Second).UnixMilli(), ranjit.NewID("lease"), now.UnixMilli(), s.webhookTarget, WebhookMaxAttempts, now.UnixMilli(), now.UnixMilli()).Scan(&out.ID, &payload, &out.Attempts, &out.LeaseToken)
	if errors.Is(err, sql.ErrNoRows) {
		err = ranjit.ErrNotFound
	}
	if err != nil && !errors.Is(err, ranjit.ErrNotFound) {
		return out, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return out, commitErr
	}
	out.Payload = []byte(payload)
	return out, err
}

func (s *Store) FinishWebhook(ctx context.Context, attempt WebhookAttempt, statusCode int, now time.Time) error {
	status, code := "delivered", ""
	if statusCode < 200 || statusCode >= 300 {
		status, code = "queued", "http_error"
		if statusCode == 0 {
			code = "transport_error"
		}
		if attempt.Attempts >= WebhookMaxAttempts {
			status = "failed"
		}
	}
	delay := min(10*time.Second*time.Duration(1<<min(attempt.Attempts-1, 10)), time.Hour)
	result, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries SET status=?,error_code=?,http_status=?,updated_at=?,available_at=?,lease_until=0,lease_token='' WHERE id=? AND target_hash=? AND status='running' AND lease_token=? AND lease_until>?`, status, code, statusCode, now.UnixMilli(), now.Add(delay).UnixMilli(), attempt.ID, s.webhookTarget, attempt.LeaseToken, now.UnixMilli())
	return changed(result, err, ranjit.ErrLease)
}

func (s *Store) WebhookDeliveries(ctx context.Context, cursor string, limit int) (ranjit.Page[ranjit.WebhookDelivery], error) {
	page := ranjit.Page[ranjit.WebhookDelivery]{Data: []ranjit.WebhookDelivery{}}
	rows, err := s.db.QueryContext(ctx, `SELECT payload,status,attempts,http_status,error_code,updated_at,available_at FROM webhook_deliveries WHERE target_hash=? AND id>? ORDER BY id LIMIT ?`, s.webhookTarget, cursor, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var d ranjit.WebhookDelivery
		var payload string
		var updated, available int64
		if err = rows.Scan(&payload, &d.Status, &d.Attempts, &d.HTTPStatus, &d.ErrorCode, &updated, &available); err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(payload), &d.Event); err != nil {
			return page, err
		}
		d.UpdatedAt = instant(updated)
		if d.Status == "queued" {
			at := instant(available)
			d.NextAttemptAt = &at
		}
		page.Data = append(page.Data, d)
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		page.NextCursor = page.Data[limit-1].Event.ID
	}
	return page, rows.Err()
}

func (s *Store) RetryWebhook(ctx context.Context, id string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE webhook_deliveries SET status='queued',attempts=0,http_status=0,error_code='',updated_at=?,available_at=? WHERE id=? AND target_hash=? AND status='failed'`, now.UnixMilli(), now.UnixMilli(), id, s.webhookTarget)
	return changed(result, err, ranjit.ErrWebhookState)
}
