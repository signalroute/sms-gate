// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/signalroute/sms-gate/internal/metrics"
	"github.com/signalroute/sms-gate/internal/modem"
)

// newTestMetrics creates an isolated Prometheus registry with all metrics.
func newTestMetrics(t *testing.T) *metrics.Gateway {
	t.Helper()
	return metrics.New(prometheus.NewRegistry())
}

// makeFactory returns a WorkerFactory that creates Workers pointing at addr
// (which should have nothing listening so Run() fails immediately).
func makeFactory(addr string, met *metrics.Gateway) modem.WorkerFactory {
	log := slog.Default()
	return func() *modem.Worker {
		return modem.NewWorker(modem.WorkerConfig{
			Port:    addr,
			Logger:  log,
			Metrics: met,
		})
	}
}

// TestRunSupervisedExitsOnContextCancel verifies that RunSupervised returns
// promptly when the context is cancelled.
func TestRunSupervisedExitsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	reg := modem.NewRegistry()
	met := newTestMetrics(t)
	log := slog.Default()

	done := make(chan struct{})
	go func() {
		defer close(done)
		modem.RunSupervised(ctx, makeFactory("127.0.0.1:19997", met), reg, met, log)
	}()

	time.Sleep(60 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// good — supervisor exited after cancel
	case <-time.After(5 * time.Second):
		t.Fatal("RunSupervised did not exit after context cancel")
	}
}

// TestRunSupervisedIncrementsCounter verifies that ModemReconnectTotal is
// incremented each time the supervisor retries after a failure.
func TestRunSupervisedIncrementsCounter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := modem.NewRegistry()
	met := newTestMetrics(t)
	log := slog.Default()

	calls := make(chan struct{}, 20)
	factory := func() *modem.Worker {
		calls <- struct{}{}
		return modem.NewWorker(modem.WorkerConfig{
			Port:    "127.0.0.1:19996", // nothing listening — fails immediately
			Logger:  log,
			Metrics: met,
		})
	}

	go modem.RunSupervised(ctx, factory, reg, met, log)

	// Wait for at least 2 factory invocations (initial + first reconnect).
	deadline := time.After(6 * time.Second)
	seen := 0
	for seen < 2 {
		select {
		case <-calls:
			seen++
		case <-deadline:
			t.Fatalf("only %d factory calls before deadline", seen)
		}
	}
	cancel()

	// Counter must be ≥ 1 (first reconnect attempt increments it).
	got := testutil.ToFloat64(met.ModemReconnectTotal.WithLabelValues("127.0.0.1:19996"))
	if got < 1 {
		t.Errorf("modem_reconnect_total = %.0f, want ≥ 1", got)
	}
}

