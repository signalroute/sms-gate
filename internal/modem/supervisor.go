// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"context"
	"log/slog"
	"time"

	"github.com/signalroute/sms-gate/internal/backoff"
	"github.com/signalroute/sms-gate/internal/metrics"
)

// WorkerFactory creates a fresh Worker for a given port. A new Worker must be
// created for each attempt because Run() mutates internal state.
type WorkerFactory func() *Worker

// RunSupervised starts the Worker returned by factory and restarts it on
// failure using exponential back-off. It stops when ctx is canceled.
//
// Each reconnect attempt is logged with the attempt number and the computed
// delay. The smsgate_modem_reconnect_total counter is incremented on every
// attempt (not on the first start).
//
// The port label used for the counter is taken from the first worker created
// by factory — the factory must always produce workers for the same port.
func RunSupervised(ctx context.Context, factory WorkerFactory, reg *Registry, met *metrics.Gateway, log *slog.Logger) {
	// Determine the port label from the first worker (before starting).
	probe := factory()
	port := probe.port

	log = log.With("port", port, "component", "supervisor")

	attempt := 0
	for {
		attempt++
		var w *Worker
		if attempt == 1 {
			w = probe // reuse the probe worker for the first attempt
		} else {
			w = factory()
		}

		log.Info("starting modem worker", "attempt", attempt)
		finalState := w.Run(ctx, reg)

		// If context is done, exit cleanly regardless of final state.
		if ctx.Err() != nil {
			log.Info("context canceled, supervisor exiting", "final_state", finalState)
			return
		}

		// Worker exited for a non-context reason — decide whether to reconnect.
		if finalState == StateBanned {
			log.Error("worker entered BANNED state, not reconnecting")
			return
		}

		// Compute back-off delay for the next attempt.
		delay := backoff.Compute(attempt)
		log.Warn("worker exited, reconnecting with back-off",
			"final_state", finalState,
			"attempt", attempt+1,
			"delay", delay.Round(time.Millisecond),
		)

		if met != nil {
			met.ModemReconnectTotal.WithLabelValues(port).Inc()
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}
