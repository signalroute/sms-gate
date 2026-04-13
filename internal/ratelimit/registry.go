// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package ratelimit

import (
	"sync"
	"time"
)

// Registry is a generic per-key rate limiter. K can be any comparable type
// (string ICCID, int64 user ID, net/netip.Addr, etc.).
//
// Usage:
//
//	reg := ratelimit.NewRegistry[string](10) // 10 per minute per key
//	ok, retry := reg.Allow("89490200001234567890")
//
//	ipReg := ratelimit.NewRegistry[netip.Addr](100)
//	ok, retry = ipReg.Allow(clientAddr)
type Registry[K comparable] struct {
	mu      sync.Mutex
	perMin  int
	buckets map[K]*bucket
}

// NewRegistry creates a generic rate-limit registry keyed by K.
func NewRegistry[K comparable](perMin int) *Registry[K] {
	return &Registry[K]{
		perMin:  perMin,
		buckets: make(map[K]*bucket),
	}
}

// Allow checks and advances the token bucket for the given key.
// Returns (true, 0) if permitted, or (false, retryAfter) if the limit is
// exceeded.
func (r *Registry[K]) Allow(key K) (bool, time.Duration) {
	r.mu.Lock()
	b, ok := r.buckets[key]
	if !ok {
		b = newBucket(r.perMin)
		r.buckets[key] = b
	}
	r.mu.Unlock()
	return b.Allow()
}

// Len returns the number of tracked keys.
func (r *Registry[K]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buckets)
}
