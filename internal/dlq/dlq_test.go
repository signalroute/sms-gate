// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package dlq

import (
	"sync"
	"testing"
)

func entry(id string) Entry {
	return Entry{ID: id, To: "+1555000000", Body: "test", ICCID: "iccid-1", Reason: "max retries", Retries: 3}
}

func TestPush_AddsEntry(t *testing.T) {
	q := New(10)
	q.Push(entry("e1"))
	if q.Len() != 1 {
		t.Errorf("expected Len=1, got %d", q.Len())
	}
}

func TestLen_ReturnsCorrectCount(t *testing.T) {
	q := New(10)
	for i := 0; i < 5; i++ {
		q.Push(entry("x"))
	}
	if q.Len() != 5 {
		t.Errorf("expected Len=5, got %d", q.Len())
	}
}

func TestDrain_ReturnsAllAndClears(t *testing.T) {
	q := New(10)
	q.Push(entry("a"))
	q.Push(entry("b"))
	q.Push(entry("c"))

	out := q.Drain()
	if len(out) != 3 {
		t.Fatalf("expected 3 entries from Drain, got %d", len(out))
	}
	if q.Len() != 0 {
		t.Errorf("queue should be empty after Drain, got Len=%d", q.Len())
	}
}

func TestOverflow_DropsOldest(t *testing.T) {
	q := New(3)
	q.Push(entry("first"))
	q.Push(entry("second"))
	q.Push(entry("third"))
	q.Push(entry("fourth")) // should evict "first"

	out := q.Drain()
	if len(out) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(out))
	}
	if out[0].ID != "second" {
		t.Errorf("expected oldest remaining entry to be 'second', got %q", out[0].ID)
	}
	if out[2].ID != "fourth" {
		t.Errorf("expected newest entry to be 'fourth', got %q", out[2].ID)
	}
}

func TestNew_ZeroDefaultsTo1000(t *testing.T) {
	q := New(0)
	if q.maxSize != 1000 {
		t.Errorf("expected maxSize=1000 for New(0), got %d", q.maxSize)
	}
}

func TestConcurrentPushDrain_Safe(t *testing.T) {
	q := New(500)
	var wg sync.WaitGroup
	const producers = 20
	const perProducer = 50

	wg.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				q.Push(entry("concurrent"))
			}
		}()
	}

	// concurrent drains
	wg.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			_ = q.Drain()
		}()
	}

	wg.Wait()
	// No assertion on final count — just verify no race / panic.
	_ = q.Len()
}
