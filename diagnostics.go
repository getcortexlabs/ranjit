package ranjit

import (
	"context"
	"net/http"
	"time"
)

type CertificateStatus struct {
	Profile       string     `json:"profile"`
	Level         string     `json:"level"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	DaysRemaining int        `json:"days_remaining"`
}

type Diagnostics struct {
	Version             string              `json:"version"`
	Certificates        []CertificateStatus `json:"certificates"`
	UnavailableProfiles []string            `json:"unavailable_profiles"`
	Operations          *OperationsStatus   `json:"operations,omitempty"`
}

type OperationsStatus struct {
	ObservedAt   time.Time               `json:"observed_at"`
	Database     string                  `json:"database"`
	Workers      map[string]WorkerStatus `json:"workers"`
	SyncQueue    QueueStatus             `json:"sync_queue"`
	WebhookQueue QueueStatus             `json:"webhook_queue"`
}

type WorkerStatus struct {
	State          string     `json:"state"`
	Healthy        bool       `json:"healthy"`
	LastStartedAt  *time.Time `json:"last_started_at,omitempty"`
	LastFinishedAt *time.Time `json:"last_finished_at,omitempty"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	Steps          uint64     `json:"steps"`
	Errors         uint64     `json:"errors"`
}

// QueueStatus is a database snapshot. Recent outcomes cover the last 24 hours;
// worker step counters are process-local and reset on restart.
type QueueStatus struct {
	Counts          map[string]int64 `json:"counts"`
	OldestDueAt     *time.Time       `json:"oldest_due_at,omitempty"`
	LastSucceededAt *time.Time       `json:"last_succeeded_at,omitempty"`
	LastFailedAt    *time.Time       `json:"last_failed_at,omitempty"`
	RecentSucceeded int64            `json:"recent_succeeded"`
	RecentFailed    int64            `json:"recent_failed"`
}

func (c *Client) Diagnostics(ctx context.Context) (Diagnostics, error) {
	var out Diagnostics
	err := c.do(ctx, http.MethodGet, "/v1/diagnostics", nil, &out)
	return out, err
}

func (c *Client) Ready(ctx context.Context) error  { return c.healthStatus(ctx, "/readyz", "ready") }
func (c *Client) Health(ctx context.Context) error { return c.healthStatus(ctx, "/healthz", "ok") }
func (c *Client) healthStatus(ctx context.Context, path, want string) error {
	var result struct {
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return err
	}
	if result.Status != want {
		return ErrInvalidResponse
	}
	return nil
}
