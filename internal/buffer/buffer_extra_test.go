// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package buffer

import (
	"fmt"
	"testing"
	"time"
)

// TestInsert_ManyEntries inserts 500 unique entries and verifies each succeeds.
func TestInsert_ManyEntries(t *testing.T) {
	buf := openTestBuffer(t)
	ts := time.Now().UnixMilli()
	var prevID int64

	for i := 0; i < 500; i++ {
		i := i
		t.Run(fmt.Sprintf("entry_%d", i), func(t *testing.T) {
			hash := fmt.Sprintf("sha256:unique%08x000000000000000000000000000000000000000000000000", i)
			id, isDup, err := buf.Insert("89490200001234567890", "+491234", "body", hash, "", ts)
			if err != nil {
				t.Fatalf("Insert: %v", err)
			}
			if isDup {
				t.Error("expected isDup == false")
			}
			if id <= 0 {
				t.Errorf("id should be positive, got %d", id)
			}
			if id <= prevID {
				t.Errorf("id should be incrementing: prev=%d got=%d", prevID, id)
			}
			prevID = id
		})
	}
}

// TestInsert_DeduplicationTable inserts each of 200 hashes twice; second must be isDup.
func TestInsert_DeduplicationTable(t *testing.T) {
	buf := openTestBuffer(t)
	ts := time.Now().UnixMilli()

	for i := 0; i < 200; i++ {
		i := i
		t.Run(fmt.Sprintf("dup_%d", i), func(t *testing.T) {
			hash := fmt.Sprintf("sha256:dup%08x0000000000000000000000000000000000000000000000000", i)

			_, isDup1, err := buf.Insert("89490200001234567890", "+491", "msg", hash, "", ts)
			if err != nil {
				t.Fatalf("first Insert: %v", err)
			}
			if isDup1 {
				t.Error("first insert should not be duplicate")
			}

			id2, isDup2, err := buf.Insert("89490200001234567890", "+491", "msg", hash, "", ts)
			if err != nil {
				t.Fatalf("second Insert: %v", err)
			}
			if !isDup2 {
				t.Error("second insert should be duplicate")
			}
			if id2 != 0 {
				t.Errorf("duplicate id should be 0, got %d", id2)
			}
		})
	}
}

// TestPendingCount_Comprehensive inserts N entries and checks PendingCount == N for N in 1..100.
func TestPendingCount_Comprehensive(t *testing.T) {
	for n := 1; n <= 100; n++ {
		n := n
		t.Run(fmt.Sprintf("n_%d", n), func(t *testing.T) {
			buf := openTestBuffer(t)
			ts := time.Now().UnixMilli()

			for i := 0; i < n; i++ {
				hash := fmt.Sprintf("sha256:cnt%04d%08x000000000000000000000000000000000000000000", n, i)
				_, _, err := buf.Insert("89490200001234567890", "+491", "body", hash, "", ts)
				if err != nil {
					t.Fatalf("Insert[%d]: %v", i, err)
				}
			}

			got, err := buf.PendingCount()
			if err != nil {
				t.Fatalf("PendingCount: %v", err)
			}
			if got != n {
				t.Errorf("PendingCount: got %d, want %d", got, n)
			}
		})
	}
}

// TestMarkDelivered_Comprehensive inserts 300 entries, marks each delivered,
// and verifies PendingCount decreases by 1 each time.
func TestMarkDelivered_Comprehensive(t *testing.T) {
	buf := openTestBuffer(t)
	ts := time.Now().UnixMilli()
	const total = 300

	ids := make([]int64, total)
	for i := 0; i < total; i++ {
		hash := fmt.Sprintf("sha256:ack%08x0000000000000000000000000000000000000000000000000", i)
		id, _, err := buf.Insert("89490200001234567890", "+491", "body", hash, "", ts)
		if err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
		ids[i] = id
	}

	for i := 0; i < total; i++ {
		i := i
		t.Run(fmt.Sprintf("ack_%d", i), func(t *testing.T) {
			before, countErr := buf.PendingCount()
			if countErr != nil {
				t.Fatalf("PendingCount before: %v", countErr)
			}

			if markErr := buf.MarkDelivered(ids[i]); markErr != nil {
				t.Fatalf("MarkDelivered(%d): %v", ids[i], markErr)
			}

			after, countErr := buf.PendingCount()
			if countErr != nil {
				t.Fatalf("PendingCount after: %v", countErr)
			}
			if after != before-1 {
				t.Errorf("PendingCount: before=%d after=%d, expected decrement by 1", before, after)
			}
		})
	}
}

// TestInsert_ICCIDVariants inserts one entry per unique ICCID format and verifies no errors.
func TestInsert_ICCIDVariants(t *testing.T) {
	buf := openTestBuffer(t)
	ts := time.Now().UnixMilli()

	for i := 0; i < 200; i++ {
		i := i
		t.Run(fmt.Sprintf("iccid_%d", i), func(t *testing.T) {
			iccid := fmt.Sprintf("894902000012345%05d", i)
			hash := fmt.Sprintf("sha256:iccid%08x000000000000000000000000000000000000000000000000", i)
			_, _, err := buf.Insert(iccid, "+491234", "hello", hash, "", ts)
			if err != nil {
				t.Fatalf("Insert with iccid %q: %v", iccid, err)
			}
		})
	}
}

// TestInsert_SenderVariants inserts one entry per unique sender and verifies no errors.
func TestInsert_SenderVariants(t *testing.T) {
	buf := openTestBuffer(t)
	ts := time.Now().UnixMilli()

	for i := 0; i < 200; i++ {
		i := i
		t.Run(fmt.Sprintf("sender_%d", i), func(t *testing.T) {
			sender := fmt.Sprintf("+491234%05d", i)
			hash := fmt.Sprintf("sha256:sndr%08x000000000000000000000000000000000000000000000000", i)
			_, _, err := buf.Insert("89490200001234567890", sender, "hello", hash, "", ts)
			if err != nil {
				t.Fatalf("Insert with sender %q: %v", sender, err)
			}
		})
	}
}
