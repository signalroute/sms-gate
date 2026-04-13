// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package breaker_test

import (
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/breaker"
)

// TestStartsClosed verifies the initial state is Closed and Allow returns true.
func TestStartsClosed(t *testing.T) {
	b := breaker.New(5, 60*time.Second)
	if b.State() != breaker.Closed {
		t.Fatalf("want Closed, got %s", b.State())
	}
	if !b.Allow() {
		t.Error("Allow() should return true in Closed state")
	}
}

// TestTripsAfterThreshold verifies the breaker opens after the configured
// number of consecutive failures.
func TestTripsAfterThreshold(t *testing.T) {
	const threshold = 5
	b := breaker.New(threshold, 60*time.Second)

	for i := 0; i < threshold-1; i++ {
		b.RecordFailure()
		if b.State() != breaker.Closed {
			t.Fatalf("after %d failures want Closed, got %s", i+1, b.State())
		}
	}

	b.RecordFailure() // this is the threshold-th failure
	if b.State() != breaker.Open {
		t.Fatalf("after %d failures want Open, got %s", threshold, b.State())
	}
}

// TestOpenRejectsRequests verifies Allow() returns false while the breaker is Open.
func TestOpenRejectsRequests(t *testing.T) {
	b := breaker.New(1, 60*time.Second)
	b.RecordFailure() // trip immediately (threshold=1)

	if b.State() != breaker.Open {
		t.Fatalf("want Open, got %s", b.State())
	}
	if b.Allow() {
		t.Error("Allow() should return false in Open state before timeout")
	}
}

// TestHalfOpenAfterTimeout verifies the breaker transitions to HalfOpen once
// the reset timeout elapses.
func TestHalfOpenAfterTimeout(t *testing.T) {
	b := breaker.New(1, 10*time.Millisecond)
	b.RecordFailure()

	if b.State() != breaker.Open {
		t.Fatalf("want Open, got %s", b.State())
	}

	time.Sleep(20 * time.Millisecond)

	// Allow() should transition to HalfOpen and return true.
	if !b.Allow() {
		t.Error("Allow() should return true after reset timeout")
	}
	if b.State() != breaker.HalfOpen {
		t.Errorf("want HalfOpen after timeout, got %s", b.State())
	}
}

// TestSuccessInHalfOpenRecloses verifies that a successful probe in HalfOpen
// resets the breaker to Closed.
func TestSuccessInHalfOpenRecloses(t *testing.T) {
	b := breaker.New(1, 10*time.Millisecond)
	b.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	b.Allow() // transitions to HalfOpen

	b.RecordSuccess()
	if b.State() != breaker.Closed {
		t.Errorf("want Closed after success in HalfOpen, got %s", b.State())
	}
}

// TestFailureInHalfOpenReopens verifies that a failed probe in HalfOpen sends
// the breaker back to Open.
func TestFailureInHalfOpenReopens(t *testing.T) {
	b := breaker.New(1, 10*time.Millisecond)
	b.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	b.Allow() // transitions to HalfOpen

	b.RecordFailure()
	if b.State() != breaker.Open {
		t.Errorf("want Open after failure in HalfOpen, got %s", b.State())
	}
	// The reset timer must have been restarted, so Allow should return false immediately.
	if b.Allow() {
		t.Error("Allow() should return false immediately after re-opening from HalfOpen")
	}
}

// TestSuccessResetsFailureCounter verifies that a success in Closed state
// resets the consecutive-failure counter.
func TestSuccessResetsFailureCounter(t *testing.T) {
	b := breaker.New(5, 60*time.Second)

	// Record 4 failures (one below the threshold).
	for i := 0; i < 4; i++ {
		b.RecordFailure()
	}
	if b.Failures() != 4 {
		t.Fatalf("want 4 failures, got %d", b.Failures())
	}

	b.RecordSuccess()
	if b.Failures() != 0 {
		t.Errorf("want 0 failures after success, got %d", b.Failures())
	}
	if b.State() != breaker.Closed {
		t.Errorf("want Closed, got %s", b.State())
	}
}

// TestStateString verifies the String() method returns sensible names.
func TestStateString(t *testing.T) {
	cases := []struct {
		s    breaker.State
		want string
	}{
		{breaker.Closed, "Closed"},
		{breaker.Open, "Open"},
		{breaker.HalfOpen, "HalfOpen"},
	}
	for _, tc := range cases {
		if tc.s.String() != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", tc.s, tc.s.String(), tc.want)
		}
	}
}

// TestConcurrentAccess exercises the breaker under concurrent goroutines to
// detect data races when run with -race.
func TestConcurrentAccess(t *testing.T) {
	b := breaker.New(5, 10*time.Millisecond)
	done := make(chan struct{})

	for i := 0; i < 8; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					if b.Allow() {
						b.RecordFailure()
					}
					_ = b.State()
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(done)
}
