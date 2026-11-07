package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
	"github.com/getcortexlabs/ranjit/internal/store"
)

type Worker struct {
	ConnectionIDs map[string]bool
	Store         *store.Store
	Source        provider.Source
	Interval      time.Duration
	Logger        *slog.Logger
	Now           func() time.Time
}

func New(db *store.Store, source provider.Source, interval time.Duration, logger *slog.Logger) *Worker {
	return &Worker{Store: db, Source: source, Interval: interval, Logger: logger, Now: time.Now}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := w.Step(ctx); err != nil && ctx.Err() == nil {
			w.Logger.Error("worker step failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Step processes at most one job. A lease fences results from a worker that has lost ownership.
func (w *Worker) Step(ctx context.Context) error {
	now := w.Now()
	if err := w.Store.Expire(ctx, now); err != nil {
		return err
	}
	selected := w.ConnectionIDs
	if selected == nil {
		institutions := []string{}
		for _, i := range w.Source.Institutions() {
			institutions = append(institutions, i.ID)
		}
		var err error
		selected, err = w.Store.ConfiguredConnections(ctx, institutions)
		if err != nil {
			return err
		}
		if err = w.Store.ScheduleSelected(ctx, now, w.Interval, selected); err != nil {
			return err
		}
	}
	j, err := w.Store.ClaimSelected(ctx, now, time.Minute, selected)
	if errors.Is(err, ranjit.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	c, err := w.Store.Connection(ctx, j.ConnectionID, w.Now())
	if err != nil {
		return err
	}
	if c.Status != "active" {
		return w.Store.Expire(ctx, w.Now())
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	fetchCtx = provider.WithCheckpoint(fetchCtx, store.Checkpoint{Store: w.Store, Job: j, Now: w.Now}, j.CreatedAt)
	batch, fetchErr := w.Source.Fetch(fetchCtx, c)
	if fetchErr == nil {
		fetchErr = fetchCtx.Err()
	}
	cancel()
	if errors.Is(fetchErr, context.DeadlineExceeded) && ctx.Err() == nil {
		current, err := w.Store.Job(ctx, j.ID)
		if err == nil && current.RequestsCompleted > j.RequestsCompleted {
			return w.Store.Yield(ctx, j, w.Now())
		}
	}
	code := "provider_unavailable"
	if fetchErr == nil {
		fetchErr = w.Store.Complete(ctx, j, batch, w.Now())
		code = "storage_unavailable"
	}
	if fetchErr == nil {
		w.Logger.Info("sync completed", "job_id", j.ID)
		return nil
	}
	if errors.Is(fetchErr, ranjit.ErrConflict) || errors.Is(fetchErr, ranjit.ErrLease) {
		return nil
	}
	if errors.Is(fetchErr, ranjit.ErrInvalid) {
		code = "invalid_provider_data"
	}
	if errors.Is(fetchErr, ranjit.ErrProviderCredentials) {
		code = "provider_credentials"
	}
	// Provider responses can contain account data or credentials. Persist only a bounded error code.
	w.Logger.Warn("sync failed", "job_id", j.ID, "code", code, "attempt", j.Attempts)
	var retry *provider.RetryError
	if errors.As(fetchErr, &retry) {
		return w.Store.FailAfter(ctx, j, code, w.Now(), retry.After)
	}
	return w.Store.Fail(ctx, j, code, w.Now())
}
