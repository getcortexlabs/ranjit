package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/getcortexlabs/ranjit"
)

func Operations(fn func(context.Context) ranjit.Diagnostics) Option {
	return func(h *Server, mux *http.ServeMux) {
		h.diagnostics = fn
		mux.HandleFunc("GET /v1/diagnostics", func(w http.ResponseWriter, r *http.Request) { write(w, 200, fn(r.Context())) })
	}
}

func (h *Server) metrics(w http.ResponseWriter, r *http.Request) {
	data := h.diagnostics(r.Context())
	if data.Operations == nil || data.Operations.Database != "ready" {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	op := data.Operations
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintln(w, "# HELP ranjit_worker_healthy Worker is progressing or deliberately disabled.\n# TYPE ranjit_worker_healthy gauge")
	for _, name := range []string{"sync", "webhook"} {
		worker := op.Workers[name]
		healthy := 0
		if worker.Healthy {
			healthy = 1
		}
		fmt.Fprintf(w, "ranjit_worker_healthy{worker=%q} %d\n", name, healthy)
	}
	fmt.Fprintln(w, "# HELP ranjit_worker_steps_total Completed worker steps since process start.\n# TYPE ranjit_worker_steps_total counter")
	for _, name := range []string{"sync", "webhook"} {
		fmt.Fprintf(w, "ranjit_worker_steps_total{worker=%q} %d\n", name, op.Workers[name].Steps)
	}
	fmt.Fprintln(w, "# HELP ranjit_worker_errors_total Failed worker steps since process start.\n# TYPE ranjit_worker_errors_total counter")
	for _, name := range []string{"sync", "webhook"} {
		fmt.Fprintf(w, "ranjit_worker_errors_total{worker=%q} %d\n", name, op.Workers[name].Errors)
	}
	fmt.Fprintln(w, "# HELP ranjit_queue_items Records currently in each queue state.\n# TYPE ranjit_queue_items gauge")
	queues := []struct {
		name  string
		stats ranjit.QueueStatus
	}{{"sync", op.SyncQueue}, {"webhook", op.WebhookQueue}}
	for _, q := range queues {
		for _, state := range []string{"queued", "running", "succeeded", "delivered", "failed", "cancelled"} {
			if count, ok := q.stats.Counts[state]; ok {
				fmt.Fprintf(w, "ranjit_queue_items{queue=%q,state=%q} %d\n", q.name, state, count)
			}
		}
	}
	fmt.Fprintln(w, "# HELP ranjit_queue_oldest_due_seconds Age of the oldest due queued record.\n# TYPE ranjit_queue_oldest_due_seconds gauge")
	for _, q := range queues {
		age := 0.0
		if q.stats.OldestDueAt != nil {
			age = max(0, op.ObservedAt.Sub(*q.stats.OldestDueAt).Seconds())
		}
		fmt.Fprintf(w, "ranjit_queue_oldest_due_seconds{queue=%q} %g\n", q.name, age)
	}
	fmt.Fprintln(w, "# HELP ranjit_queue_recent_outcomes Outcomes recorded in the last 24 hours.\n# TYPE ranjit_queue_recent_outcomes gauge")
	for _, q := range queues {
		fmt.Fprintf(w, "ranjit_queue_recent_outcomes{queue=%q,outcome=\"succeeded\"} %d\nranjit_queue_recent_outcomes{queue=%q,outcome=\"failed\"} %d\n", q.name, q.stats.RecentSucceeded, q.name, q.stats.RecentFailed)
	}
	fmt.Fprintln(w, "# HELP ranjit_queue_last_outcome_timestamp_seconds Unix time of the latest recorded outcome, or zero.\n# TYPE ranjit_queue_last_outcome_timestamp_seconds gauge")
	for _, q := range queues {
		fmt.Fprintf(w, "ranjit_queue_last_outcome_timestamp_seconds{queue=%q,outcome=\"succeeded\"} %d\nranjit_queue_last_outcome_timestamp_seconds{queue=%q,outcome=\"failed\"} %d\n", q.name, unixOrZero(q.stats.LastSucceededAt), q.name, unixOrZero(q.stats.LastFailedAt))
	}
}

func unixOrZero(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}
