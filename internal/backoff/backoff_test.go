// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package backoff_test

import (
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/backoff"
)

func TestComputeAttempt1StartsAtBase(t *testing.T) {
	const runs = 200
	for i := 0; i < runs; i++ {
		d := backoff.Compute(1)
		// With ±20 % jitter the result must be in [0.8 s, 1.2 s].
		if d < 800*time.Millisecond || d > 1200*time.Millisecond {
			t.Fatalf("attempt 1: got %v, want [800ms, 1200ms]", d)
		}
	}
}

func TestComputeDoublesEachAttempt(t *testing.T) {
	// Without jitter the raw delays must double each step.
	// Use ComputeWith with jitter=0 for deterministic checks.
	cases := []struct {
		attempt  int
		wantBase time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
	}
	for _, tc := range cases {
		d := backoff.ComputeWith(tc.attempt, backoff.DefaultBase, backoff.DefaultMax, 0)
		if d != tc.wantBase {
			t.Errorf("attempt %d: got %v, want %v", tc.attempt, d, tc.wantBase)
		}
	}
}

func TestComputeCapsAtMax(t *testing.T) {
	const runs = 200
	for i := 0; i < runs; i++ {
		d := backoff.Compute(100)
		// With ±20 % jitter the cap is 60 s; result must not exceed 60 s * 1.2.
		if d > time.Duration(float64(backoff.DefaultMax)*1.21) {
			t.Fatalf("attempt 100: got %v, exceeds capped max", d)
		}
	}
}

func TestComputeCapIsRespected(t *testing.T) {
	// No jitter — result must equal cap exactly once exponent would exceed it.
	for attempt := 7; attempt <= 20; attempt++ {
		d := backoff.ComputeWith(attempt, backoff.DefaultBase, backoff.DefaultMax, 0)
		if d != backoff.DefaultMax {
			t.Errorf("attempt %d: got %v, want %v (cap)", attempt, d, backoff.DefaultMax)
		}
	}
}

func TestComputeJitterIsWithinBounds(t *testing.T) {
	const runs = 500
	for i := 0; i < runs; i++ {
		d := backoff.ComputeWith(3, backoff.DefaultBase, backoff.DefaultMax, 0.20)
		// Raw delay for attempt 3 is 4 s; ±20 % → [3.2 s, 4.8 s].
		if d < 3200*time.Millisecond || d > 4800*time.Millisecond {
			t.Fatalf("attempt 3 jitter out of bounds: %v", d)
		}
	}
}

func TestComputeAttemptZeroTreatedAsOne(t *testing.T) {
	d := backoff.ComputeWith(0, backoff.DefaultBase, backoff.DefaultMax, 0)
	if d != backoff.DefaultBase {
		t.Fatalf("attempt 0: got %v, want %v", d, backoff.DefaultBase)
	}
}

func TestComputeNegativeAttemptTreatedAsOne(t *testing.T) {
	d := backoff.ComputeWith(-5, backoff.DefaultBase, backoff.DefaultMax, 0)
	if d != backoff.DefaultBase {
		t.Fatalf("attempt -5: got %v, want %v", d, backoff.DefaultBase)
	}
}
