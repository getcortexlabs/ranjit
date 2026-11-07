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
	Store    *store.Store
	Source   provider.Source
	Interval time.Duration
	Logger   *slog.Logger
	Now      func() time.Time
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
	if err := w.Store.Schedule(ctx, now, w.Interval); err != nil {
		return err
	}
	j, err := w.Store.Claim(ctx, now, time.Minute)
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
	batch, fetchErr := w.Source.Fetch(fetchCtx, c)
	if fetchErr == nil {
		fetchErr = fetchCtx.Err()
	}
	cancel()
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
	// Provider responses can contain account data or credentials. Persist only a bounded error code.
	w.Logger.Warn("sync failed", "job_id", j.ID, "code", code, "attempt", j.Attempts)
	return w.Store.Fail(ctx, j, code, w.Now())
}
