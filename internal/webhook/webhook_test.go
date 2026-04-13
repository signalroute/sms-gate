// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/webhook"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testPayload() webhook.Payload {
	return webhook.Payload{
		Event:     "sms.received",
		ICCID:     "894410000000000001",
		From:      "+15551234567",
		Body:      "Hello world",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// TestNotify_Success verifies a successful delivery on first attempt.
func TestNotify_Success(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		var p webhook.Payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if p.Event != "sms.received" {
			t.Errorf("unexpected event %q", p.Event)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := webhook.New(webhook.Config{URL: srv.URL, Logger: discardLogger()})
	if err := n.Notify(context.Background(), testPayload()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received.Load() != 1 {
		t.Errorf("expected 1 request, got %d", received.Load())
	}
}

// TestNotify_Signature verifies the HMAC-SHA256 signature header.
func TestNotify_Signature(t *testing.T) {
	secret := "super-secret"
	var sigReceived string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigReceived = r.Header.Get("X-Signalroute-Signature")
		body, _ := io.ReadAll(r.Body)
		// Verify the signature ourselves.
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if sigReceived != expected {
			t.Errorf("signature mismatch: got %q want %q", sigReceived, expected)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := webhook.New(webhook.Config{URL: srv.URL, Secret: secret, Logger: discardLogger()})
	if err := n.Notify(context.Background(), testPayload()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sigReceived, "sha256=") {
		t.Errorf("expected sha256= prefix, got %q", sigReceived)
	}
}

// TestNotify_NoSignature verifies no signature header when secret is empty.
func TestNotify_NoSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sig := r.Header.Get("X-Signalroute-Signature"); sig != "" {
			t.Errorf("unexpected signature header: %q", sig)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := webhook.New(webhook.Config{URL: srv.URL, Logger: discardLogger()})
	if err := n.Notify(context.Background(), testPayload()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestNotify_RetriesOnServerError verifies that transient 5xx responses are retried.
func TestNotify_RetriesOnServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := webhook.New(webhook.Config{
		URL:    srv.URL,
		Logger: discardLogger(),
		Client: &http.Client{Timeout: 5 * time.Second},
	})
	if err := n.Notify(context.Background(), testPayload()); err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", calls.Load())
	}
}

// TestNotify_FailsAfterMaxAttempts verifies an error is returned after 3 failures.
func TestNotify_FailsAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := webhook.New(webhook.Config{
		URL:    srv.URL,
		Logger: discardLogger(),
		Client: &http.Client{Timeout: 5 * time.Second},
	})
	err := n.Notify(context.Background(), testPayload())
	if err == nil {
		t.Fatal("expected error after max attempts")
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
}

// TestNotify_ContextCancellation verifies delivery stops when ctx is cancelled.
func TestNotify_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	n := webhook.New(webhook.Config{
		URL:    srv.URL,
		Logger: discardLogger(),
		Client: &http.Client{Timeout: 5 * time.Second},
	})
	err := n.Notify(ctx, testPayload())
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

// TestNew_NilWhenURLEmpty verifies that New returns nil when URL is empty.
func TestNew_NilWhenURLEmpty(t *testing.T) {
	n := webhook.New(webhook.Config{URL: ""})
	if n != nil {
		t.Fatal("expected nil notifier when URL is empty")
	}
}

// TestFromEnv_NilWhenURLUnset verifies that FromEnv returns nil when WEBHOOK_URL is unset.
func TestFromEnv_NilWhenURLUnset(t *testing.T) {
	t.Setenv("WEBHOOK_URL", "")
	t.Setenv("WEBHOOK_SECRET", "")
	n := webhook.FromEnv(discardLogger())
	if n != nil {
		t.Fatal("expected nil notifier when WEBHOOK_URL is empty")
	}
}

// TestFromEnv_CreatesNotifier verifies FromEnv creates a working notifier.
func TestFromEnv_CreatesNotifier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	t.Setenv("WEBHOOK_URL", srv.URL)
	t.Setenv("WEBHOOK_SECRET", "my-secret")

	n := webhook.FromEnv(discardLogger())
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
	if err := n.Notify(context.Background(), testPayload()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
