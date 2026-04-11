// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"sync"
	"time"
)

// RateLimitConfig specifies per-SIM send limits.
type RateLimitConfig struct {
	PerMin  int
	PerHour int
	PerDay  int
}

// window is a sliding count over a fixed duration.
type window struct {
	mu       sync.Mutex
	limit    int
	duration time.Duration
	events   []time.Time
}

func newWindow(limit int, dur time.Duration) *window {
	return &window{limit: limit, duration: dur, events: make([]time.Time, 0, limit+1)}
}

// Allow returns true if a new event is within the limit, advancing the window.
func (w *window) Allow(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	cutoff := now.Add(-w.duration)
	// Evict expired events.
	valid := w.events[:0]
	for _, t := range w.events {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	w.events = valid

	if len(w.events) >= w.limit {
		return false
	}
	w.events = append(w.events, now)
	return true
}

// SIMRateLimiter enforces per-min, per-hour, and per-day SMS send limits for a SIM.
type SIMRateLimiter struct {
	perMin  *window
	perHour *window
	perDay  *window
}

func newSIMRateLimiter(cfg RateLimitConfig) *SIMRateLimiter {
	return &SIMRateLimiter{
		perMin:  newWindow(cfg.PerMin, time.Minute),
		perHour: newWindow(cfg.PerHour, time.Hour),
		perDay:  newWindow(cfg.PerDay, 24*time.Hour),
	}
}

// Allow returns true if the send is permitted under all three windows.
// If any window would be exceeded, none of them are advanced.
func (r *SIMRateLimiter) Allow() bool {
	now := time.Now()
	// Check all windows without advancing, then advance if all pass.
	if !r.tryAll(now) {
		return false
	}
	return true
}

func (r *SIMRateLimiter) tryAll(now time.Time) bool {
	// We must advance all-or-nothing. Use separate check+advance with rollback
	// by treating the event list as immutable during check.
	// Simpler: just check counts without adding.
	return r.wouldAllow(r.perMin, now) &&
		r.wouldAllow(r.perHour, now) &&
		r.wouldAllow(r.perDay, now) &&
		r.perMin.Allow(now) &&
		r.perHour.Allow(now) &&
		r.perDay.Allow(now)
}

func (r *SIMRateLimiter) wouldAllow(w *window, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := now.Add(-w.duration)
	count := 0
	for _, t := range w.events {
		if t.After(cutoff) {
			count++
		}
	}
	return count < w.limit
}

// RateLimiterRegistry holds per-ICCID rate limiters.
type RateLimiterRegistry struct {
	mu       sync.RWMutex
	limiters map[string]*SIMRateLimiter
}

func NewRateLimiterRegistry() *RateLimiterRegistry {
	return &RateLimiterRegistry{limiters: make(map[string]*SIMRateLimiter)}
}

// Register creates a rate limiter for the given ICCID.
func (r *RateLimiterRegistry) Register(iccid string, cfg RateLimitConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limiters[iccid] = newSIMRateLimiter(cfg)
}

// Allow returns true if a send is permitted for the given ICCID.
func (r *RateLimiterRegistry) Allow(iccid string) bool {
	r.mu.RLock()
	lim, ok := r.limiters[iccid]
	r.mu.RUnlock()
	if !ok {
		return true // no limiter configured → allow
	}
	return lim.Allow()
}
