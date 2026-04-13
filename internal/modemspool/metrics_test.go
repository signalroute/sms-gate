// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modemspool

import (
	"sync"
	"testing"
	"time"
)

func TestNewPool_Empty(t *testing.T) {
	p := NewPool()
	snap := p.Snapshot()
	if len(snap) != 0 {
		t.Fatalf("expected empty pool, got %d entries", len(snap))
	}
}

func TestRecordSent_IncrementsCount(t *testing.T) {
	p := NewPool()
	p.RecordSent("iccid-1")
	p.RecordSent("iccid-1")
	snap := p.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].SentCount != 2 {
		t.Errorf("expected SentCount=2, got %d", snap[0].SentCount)
	}
}

func TestRecordError_IncrementsCount(t *testing.T) {
	p := NewPool()
	p.RecordError("iccid-2")
	p.RecordError("iccid-2")
	p.RecordError("iccid-2")
	snap := p.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].ErrCount != 3 {
		t.Errorf("expected ErrCount=3, got %d", snap[0].ErrCount)
	}
}

func TestUnknownICCID_AutoRegisters(t *testing.T) {
	p := NewPool()
	p.RecordSent("new-iccid")
	snap := p.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry after auto-register, got %d", len(snap))
	}
	if snap[0].ICCID != "new-iccid" {
		t.Errorf("expected ICCID=new-iccid, got %q", snap[0].ICCID)
	}
}

func TestSnapshot_ReturnsCopy(t *testing.T) {
	p := NewPool()
	p.RecordSent("iccid-3")
	snap := p.Snapshot()
	snap[0].SentCount = 9999 // mutate the copy
	snap2 := p.Snapshot()
	if snap2[0].SentCount == 9999 {
		t.Error("Snapshot returned a reference, not a copy")
	}
}

func TestLastActive_UpdatedOnRecord(t *testing.T) {
	p := NewPool()
	before := time.Now()
	p.RecordSent("iccid-4")
	after := time.Now()
	snap := p.Snapshot()
	if snap[0].LastActive.Before(before) || snap[0].LastActive.After(after) {
		t.Errorf("LastActive %v not in [%v, %v]", snap[0].LastActive, before, after)
	}
}

func TestConcurrentAccess_Safe(t *testing.T) {
	p := NewPool()
	var wg sync.WaitGroup
	const goroutines = 50
	const ops = 100

	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				p.RecordSent("iccid-race")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				p.RecordError("iccid-race")
			}
		}()
	}
	// concurrent snapshots
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = p.Snapshot()
		}()
	}

	wg.Wait()
	snap := p.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	total := snap[0].SentCount + snap[0].ErrCount
	expected := uint64(goroutines * ops * 2)
	if total != expected {
		t.Errorf("expected total %d ops, got %d", expected, total)
	}
}
