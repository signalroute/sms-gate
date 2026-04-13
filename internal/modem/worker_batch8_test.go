// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
)

// ── TestWorkerStatusJSON (#129) ───────────────────────────────────────────

func TestWorkerStatus_JSON(t *testing.T) {
	ws := WorkerStatus{
		Port:         "/dev/ttyUSB0",
		State:        "ACTIVE",
		ICCID:        "89490200001234567890",
		LastActivity: 1700000000000,
	}
	data, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded WorkerStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.Port != ws.Port {
		t.Errorf("port: got %q, want %q", decoded.Port, ws.Port)
	}
	if decoded.State != ws.State {
		t.Errorf("state: got %q, want %q", decoded.State, ws.State)
	}
	if decoded.ICCID != ws.ICCID {
		t.Errorf("ICCID: got %q, want %q", decoded.ICCID, ws.ICCID)
	}
}

func TestWorkerStatus_JSONAllStates(t *testing.T) {
	states := []State{
		StateInitializing, StateActive, StateExecuting,
		StateRecovering, StateResetting, StateBanned, StateFailed,
	}
	for _, s := range states {
		t.Run(s.String(), func(t *testing.T) {
			ws := WorkerStatus{State: s.String()}
			data, err := json.Marshal(ws)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded WorkerStatus
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if decoded.State != s.String() {
				t.Errorf("state: got %q, want %q", decoded.State, s.String())
			}
		})
	}
}

// ── TestConcurrentSnapshot (#88) ──────────────────────────────────────────

func TestRegistry_ConcurrentSnapshot(t *testing.T) {
	reg := NewRegistry()

	for i := 0; i < 5; i++ {
		iccid := "8949020000123456789" + string(rune('0'+i))
		w := &Worker{port: "/dev/ttyUSB" + string(rune('0'+i))}
		w.state.Store(int32(StateActive))
		reg.Register(iccid, w)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.Snapshot()
		}()
	}
	wg.Wait()
}

// ── TestStateTransitions (#62) ────────────────────────────────────────────

func TestState_AllDefined(t *testing.T) {
	expected := map[State]string{
		StateInitializing: "INITIALIZING",
		StateActive:       "ACTIVE",
		StateExecuting:    "EXECUTING",
		StateRecovering:   "RECOVERING",
		StateResetting:    "RESETTING",
		StateBanned:       "BANNED",
		StateFailed:       "FAILED",
	}
	for s, name := range expected {
		if s.String() != name {
			t.Errorf("state %d: got %q, want %q", s, s.String(), name)
		}
	}
	if len(expected) != 7 {
		t.Errorf("expected 7 states, got %d", len(expected))
	}
}

// ── TestBanLifecycle (#81) ────────────────────────────────────────────────

func TestBanLifecycle(t *testing.T) {
	reg := NewRegistry()

	w := &Worker{port: "/dev/ttyUSB0"}
	w.state.Store(int32(StateActive))
	reg.Register("89490200001234567890", w)

	w.state.Store(int32(StateBanned))

	snap := reg.Snapshot()
	ws := snap["89490200001234567890"]
	if ws.State != "BANNED" {
		t.Errorf("state: got %q, want BANNED", ws.State)
	}

	total, active, banned := reg.PoolCounts()
	if total != 1 || active != 0 || banned != 1 {
		t.Errorf("pool counts: total=%d active=%d banned=%d", total, active, banned)
	}
}

// ── TestRecoveryAfterNFailures (#71) ──────────────────────────────────────

func TestRecoveryAfterNFailures(t *testing.T) {
	reg := NewRegistry()

	w := &Worker{port: "/dev/ttyUSB0"}
	w.state.Store(int32(StateActive))
	reg.Register("89490200001234567890", w)

	w.state.Store(int32(StateRecovering))
	snap := reg.Snapshot()
	if snap["89490200001234567890"].State != "RECOVERING" {
		t.Errorf("expected RECOVERING state")
	}

	w.state.Store(int32(StateBanned))
	snap = reg.Snapshot()
	if snap["89490200001234567890"].State != "BANNED" {
		t.Errorf("expected BANNED state")
	}
}

// ── TestAllBannedPoolCounts (#40) ─────────────────────────────────────────

func TestAllBannedPoolCounts(t *testing.T) {
	reg := NewRegistry()

	for i := 0; i < 3; i++ {
		iccid := "8949020000123456789" + string(rune('0'+i))
		w := &Worker{port: "/dev/ttyUSB" + string(rune('0'+i))}
		w.state.Store(int32(StateBanned))
		reg.Register(iccid, w)
	}

	total, active, banned := reg.PoolCounts()
	if total != 3 || active != 0 || banned != 3 {
		t.Errorf("pool counts: total=%d active=%d banned=%d (want 3,0,3)", total, active, banned)
	}
}

// ── TestPoolCounts_MixedStates ────────────────────────────────────────────

func TestPoolCounts_MixedStates(t *testing.T) {
	reg := NewRegistry()

	states := []State{StateActive, StateBanned, StateRecovering, StateActive, StateInitializing}
	for i, s := range states {
		iccid := "8949020000123456789" + string(rune('0'+i))
		w := &Worker{port: "/dev/ttyUSB" + string(rune('0'+i))}
		w.state.Store(int32(s))
		reg.Register(iccid, w)
	}

	total, active, banned := reg.PoolCounts()
	if total != 5 {
		t.Errorf("total: got %d, want 5", total)
	}
	if active != 2 {
		t.Errorf("active: got %d, want 2", active)
	}
	if banned != 1 {
		t.Errorf("banned: got %d, want 1", banned)
	}
}

// ── TestDispatch_UnknownICCID ─────────────────────────────────────────────

func TestDispatch_UnknownICCID(t *testing.T) {
	reg := NewRegistry()
	err := reg.Dispatch("00000000000000000000", InboundTask{})
	if err != ErrModemNotFound {
		t.Errorf("expected ErrModemNotFound, got %v", err)
	}
}

// ── TestWorkerState_Atomic ────────────────────────────────────────────────

func TestWorkerState_Atomic(t *testing.T) {
	var state atomic.Int32
	state.Store(int32(StateActive))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = State(state.Load()).String()
		}()
	}
	wg.Wait()
}
