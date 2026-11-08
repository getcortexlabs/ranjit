package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/getcortexlabs/ranjit"
)

func (s *Store) Queues(ctx context.Context, now time.Time) (ranjit.QueueStatus, ranjit.QueueStatus, error) {
	sync, err := s.queueStats(ctx, "jobs", "succeeded", "", nil, now)
	if err != nil {
		return sync, ranjit.QueueStatus{}, err
	}
	hooks, err := s.queueStats(ctx, "webhook_deliveries", "delivered", " WHERE target_hash=?", []any{s.webhookTarget}, now)
	return sync, hooks, err
}

func (s *Store) queueStats(ctx context.Context, table, success, filter string, args []any, now time.Time) (ranjit.QueueStatus, error) {
	out := ranjit.QueueStatus{Counts: map[string]int64{"queued": 0, "running": 0, "failed": 0, success: 0}}
	if table == "jobs" {
		out.Counts["cancelled"] = 0
	}
	// Identifiers and state names are internal constants, never request input.
	query := `SELECT status,COUNT(*),MIN(CASE WHEN status='queued' AND available_at<=? THEN available_at END),MAX(updated_at),SUM(CASE WHEN updated_at>=? THEN 1 ELSE 0 END) FROM ` + table + filter + ` GROUP BY status`
	params := append([]any{now.UnixMilli(), now.Add(-24 * time.Hour).UnixMilli()}, args...)
	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count, recent int64
		var due, last sql.NullInt64
		if err = rows.Scan(&status, &count, &due, &last, &recent); err != nil {
			return out, err
		}
		out.Counts[status] = count
		if due.Valid {
			at := instant(due.Int64)
			out.OldestDueAt = &at
		}
		if last.Valid {
			at := instant(last.Int64)
			switch status {
			case success:
				out.LastSucceededAt, out.RecentSucceeded = &at, recent
			case "failed":
				out.LastFailedAt, out.RecentFailed = &at, recent
			}
		}
	}
	return out, rows.Err()
}
