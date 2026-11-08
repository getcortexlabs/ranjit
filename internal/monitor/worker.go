package monitor

import (
	"sync"
	"time"

	"github.com/getcortexlabs/ranjit"
)

// Worker records completed work, not a separate heartbeat that could keep
// reporting healthy while the real worker is stuck.
type Worker struct {
	mu                         sync.Mutex
	enabled                    bool
	running                    bool
	stopped                    bool
	failed                     bool
	budget                     time.Duration
	started, finished, success time.Time
	steps, errors              uint64
}

func NewWorker(enabled bool, budget time.Duration) *Worker {
	return &Worker{enabled: enabled, budget: budget}
}

func (m *Worker) Begin(now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running, m.started = true, now.UTC()
}
func (m *Worker) End(now time.Time, err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running, m.finished, m.failed = false, now.UTC(), err != nil
	m.steps++
	if err != nil {
		m.errors++
	} else {
		m.success = now.UTC()
	}
}
func (m *Worker) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
}

func (m *Worker) Snapshot(now time.Time) ranjit.WorkerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := ranjit.WorkerStatus{State: "idle", Healthy: true, Steps: m.steps, Errors: m.errors, LastStartedAt: stamp(m.started), LastFinishedAt: stamp(m.finished), LastSuccessAt: stamp(m.success)}
	switch {
	case !m.enabled:
		out.State = "disabled"
	case m.stopped:
		out.State, out.Healthy = "stopped", false
	case m.started.IsZero():
		out.State, out.Healthy = "starting", false
	case m.running && now.Sub(m.started) > m.budget || !m.running && now.Sub(m.finished) > 5*time.Second:
		out.State, out.Healthy = "stalled", false
	case m.failed:
		out.State, out.Healthy = "error", false
	case m.running:
		out.State = "running"
	}
	return out
}

func stamp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
