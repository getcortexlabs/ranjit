package provider

import (
	"context"
	"time"
)

// Checkpoint retains successful read responses for one leased job. OAuth
// responses, headers and credentials never enter this boundary.
type Checkpoint interface {
	Load(context.Context, string) ([]byte, error)
	Save(context.Context, string, []byte) error
}
type checkpointKey struct{}
type syncTimeKey struct{}

func WithCheckpoint(ctx context.Context, c Checkpoint, at time.Time) context.Context {
	return context.WithValue(context.WithValue(ctx, checkpointKey{}, c), syncTimeKey{}, at)
}

// SyncTime freezes the requested period across retries, including at midnight.
// Authentication and consent checks continue to use the actual clock.
func SyncTime(ctx context.Context, fallback time.Time) time.Time {
	if at, ok := ctx.Value(syncTimeKey{}).(time.Time); ok {
		return at
	}
	return fallback
}
