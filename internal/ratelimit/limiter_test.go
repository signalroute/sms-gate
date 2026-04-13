// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/ratelimit"
)

func TestLimiter_AllowsUpToLimit(t *testing.T) {
	l := ratelimit.New(5) // 5 per minute

	for i := 0; i < 5; i++ {
		ok, _ := l.Allow("iccid-1")
		if !ok {
			t.Fatalf("expected allow on attempt %d", i+1)
		}
	}
}

func TestLimiter_DeniesAfterLimit(t *testing.T) {
	l := ratelimit.New(3)

	for i := 0; i < 3; i++ {
		ok, _ := l.Allow("iccid-x")
		if !ok {
			t.Fatalf("expected allow on attempt %d", i+1)
		}
	}
	ok, retryAfter := l.Allow("iccid-x")
	if ok {
		t.Fatal("expected deny after limit exhausted")
	}
	if retryAfter < time.Second {
		t.Errorf("expected retryAfter >= 1s, got %v", retryAfter)
	}
}

func TestLimiter_IndependentPerICCID(t *testing.T) {
	l := ratelimit.New(2)

	// Exhaust iccid-A.
	l.Allow("iccid-A")
	l.Allow("iccid-A")

	// iccid-B should still be allowed.
	ok, _ := l.Allow("iccid-B")
	if !ok {
		t.Fatal("expected iccid-B to be allowed independently")
	}

	// iccid-A should now be denied.
	ok, _ = l.Allow("iccid-A")
	if ok {
		t.Fatal("expected iccid-A to be denied")
	}
}

func TestLimiter_RetryAfterIsPositive(t *testing.T) {
	l := ratelimit.New(1)

	l.Allow("iccid-r") // consume the single token
	ok, retryAfter := l.Allow("iccid-r")
	if ok {
		t.Fatal("expected deny")
	}
	if retryAfter <= 0 {
		t.Errorf("Retry-After must be positive, got %v", retryAfter)
	}
}

func TestFromEnv_DefaultsTo10(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_ICCID", "")
	l := ratelimit.FromEnv()
	if l == nil {
		t.Fatal("expected non-nil limiter with default 10/min")
	}

	// Should allow 10 requests.
	for i := 0; i < 10; i++ {
		ok, _ := l.Allow("iccid-d")
		if !ok {
			t.Fatalf("expected allow on attempt %d", i+1)
		}
	}
	ok, _ := l.Allow("iccid-d")
	if ok {
		t.Fatal("expected deny after 10 requests")
	}
}

func TestFromEnv_Zero_ReturnsNil(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_ICCID", "0")
	l := ratelimit.FromEnv()
	if l != nil {
		t.Fatal("expected nil limiter when limit is 0 (disabled)")
	}
}

func TestFromEnv_CustomValue(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_ICCID", "7")
	l := ratelimit.FromEnv()
	if l == nil {
		t.Fatal("expected non-nil limiter")
	}
	for i := 0; i < 7; i++ {
		ok, _ := l.Allow("iccid-c")
		if !ok {
			t.Fatalf("expected allow on attempt %d", i+1)
		}
	}
	ok, _ := l.Allow("iccid-c")
	if ok {
		t.Fatal("expected deny after 7 requests")
	}
}

func TestMiddleware_Returns429WithRetryAfter(t *testing.T) {
	l := ratelimit.New(1)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request — should pass.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-ICCID", "iccid-m")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Second request — rate limited.
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.Header.Set("X-ICCID", "iccid-m")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec2.Code)
	}
	ra := rec2.Header().Get("Retry-After")
	if ra == "" {
		t.Fatal("expected Retry-After header")
	}
	secs, err := strconv.Atoi(ra)
	if err != nil || secs < 1 {
		t.Errorf("Retry-After must be a positive integer, got %q", ra)
	}
}

func TestMiddleware_NoICCID_PassesThrough(t *testing.T) {
	l := ratelimit.New(1)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// No X-ICCID header → request passes through regardless.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}
