// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package ratelimit_test

import (
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/ratelimit"
)

func TestRegistry_StringKey(t *testing.T) {
	reg := ratelimit.NewRegistry[string](3)

	for i := 0; i < 3; i++ {
		ok, _ := reg.Allow("key-1")
		if !ok {
			t.Fatalf("expected allow on attempt %d", i+1)
		}
	}
	ok, retry := reg.Allow("key-1")
	if ok {
		t.Fatal("expected deny after limit")
	}
	if retry < time.Second {
		t.Errorf("expected retryAfter >= 1s, got %v", retry)
	}
}

func TestRegistry_IntKey(t *testing.T) {
	reg := ratelimit.NewRegistry[int64](2)
	reg.Allow(42)
	reg.Allow(42)

	ok, _ := reg.Allow(42)
	if ok {
		t.Fatal("expected deny for int key after limit")
	}

	// Different key should still be allowed.
	ok, _ = reg.Allow(99)
	if !ok {
		t.Fatal("expected allow for different int key")
	}
}

func TestRegistry_Len(t *testing.T) {
	reg := ratelimit.NewRegistry[string](10)
	if reg.Len() != 0 {
		t.Fatalf("expected 0, got %d", reg.Len())
	}
	reg.Allow("a")
	reg.Allow("b")
	if reg.Len() != 2 {
		t.Fatalf("expected 2, got %d", reg.Len())
	}
}

type addr struct{ ip [4]byte }

func TestRegistry_CustomComparableKey(t *testing.T) {
	reg := ratelimit.NewRegistry[addr](1)
	a := addr{ip: [4]byte{192, 168, 1, 1}}

	ok, _ := reg.Allow(a)
	if !ok {
		t.Fatal("first allow should succeed")
	}
	ok, _ = reg.Allow(a)
	if ok {
		t.Fatal("second allow should be denied")
	}
}
