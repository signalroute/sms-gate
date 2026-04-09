// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package modem

import "testing"

// ── State.String ──────────────────────────────────────────────────────────

func TestStateString_AllValues(t *testing.T) {
	t.Parallel()
	want := []string{
		"INITIALIZING", // 0
		"ACTIVE",       // 1
		"EXECUTING",    // 2
		"RECOVERING",   // 3
		"RESETTING",    // 4
		"BANNED",       // 5
		"FAILED",       // 6
	}
	for i, name := range want {
		s := State(i)
		got := s.String()
		if got == "" || got == "UNKNOWN" {
			t.Errorf("State(%d).String() = %q, want non-empty non-UNKNOWN", i, got)
		}
		if got != name {
			t.Errorf("State(%d).String() = %q, want %q", i, got, name)
		}
	}
}

func TestStateString_Unknown(t *testing.T) {
	t.Parallel()
	if got := State(99).String(); got != "UNKNOWN" {
		t.Errorf("State(99).String() = %q, want UNKNOWN", got)
	}
}

// ── NewWorkerForTest ───────────────────────────────────────────────────────

func TestNewWorkerForTest_ICCID(t *testing.T) {
	t.Parallel()
	w := NewWorkerForTest("ICCID42")
	if w.ICCID() != "ICCID42" {
		t.Errorf("ICCID() = %q, want ICCID42", w.ICCID())
	}
}

func TestNewWorkerForTest_TaskChNotNil(t *testing.T) {
	t.Parallel()
	w := NewWorkerForTest("TEST_ICCID")
	if w.TaskCh() == nil {
		t.Error("TaskCh() returned nil, want non-nil channel")
	}
}

// ── Registry.Snapshot with NewWorkerForTest ────────────────────────────────

func TestRegistry_Snapshot_WithWorker(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	w := NewWorkerForTest("ICCID1")
	reg.Register("ICCID1", w)

	snap := reg.Snapshot()
	if _, ok := snap["ICCID1"]; !ok {
		t.Error("Snapshot() missing ICCID1")
	}
	if len(snap) != 1 {
		t.Errorf("Snapshot() len = %d, want 1", len(snap))
	}
}
