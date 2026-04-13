// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package ratelimit provides per-ICCID outbound SMS rate limiting using a
// token-bucket algorithm.
//
// Configuration:
//
//	RATE_LIMIT_PER_ICCID  maximum messages per minute per ICCID (0 = disabled, default 10)
//
// When the limit is exceeded, Middleware returns HTTP 429 Too Many Requests
// with a Retry-After header indicating the seconds until the next token is
// available.
package ratelimit

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const defaultPerMin = 10

// bucket is a token-bucket for one ICCID.
type bucket struct {
	mu       sync.Mutex
	tokens   float64
	maxTokens float64
	refillRate float64 // tokens per nanosecond
	lastRefill time.Time
}

func newBucket(perMin int) *bucket {
	max := float64(perMin)
	rate := max / float64(time.Minute) // tokens per ns
	return &bucket{
		tokens:     max,
		maxTokens:  max,
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

// Allow consumes one token and reports whether the request is permitted.
// If denied, retryAfter is the duration until the next token is available.
func (b *bucket) Allow() (ok bool, retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill)
	b.lastRefill = now

	b.tokens += elapsed.Seconds() * b.refillRate * float64(time.Second)
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// Compute time until one full token is available.
	needed := 1 - b.tokens
	waitNs := time.Duration(needed / (b.refillRate * float64(time.Second)) * float64(time.Second))
	if waitNs < time.Second {
		waitNs = time.Second
	}
	return false, waitNs
}

// Limiter holds per-ICCID token buckets and enforces the rate limit.
// For a type-safe generic version, see Registry[K].
type Limiter struct {
	mu     sync.Mutex
	perMin int
	buckets map[string]*bucket
}

// FromEnv creates a Limiter configured from the RATE_LIMIT_PER_ICCID env var.
// Returns nil if the rate limit is disabled (value 0).
func FromEnv() *Limiter {
	perMin := defaultPerMin
	if v := os.Getenv("RATE_LIMIT_PER_ICCID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			perMin = n
		}
	}
	if perMin == 0 {
		return nil
	}
	return New(perMin)
}

// New creates a Limiter with the given per-ICCID messages-per-minute limit.
func New(perMin int) *Limiter {
	return &Limiter{
		perMin:  perMin,
		buckets: make(map[string]*bucket),
	}
}

// Allow checks and advances the token bucket for the given ICCID.
// Returns (true, 0) if the request is permitted, or (false, retryAfter) if denied.
func (l *Limiter) Allow(iccid string) (bool, time.Duration) {
	l.mu.Lock()
	b, ok := l.buckets[iccid]
	if !ok {
		b = newBucket(l.perMin)
		l.buckets[iccid] = b
	}
	l.mu.Unlock()
	return b.Allow()
}

// Middleware returns an http.Handler that enforces rate limits per ICCID.
// The ICCID is read from the "X-ICCID" request header. If the header is
// absent the request is allowed through (no ICCID to limit on).
// When the limit is exceeded, a 429 response is written with a Retry-After
// header (in seconds).
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		iccid := r.Header.Get("X-ICCID")
		if iccid == "" {
			next.ServeHTTP(w, r)
			return
		}
		ok, retryAfter := l.Allow(iccid)
		if !ok {
			secs := int(retryAfter.Seconds())
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", secs))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
