// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"testing"
	"time"
)

// ── window ────────────────────────────────────────────────────────────────

func TestWindow_AllowUpToLimit(t *testing.T) {
	w := newWindow(3, time.Minute)
	now := time.Now()

	for i := 0; i < 3; i++ {
		if !w.Allow(now) {
			t.Errorf("Allow[%d] returned false, want true", i)
		}
	}
}

func TestWindow_BlockAtLimit(t *testing.T) {
	w := newWindow(3, time.Minute)
	now := time.Now()

	for i := 0; i < 3; i++ {
		w.Allow(now)
	}
	if w.Allow(now) {
		t.Error("Allow after limit should return false")
	}
}

func TestWindow_SlidesCorrectly(t *testing.T) {
	w := newWindow(3, 100*time.Millisecond)
	now := time.Now()

	// Fill the window.
	for i := 0; i < 3; i++ {
		w.Allow(now)
	}
	if w.Allow(now) {
		t.Fatal("should be blocked immediately after filling")
	}

	// Advance time past the window duration.
	future := now.Add(110 * time.Millisecond)
	if !w.Allow(future) {
		t.Error("should be allowed after window slides")
	}
}

func TestWindow_PartialExpiry(t *testing.T) {
	// Limit=3, duration=100ms.
	// Add 2 events at t=0, 1 at t=60ms.
	// At t=110ms: the 2 events from t=0 have expired; only the t=60ms event remains.
	// So we should be able to add 2 more at t=110ms (filling up to 3 again).
	w := newWindow(3, 100*time.Millisecond)
	t0 := time.Now()

	w.Allow(t0)                        // count=1
	w.Allow(t0)                        // count=2
	w.Allow(t0.Add(60 * time.Millisecond)) // count=3
	if w.Allow(t0.Add(60 * time.Millisecond)) {
		t.Fatal("should be blocked at count=3")
	}

	// At t=110ms the two t=0 events have expired; count=1.
	t1 := t0.Add(110 * time.Millisecond)
	if !w.Allow(t1) { // count → 2
		t.Error("first allow at t=110ms should succeed")
	}
	if !w.Allow(t1) { // count → 3
		t.Error("second allow at t=110ms should succeed")
	}
	if w.Allow(t1) { // count=3, blocked
		t.Error("third allow at t=110ms should be blocked")
	}
}

// ── SIMRateLimiter ────────────────────────────────────────────────────────

func TestSIMRateLimiter_UnderAllLimits(t *testing.T) {
	lim := newSIMRateLimiter(RateLimitConfig{PerMin: 3, PerHour: 10, PerDay: 100})
	for i := 0; i < 3; i++ {
		if !lim.Allow() {
			t.Errorf("Allow[%d] returned false, want true", i)
		}
	}
}

func TestSIMRateLimiter_BlocksAtPerMin(t *testing.T) {
	lim := newSIMRateLimiter(RateLimitConfig{PerMin: 2, PerHour: 100, PerDay: 1000})
	lim.Allow() // 1
	lim.Allow() // 2
	if lim.Allow() { // should block on per-min
		t.Error("should be blocked at per-min limit of 2")
	}
}

func TestSIMRateLimiter_BlocksAtPerHour(t *testing.T) {
	lim := newSIMRateLimiter(RateLimitConfig{PerMin: 100, PerHour: 3, PerDay: 1000})
	lim.Allow() // 1
	lim.Allow() // 2
	lim.Allow() // 3
	if lim.Allow() { // should block on per-hour
		t.Error("should be blocked at per-hour limit of 3")
	}
}

func TestSIMRateLimiter_BlocksAtPerDay(t *testing.T) {
	lim := newSIMRateLimiter(RateLimitConfig{PerMin: 100, PerHour: 100, PerDay: 2})
	lim.Allow()
	lim.Allow()
	if lim.Allow() {
		t.Error("should be blocked at per-day limit of 2")
	}
}

// ── RateLimiterRegistry ───────────────────────────────────────────────────

func TestRateLimiterRegistry_Allow(t *testing.T) {
	reg := NewRateLimiterRegistry()
	reg.Register("ICCID1", RateLimitConfig{PerMin: 2, PerHour: 10, PerDay: 100})

	if !reg.Allow("ICCID1") {
		t.Error("first Allow should succeed")
	}
	if !reg.Allow("ICCID1") {
		t.Error("second Allow should succeed")
	}
	if reg.Allow("ICCID1") {
		t.Error("third Allow should be blocked (per-min=2)")
	}
}

func TestRateLimiterRegistry_UnknownICCID(t *testing.T) {
	reg := NewRateLimiterRegistry()
	// Unknown ICCID: no limiter configured → always allow.
	for i := 0; i < 100; i++ {
		if !reg.Allow("UNKNOWN") {
			t.Errorf("Allow[%d] for unknown ICCID should return true", i)
		}
	}
}

func TestRateLimiterRegistry_IsolatesICCIDs(t *testing.T) {
	reg := NewRateLimiterRegistry()
	reg.Register("A", RateLimitConfig{PerMin: 1, PerHour: 100, PerDay: 1000})
	reg.Register("B", RateLimitConfig{PerMin: 1, PerHour: 100, PerDay: 1000})

	reg.Allow("A") // consume A's per-min quota

	// B's quota should be untouched.
	if !reg.Allow("B") {
		t.Error("B's quota should be independent of A's")
	}
	if reg.Allow("A") {
		t.Error("A is at limit, should be blocked")
	}
}

// ── Additional named coverage tests ──────────────────────────────────────

func TestWindow_Allow_UnderLimit(t *testing.T) {
	t.Parallel()
	const limit = 5
	w := newWindow(limit, time.Minute)
	now := time.Now()
	for i := 0; i < limit-1; i++ {
		if !w.Allow(now) {
			t.Errorf("Allow[%d] returned false, want true (under limit)", i)
		}
	}
}

func TestWindow_Allow_AtLimit(t *testing.T) {
	t.Parallel()
	const limit = 5
	w := newWindow(limit, time.Minute)
	now := time.Now()
	for i := 0; i < limit; i++ {
		w.Allow(now)
	}
	if w.Allow(now) {
		t.Error("Allow after limit should return false")
	}
}

func TestWindow_Allow_Expiry(t *testing.T) {
	const limit = 3
	w := newWindow(limit, 10*time.Millisecond)
	now := time.Now()
	for i := 0; i < limit; i++ {
		w.Allow(now)
	}
	if w.Allow(now) {
		t.Fatal("should be blocked immediately after filling")
	}
	time.Sleep(15 * time.Millisecond)
	future := time.Now()
	if !w.Allow(future) {
		t.Error("should be allowed after 10ms window expired")
	}
}

// TestSIMRateLimiter_AllWindows verifies each window independently blocks.
func TestSIMRateLimiter_AllWindows(t *testing.T) {
	t.Parallel()

	t.Run("per-min fires", func(t *testing.T) {
		lim := newSIMRateLimiter(RateLimitConfig{PerMin: 2, PerHour: 1000, PerDay: 10000})
		lim.Allow()
		lim.Allow()
		if lim.Allow() {
			t.Error("per-min window should block")
		}
	})

	t.Run("per-hour fires", func(t *testing.T) {
		lim := newSIMRateLimiter(RateLimitConfig{PerMin: 1000, PerHour: 2, PerDay: 10000})
		lim.Allow()
		lim.Allow()
		if lim.Allow() {
			t.Error("per-hour window should block")
		}
	})

	t.Run("per-day fires", func(t *testing.T) {
		lim := newSIMRateLimiter(RateLimitConfig{PerMin: 1000, PerHour: 1000, PerDay: 2})
		lim.Allow()
		lim.Allow()
		if lim.Allow() {
			t.Error("per-day window should block")
		}
	})
}

func TestSIMRateLimiter_AllPass(t *testing.T) {
	t.Parallel()
	lim := newSIMRateLimiter(RateLimitConfig{PerMin: 100, PerHour: 100, PerDay: 100})
	for i := 0; i < 10; i++ {
		if !lim.Allow() {
			t.Errorf("Allow[%d] returned false, want true", i)
		}
	}
}

func TestRateLimiterRegistry_NoLimiter(t *testing.T) {
	t.Parallel()
	reg := NewRateLimiterRegistry()
	for i := 0; i < 10; i++ {
		if !reg.Allow("ICCID_UNREGISTERED") {
			t.Errorf("Allow[%d] for unknown ICCID should return true", i)
		}
	}
}

func TestRateLimiterRegistry_Register_Allow(t *testing.T) {
	t.Parallel()
	reg := NewRateLimiterRegistry()
	reg.Register("ICCID_RL", RateLimitConfig{PerMin: 5, PerHour: 100, PerDay: 1000})
	for i := 0; i < 5; i++ {
		if !reg.Allow("ICCID_RL") {
			t.Errorf("Allow[%d] should pass (within per-min=5)", i)
		}
	}
	if reg.Allow("ICCID_RL") {
		t.Error("6th Allow should fail (per-min=5 exhausted)")
	}
}

func TestRateLimiterRegistry_MultipleICCIDs(t *testing.T) {
	t.Parallel()
	reg := NewRateLimiterRegistry()
	reg.Register("ICCID_X", RateLimitConfig{PerMin: 1, PerHour: 100, PerDay: 1000})
	reg.Register("ICCID_Y", RateLimitConfig{PerMin: 1, PerHour: 100, PerDay: 1000})

	reg.Allow("ICCID_X") // exhaust ICCID_X per-min

	if !reg.Allow("ICCID_Y") {
		t.Error("ICCID_Y quota must be independent of ICCID_X")
	}
	if reg.Allow("ICCID_X") {
		t.Error("ICCID_X should still be blocked")
	}
}

// ── wouldAllow correctness ─────────────────────────────────────────────────
// Verify that wouldAllow does NOT advance the window (pure check).

func TestWouldAllow_DoesNotAdvance(t *testing.T) {
	win := newWindow(3, time.Minute)
	now := time.Now()
	lim := &SIMRateLimiter{
		perMin:  win,
		perHour: newWindow(100, time.Hour),
		perDay:  newWindow(1000, 24*time.Hour),
	}

	// Check 10 times without actual Allow: counter must not advance.
	for i := 0; i < 10; i++ {
		if !lim.wouldAllow(win, now) {
			t.Errorf("wouldAllow[%d] returned false but nothing was consumed", i)
		}
	}
	// After all the checks, a real Allow should still succeed (nothing advanced).
	if !win.Allow(now) {
		t.Error("Allow after wouldAllow calls should succeed — window was not advanced")
	}
}
