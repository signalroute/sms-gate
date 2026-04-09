// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package modem

import (
	"errors"
	"testing"
)

// makeWorkerWithTask returns a Worker whose inboundCh is populated with one task,
// making it appear "busy" for the next Dispatch call.
func makeWorkerForRegistry(t *testing.T, busy bool) *Worker {
	t.Helper()
	w := &Worker{
		inboundCh: make(chan InboundTask, 1),
	}
	if busy {
		w.inboundCh <- InboundTask{} // fill the single-slot buffer
	}
	return w
}

// ── Basic registration ─────────────────────────────────────────────────────

func TestRegistry_RegisterAndLookup(t *testing.T) {
	reg := NewRegistry()
	w := makeWorkerForRegistry(t, false)

	reg.Register("ICCID1", w)

	got, ok := reg.Lookup("ICCID1")
	if !ok {
		t.Fatal("Lookup returned not-found after Register")
	}
	if got != w {
		t.Error("Lookup returned wrong worker")
	}
}

func TestRegistry_LookupMissing(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Lookup("NOTHERE")
	if ok {
		t.Error("Lookup should return false for unregistered ICCID")
	}
}

func TestRegistry_Deregister(t *testing.T) {
	reg := NewRegistry()
	w := makeWorkerForRegistry(t, false)

	reg.Register("ICCID1", w)
	reg.Deregister("ICCID1")

	_, ok := reg.Lookup("ICCID1")
	if ok {
		t.Error("Lookup should return false after Deregister")
	}
}

func TestRegistry_DeregisterMissing(t *testing.T) {
	// Deregistering a key that was never registered must not panic.
	reg := NewRegistry()
	reg.Deregister("GHOST")
}

// ── Dispatch ──────────────────────────────────────────────────────────────

func TestRegistry_Dispatch_Success(t *testing.T) {
	reg := NewRegistry()
	w := makeWorkerForRegistry(t, false)
	reg.Register("ICCID1", w)

	task := InboundTask{}
	err := reg.Dispatch("ICCID1", task)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Verify the task landed in the channel.
	select {
	case got := <-w.inboundCh:
		_ = got
	default:
		t.Error("task was not enqueued in worker's inboundCh")
	}
}

func TestRegistry_Dispatch_NotFound(t *testing.T) {
	reg := NewRegistry()
	err := reg.Dispatch("UNKNOWN", InboundTask{})
	if !errors.Is(err, ErrModemNotFound) {
		t.Errorf("expected ErrModemNotFound, got %v", err)
	}
}

func TestRegistry_Dispatch_Busy(t *testing.T) {
	reg := NewRegistry()
	w := makeWorkerForRegistry(t, true) // inboundCh already full
	reg.Register("ICCID1", w)

	err := reg.Dispatch("ICCID1", InboundTask{})
	if !errors.Is(err, ErrModemBusy) {
		t.Errorf("expected ErrModemBusy, got %v", err)
	}
}

// ── Snapshot ──────────────────────────────────────────────────────────────

func TestRegistry_Snapshot(t *testing.T) {
	reg := NewRegistry()

	w1 := makeWorkerForRegistry(t, false)
	w1.iccid = "ICCID1"
	w2 := makeWorkerForRegistry(t, false)
	w2.iccid = "ICCID2"

	reg.Register("ICCID1", w1)
	reg.Register("ICCID2", w2)

	snap := reg.Snapshot()
	if len(snap) != 2 {
		t.Errorf("Snapshot: got %d entries, want 2", len(snap))
	}
	if _, ok := snap["ICCID1"]; !ok {
		t.Error("Snapshot missing ICCID1")
	}
	if _, ok := snap["ICCID2"]; !ok {
		t.Error("Snapshot missing ICCID2")
	}
}

func TestRegistry_Snapshot_Empty(t *testing.T) {
	reg := NewRegistry()
	snap := reg.Snapshot()
	if len(snap) != 0 {
		t.Errorf("empty registry Snapshot should return empty map, got %v", snap)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────

func TestRegistry_ConcurrentRegisterLookup(t *testing.T) {
	reg := NewRegistry()
	done := make(chan struct{})

	// Writer goroutine.
	go func() {
		for i := 0; i < 1000; i++ {
			w := makeWorkerForRegistry(t, false)
			reg.Register("ICCID_RACE", w)
			reg.Deregister("ICCID_RACE")
		}
		close(done)
	}()

	// Reader goroutine.
	for {
		select {
		case <-done:
			return
		default:
			reg.Lookup("ICCID_RACE")
			reg.Snapshot()
		}
	}
}
