// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package watchdog

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchdog_AllProbesPass(t *testing.T) {
	var pings atomic.Int32
	w := New(Config{
		Interval: 10 * time.Millisecond,
		Probes: []Probe{
			func(_ context.Context) error { return nil },
		},
	})
	w.notifyFn = func() error { pings.Add(1); return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if n := pings.Load(); n < 2 {
		t.Errorf("expected ≥2 pings, got %d", n)
	}
}

func TestWatchdog_ProbeFailureSkipsNotify(t *testing.T) {
	var pings atomic.Int32
	w := New(Config{
		Interval: 10 * time.Millisecond,
		Probes: []Probe{
			func(_ context.Context) error { return errors.New("stalled") },
		},
	})
	w.notifyFn = func() error { pings.Add(1); return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if n := pings.Load(); n != 0 {
		t.Errorf("expected 0 pings (all probes fail), got %d", n)
	}
}

func TestWatchdog_MixedProbes(t *testing.T) {
	calls := atomic.Int32{}
	healthy := func(_ context.Context) error { calls.Add(1); return nil }
	sick := func(_ context.Context) error { calls.Add(1); return errors.New("bad") }

	var pings atomic.Int32
	w := New(Config{
		Interval: 10 * time.Millisecond,
		Probes:   []Probe{healthy, sick},
	})
	w.notifyFn = func() error { pings.Add(1); return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if n := pings.Load(); n != 0 {
		t.Errorf("expected 0 pings (second probe fails), got %d", n)
	}
	if c := calls.Load(); c < 2 {
		t.Error("expected probes to be called")
	}
}

func TestWatchdog_DefaultInterval(t *testing.T) {
	w := New(Config{})
	if w.interval != 5*time.Second {
		t.Errorf("expected default 5s, got %v", w.interval)
	}
}

func TestWatchdog_NotifyError(t *testing.T) {
	// Verify notify errors don't crash the loop.
	w := New(Config{Interval: 10 * time.Millisecond})
	w.notifyFn = func() error { return errors.New("socket gone") }

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	w.Run(ctx) // Should not panic.
}

func TestSdNotify_NoSocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := sdNotify(); err != nil {
		t.Errorf("expected nil when NOTIFY_SOCKET unset, got %v", err)
	}
}
