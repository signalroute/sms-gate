// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// worker_shutdown_test.go: tests for graceful shutdown behavior.
// Covers issues #175 (in-flight tasks dropped on SIGTERM) and #176
// (buffer not flushed on SIGTERM).
package modem

import (
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/signalroute/sms-gate/internal/tunnel"
)

func testSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makeTask(msgID string) InboundTask {
	return InboundTask{
		Task: tunnel.Task{
			Envelope: tunnel.Envelope{
				Type:      tunnel.TypeTask,
				MessageID: msgID,
			},
			Action: tunnel.ActionSendSMS,
		},
	}
}

// ── drainInboundCh ────────────────────────────────────────────────────────

// TestDrainInboundCh_NACKsAllPendingTasks verifies that drainInboundCh sends
// a NACK (StatusFailed) for every task buffered in inboundCh (issue #175).
func TestDrainInboundCh_NACKsAllPendingTasks(t *testing.T) {
	const n = 5
	w := &Worker{
		inboundCh: make(chan InboundTask, n),
	}

	var mu sync.Mutex
	acks := make([]tunnel.TaskAckEvent, 0, n)

	for i := 0; i < n; i++ {
		msgID := string(rune('a' + i))
		it := makeTask(msgID)
		it.AckFn = func(ack tunnel.TaskAckEvent) {
			mu.Lock()
			acks = append(acks, ack)
			mu.Unlock()
		}
		w.inboundCh <- it
	}

	w.drainInboundCh(testSlogLogger())

	mu.Lock()
	defer mu.Unlock()

	if len(acks) != n {
		t.Fatalf("expected %d NACKs, got %d", n, len(acks))
	}
	for i, ack := range acks {
		if ack.Status != tunnel.StatusFailed {
			t.Errorf("ack[%d]: expected StatusFailed, got %q", i, ack.Status)
		}
		if ack.Error == nil {
			t.Errorf("ack[%d]: expected non-nil Error", i)
		}
		if ack.Error != nil && ack.Error.Code != tunnel.ErrCodeModemUnresponsive {
			t.Errorf("ack[%d]: expected ErrCodeModemUnresponsive, got %q", i, ack.Error.Code)
		}
	}
}

// TestDrainInboundCh_EmptyChannelIsNoop verifies that calling drainInboundCh
// on an already-empty channel is a no-op (no hang, no panic).
func TestDrainInboundCh_EmptyChannelIsNoop(t *testing.T) {
	w := &Worker{
		inboundCh: make(chan InboundTask, 64),
	}
	w.drainInboundCh(testSlogLogger()) // must return immediately
}

// TestDrainInboundCh_PreservesMessageIDs verifies that each NACK ACK carries
// the original message_id for cloud-side correlation.
func TestDrainInboundCh_PreservesMessageIDs(t *testing.T) {
	ids := []string{"msg-001", "msg-002", "msg-003"}
	w := &Worker{
		inboundCh: make(chan InboundTask, len(ids)),
	}

	var mu sync.Mutex
	gotIDs := make([]string, 0, len(ids))

	for _, id := range ids {
		id := id
		it := makeTask(id)
		it.AckFn = func(ack tunnel.TaskAckEvent) {
			mu.Lock()
			gotIDs = append(gotIDs, ack.MessageID)
			mu.Unlock()
		}
		w.inboundCh <- it
	}

	w.drainInboundCh(testSlogLogger())

	mu.Lock()
	defer mu.Unlock()

	if len(gotIDs) != len(ids) {
		t.Fatalf("expected %d message IDs, got %d", len(ids), len(gotIDs))
	}
	// Order is preserved (FIFO channel drain).
	for i, want := range ids {
		if gotIDs[i] != want {
			t.Errorf("ack[%d]: got message_id %q, want %q", i, gotIDs[i], want)
		}
	}
}

// TestDrainInboundCh_SingleTask drains a channel with exactly one task.
func TestDrainInboundCh_SingleTask(t *testing.T) {
	w := &Worker{
		inboundCh: make(chan InboundTask, 1),
	}
	called := false
	it := makeTask("x")
	it.AckFn = func(ack tunnel.TaskAckEvent) { called = true }
	w.inboundCh <- it
	w.drainInboundCh(testSlogLogger())
	if !called {
		t.Error("expected AckFn to be called for single task")
	}
}

// TestDrainInboundCh_AckEnvelopeType verifies the NACK uses TypeTaskAck.
func TestDrainInboundCh_AckEnvelopeType(t *testing.T) {
	w := &Worker{
		inboundCh: make(chan InboundTask, 1),
	}
	var got tunnel.TaskAckEvent
	it := makeTask("test")
	it.AckFn = func(ack tunnel.TaskAckEvent) { got = ack }
	w.inboundCh <- it
	w.drainInboundCh(testSlogLogger())
	if got.Type != tunnel.TypeTaskAck {
		t.Errorf("expected envelope type %q, got %q", tunnel.TypeTaskAck, got.Type)
	}
}

// TestDrainInboundCh_NilAckFnIsSkipped verifies that tasks with a nil AckFn
// do not panic when drained (defensive — cloud may send tasks without ack callback).
func TestDrainInboundCh_NilAckFnIsSkipped(t *testing.T) {
	w := &Worker{
		inboundCh: make(chan InboundTask, 1),
	}
	// AckFn is nil
	it := makeTask("no-ack")
	it.AckFn = nil
	w.inboundCh <- it
	// Must not panic even with nil AckFn.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("drainInboundCh panicked with nil AckFn: %v", r)
		}
	}()
	w.drainInboundCh(testSlogLogger())
}
