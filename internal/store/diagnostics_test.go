package store

import (
	"testing"
	"time"
)

func TestQueueDiagnosticsAcrossBackends(t *testing.T) {
	backendStores(t, func(t *testing.T, s *Store, _ string) {
		c := directGrant()
		if err := s.CreateAuthorizedConnection(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		s.EnableWebhooks("https://backend.example/events")
		sync, hooks, err := s.Queues(t.Context(), testNow.Add(time.Minute))
		if err != nil || sync.Counts["queued"] != 1 || sync.OldestDueAt == nil || !sync.OldestDueAt.Equal(testNow) || hooks.Counts["queued"] != 0 {
			t.Fatal(sync, hooks, err)
		}
		j, err := s.Claim(t.Context(), testNow, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Fail(t.Context(), j, "provider_credentials", testNow); err != nil {
			t.Fatal(err)
		}
		attempt, err := s.ClaimWebhook(t.Context(), testNow)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.FinishWebhook(t.Context(), attempt, 204, testNow); err != nil {
			t.Fatal(err)
		}
		sync, hooks, err = s.Queues(t.Context(), testNow)
		if err != nil || sync.Counts["failed"] != 1 || sync.RecentFailed != 1 || sync.LastFailedAt == nil || hooks.Counts["delivered"] != 1 || hooks.RecentSucceeded != 1 {
			t.Fatal(sync, hooks, err)
		}
		sync, hooks, err = s.Queues(t.Context(), testNow.Add(25*time.Hour))
		if err != nil || sync.RecentFailed != 0 || hooks.RecentSucceeded != 0 || sync.Counts["failed"] != 1 {
			t.Fatal(sync, hooks, err)
		}
		s.EnableWebhooks("https://another.example/events")
		_, hooks, err = s.Queues(t.Context(), testNow)
		if err != nil || hooks.Counts["delivered"] != 0 {
			t.Fatal("other target metadata exposed", hooks, err)
		}
		s.Close()
		if _, _, err = s.Queues(t.Context(), testNow); err == nil {
			t.Fatal("closed database reported healthy")
		}
	})
}
