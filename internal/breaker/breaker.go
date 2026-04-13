// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package breaker implements a three-state circuit breaker.
//
// The breaker starts in the Closed state. After [DefaultThreshold] consecutive
// failures it trips to Open. After [DefaultResetTimeout] it transitions to
// HalfOpen, allowing one probe request. A successful probe resets to Closed; a
// failed probe sends it back to Open.
package breaker

import (
	"sync"
	"time"
)

// State represents the circuit-breaker FSM state.
type State int

const (
	// Closed — normal operation; all calls are allowed.
	Closed State = iota
	// Open — breaker tripped; calls are rejected until the reset timeout elapses.
	Open
	// HalfOpen — one probe call is allowed to test whether the downstream is healthy.
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "Closed"
	case Open:
		return "Open"
	case HalfOpen:
		return "HalfOpen"
	default:
		return "Unknown"
	}
}

const (
	// DefaultThreshold is the number of consecutive failures required to trip the breaker.
	DefaultThreshold = 5
	// DefaultResetTimeout is how long the breaker stays Open before moving to HalfOpen.
	DefaultResetTimeout = 60 * time.Second
)

// Breaker is a concurrency-safe circuit breaker.
type Breaker struct {
	mu           sync.Mutex
	state        State
	failures     int
	threshold    int
	resetTimeout time.Duration
	openUntil    time.Time
}

// New creates a Breaker that trips after threshold consecutive failures and
// stays Open for resetTimeout before allowing a probe.
func New(threshold int, resetTimeout time.Duration) *Breaker {
	return &Breaker{
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

// NewDefault creates a Breaker with DefaultThreshold and DefaultResetTimeout.
func NewDefault() *Breaker {
	return New(DefaultThreshold, DefaultResetTimeout)
}

// Allow reports whether the caller should attempt the guarded operation.
//
//   - Closed → always true.
//   - Open → false, unless the reset timeout has elapsed, in which case the
//     breaker transitions to HalfOpen and returns true.
//   - HalfOpen → true (one probe allowed).
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Closed:
		return true
	case Open:
		if time.Now().Before(b.openUntil) {
			return false
		}
		// Timeout elapsed: move to HalfOpen and permit the probe.
		b.state = HalfOpen
		return true
	case HalfOpen:
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful operation.
//
//   - Closed → resets the consecutive-failure counter.
//   - HalfOpen → resets to Closed.
//   - Open → no-op (Allow returned false, so success should not be recorded).
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures = 0
	if b.state == HalfOpen || b.state == Open {
		b.state = Closed
	}
}

// RecordFailure records a failed operation.
//
//   - Closed → increments failure counter; trips to Open when threshold is reached.
//   - HalfOpen → probe failed; return to Open and restart the reset timer.
//   - Open → no-op.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Open:
		// Already open; nothing to do.
	case HalfOpen:
		// Probe failed — back to Open.
		b.state = Open
		b.openUntil = time.Now().Add(b.resetTimeout)
	case Closed:
		b.failures++
		if b.failures >= b.threshold {
			b.state = Open
			b.openUntil = time.Now().Add(b.resetTimeout)
		}
	}
}

// State returns the current FSM state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Failures returns the current consecutive-failure count.
// It is zero whenever the breaker is in Open or HalfOpen state.
func (b *Breaker) Failures() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures
}
