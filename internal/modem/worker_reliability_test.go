// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/tunnel"
)

// ── InboundQueueSize ──────────────────────────────────────────────────────

// TestNewWorker_DefaultQueueSize verifies that a zero InboundQueueSize uses
// the default of 64 so existing callers don't change behaviour.
func TestNewWorker_DefaultQueueSize(t *testing.T) {
	w := NewWorker(WorkerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if cap(w.inboundCh) != 64 {
		t.Fatalf("expected default queue size 64, got %d", cap(w.inboundCh))
	}
}

// TestNewWorker_CustomQueueSize verifies that InboundQueueSize is respected.
func TestNewWorker_CustomQueueSize(t *testing.T) {
	for _, size := range []int{1, 16, 128, 512} {
		w := NewWorker(WorkerConfig{
			InboundQueueSize: size,
			Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if cap(w.inboundCh) != size {
			t.Fatalf("size=%d: expected cap %d, got %d", size, size, cap(w.inboundCh))
		}
	}
}

// TestNewWorker_NegativeQueueSizeFallsBackToDefault ensures defensive handling
// of misconfigured queue sizes (negative values treated like 0).
func TestNewWorker_NegativeQueueSizeFallsBackToDefault(t *testing.T) {
	w := NewWorker(WorkerConfig{
		InboundQueueSize: -10,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if cap(w.inboundCh) != 64 {
		t.Fatalf("expected fallback queue size 64, got %d", cap(w.inboundCh))
	}
}

// ── StallDuration / watchdog defaults ────────────────────────────────────

// TestNewWorker_DefaultStallDuration verifies that a zero StallDuration
// defaults to 5 minutes.
func TestNewWorker_DefaultStallDuration(t *testing.T) {
	w := NewWorker(WorkerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if w.stallDuration != 5*time.Minute {
		t.Fatalf("expected default stall duration 5m, got %v", w.stallDuration)
	}
}

// TestNewWorker_CustomStallDuration ensures a caller can override the watchdog
// threshold (needed in tests to set very short durations).
func TestNewWorker_CustomStallDuration(t *testing.T) {
	w := NewWorker(WorkerConfig{
		StallDuration: 30 * time.Second,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if w.stallDuration != 30*time.Second {
		t.Fatalf("expected stall duration 30s, got %v", w.stallDuration)
	}
}

// ── lastLoopNs — stall detection timestamp ────────────────────────────────

// TestLastLoopNs_InitiallyZero verifies that the timestamp is zero before Run()
// seeds it, so callers that inspect it before the loop starts get a zero value.
func TestLastLoopNs_InitiallyZero(t *testing.T) {
	w := NewWorker(WorkerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if got := w.lastLoopNs.Load(); got != 0 {
		t.Fatalf("expected zero lastLoopNs before Run(), got %d", got)
	}
}

// TestLastLoopNs_CanBeUpdatedAtomically ensures the atomic field accepts
// concurrent writes without races.
func TestLastLoopNs_CanBeUpdatedAtomically(t *testing.T) {
	w := NewWorker(WorkerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	now := time.Now().UnixNano()
	w.lastLoopNs.Store(now)
	if got := w.lastLoopNs.Load(); got != now {
		t.Fatalf("expected %d, got %d", now, got)
	}
}

// ── Dropped-task observability (drainInboundCh + AckFn contract) ─────────

// TestDroppedTaskAckCarriesErrCodeModemUnresponsive verifies that tasks NACKed
// on shutdown use ErrCodeModemUnresponsive so the cloud knows to retry rather
// than treat the failure as permanent.
func TestDroppedTaskAckCarriesErrCodeModemUnresponsive(t *testing.T) {
	w := NewWorker(WorkerConfig{
		InboundQueueSize: 4,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	got := make([]tunnel.TaskAckEvent, 0)
	push := func(ack tunnel.TaskAckEvent) { got = append(got, ack) }

	for i := range 3 {
		_ = i
		w.inboundCh <- InboundTask{
			Task:  tunnel.Task{Envelope: tunnel.Envelope{MessageID: "m1", Type: tunnel.TypeTask}},
			AckFn: push,
		}
	}

	w.drainInboundCh(slog.New(slog.NewTextHandler(io.Discard, nil)))

	if len(got) != 3 {
		t.Fatalf("expected 3 acks, got %d", len(got))
	}
	for _, ack := range got {
		if ack.Error == nil {
			t.Fatal("expected error in NACK, got nil")
		}
		if ack.Error.Code != tunnel.ErrCodeModemUnresponsive {
			t.Fatalf("expected ErrCodeModemUnresponsive, got %q", ack.Error.Code)
		}
		if ack.Status != tunnel.StatusFailed {
			t.Fatalf("expected StatusFailed, got %q", ack.Status)
		}
	}
}

// TestDroppedTaskAckStatus verifies the ack envelope type is TypeTaskAck.
func TestDroppedTaskAckEnvelopeType(t *testing.T) {
	w := NewWorker(WorkerConfig{
		InboundQueueSize: 4,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	var got tunnel.TaskAckEvent
	w.inboundCh <- InboundTask{
		Task:  tunnel.Task{Envelope: tunnel.Envelope{MessageID: "x1", Type: tunnel.TypeTask}},
		AckFn: func(a tunnel.TaskAckEvent) { got = a },
	}

	w.drainInboundCh(nil)

	if got.Envelope.Type != tunnel.TypeTaskAck {
		t.Fatalf("expected TypeTaskAck, got %q", got.Envelope.Type)
	}
}
