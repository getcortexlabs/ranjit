package monitor

import (
	"errors"
	"testing"
	"time"
)

func TestWorkerProgressAndFailure(t *testing.T) {
	now := time.Now()
	m := NewWorker(true, 30*time.Second)
	if m.Snapshot(now).State != "starting" {
		t.Fatal("unknown startup")
	}
	m.Begin(now)
	if got := m.Snapshot(now.Add(20 * time.Second)); got.State != "running" || !got.Healthy {
		t.Fatal(got)
	}
	if got := m.Snapshot(now.Add(31 * time.Second)); got.State != "stalled" || got.Healthy {
		t.Fatal(got)
	}
	m.End(now.Add(32*time.Second), errors.New("storage failed"))
	if got := m.Snapshot(now.Add(32 * time.Second)); got.State != "error" || got.Errors != 1 {
		t.Fatal(got)
	}
	m.Begin(now.Add(33 * time.Second))
	m.End(now.Add(34*time.Second), nil)
	if got := m.Snapshot(now.Add(35 * time.Second)); got.State != "idle" || !got.Healthy || got.Steps != 2 {
		t.Fatal(got)
	}
	if got := m.Snapshot(now.Add(40 * time.Second)); got.State != "stalled" {
		t.Fatal(got)
	}
	m.Stop()
	if m.Snapshot(now).State != "stopped" {
		t.Fatal("stopped worker healthy")
	}
	if got := NewWorker(false, time.Second).Snapshot(now); !got.Healthy || got.State != "disabled" {
		t.Fatal(got)
	}
}
