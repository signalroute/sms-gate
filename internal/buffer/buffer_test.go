// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package buffer

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

// openTestBuffer creates an in-memory SQLite buffer for testing.
// ":memory:" gives a fresh DB per test with no disk I/O.
func openTestBuffer(t *testing.T) *Buffer {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	buf, err := Open(":memory:", log)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { buf.Close() })
	return buf
}

// ── Insert ────────────────────────────────────────────────────────────────

func TestInsert_Basic(t *testing.T) {
	buf := openTestBuffer(t)

	id, isDup, err := buf.Insert("89490200001234567890", "+491234", "Your OTP is 123456", "sha256:aabbcc", "+4917629900000", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if isDup {
		t.Error("first insert should not be a duplicate")
	}
	if id <= 0 {
		t.Errorf("id should be positive, got %d", id)
	}
}

func TestInsert_PendingCount(t *testing.T) {
	buf := openTestBuffer(t)

	for i := 0; i < 5; i++ {
		hash := "sha256:" + string(rune('a'+i)) + "bcd"
		_, _, err := buf.Insert("ICCID1", "+491", "body", hash, "", time.Now().UnixMilli())
		if err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
	}

	count, err := buf.PendingCount()
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != 5 {
		t.Errorf("PendingCount: got %d, want 5", count)
	}
}

// ── Deduplication ─────────────────────────────────────────────────────────

func TestInsert_DuplicatePDUHash(t *testing.T) {
	buf := openTestBuffer(t)

	hash := "sha256:deadbeef000000000000000000000000000000000000000000000000000000"

	id1, isDup1, err := buf.Insert("ICCID1", "+491", "msg", hash, "", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if isDup1 {
		t.Error("first insert flagged as duplicate")
	}

	id2, isDup2, err := buf.Insert("ICCID1", "+491", "msg", hash, "", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("second Insert: %v", err)
	}
	if !isDup2 {
		t.Error("second insert should be flagged as duplicate")
	}
	if id2 != 0 {
		t.Errorf("duplicate id should be 0, got %d", id2)
	}

	// Count should still be 1.
	count, _ := buf.PendingCount()
	if count != 1 {
		t.Errorf("PendingCount after duplicate: got %d, want 1", count)
	}
	_ = id1
}

func TestInsert_SameHashDifferentICCID(t *testing.T) {
	// Same hash, different ICCID: still a duplicate (hash is global).
	buf := openTestBuffer(t)
	hash := "sha256:cafebabe000000000000000000000000000000000000000000000000000000"

	_, isDup1, _ := buf.Insert("ICCID1", "+491", "msg", hash, "", time.Now().UnixMilli())
	_, isDup2, _ := buf.Insert("ICCID2", "+492", "msg", hash, "", time.Now().UnixMilli())

	if isDup1 {
		t.Error("first should not be duplicate")
	}
	if !isDup2 {
		t.Error("second should be duplicate regardless of ICCID")
	}
}

// ── MarkDelivered ─────────────────────────────────────────────────────────

func TestMarkDelivered(t *testing.T) {
	buf := openTestBuffer(t)

	id, _, err := buf.Insert("ICCID1", "+491", "test", "sha256:1111", "", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := buf.MarkDelivered(id); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	// After delivery, pending count should be 0.
	count, _ := buf.PendingCount()
	if count != 0 {
		t.Errorf("PendingCount after delivery: got %d, want 0", count)
	}
}

func TestMarkDelivered_PartialDelivery(t *testing.T) {
	buf := openTestBuffer(t)

	var ids [3]int64
	for i := range ids {
		id, _, err := buf.Insert("ICCID1", "+491", "msg", "sha256:"+string(rune('A'+i))+"111", "", time.Now().UnixMilli())
		if err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
		ids[i] = id
	}

	// Deliver only the first one.
	if err := buf.MarkDelivered(ids[0]); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	count, _ := buf.PendingCount()
	if count != 2 {
		t.Errorf("PendingCount: got %d, want 2", count)
	}
}

// ── PendingRows ───────────────────────────────────────────────────────────

func TestPendingRows_OrderByID(t *testing.T) {
	buf := openTestBuffer(t)

	senders := []string{"+491", "+492", "+493"}
	for i, s := range senders {
		_, _, err := buf.Insert("ICCID1", s, "body", "sha256:hash"+string(rune('0'+i)), "", int64(1000+i))
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	rows, err := buf.PendingRows()
	if err != nil {
		t.Fatalf("PendingRows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("PendingRows: got %d rows, want 3", len(rows))
	}

	// Must be ordered by ascending ID.
	for i := 1; i < len(rows); i++ {
		if rows[i].ID <= rows[i-1].ID {
			t.Errorf("rows not sorted by ID: rows[%d].ID=%d, rows[%d].ID=%d", i-1, rows[i-1].ID, i, rows[i].ID)
		}
	}
}

func TestPendingRows_ExcludesDelivered(t *testing.T) {
	buf := openTestBuffer(t)

	id1, _, _ := buf.Insert("ICCID1", "+491", "pending", "sha256:P111", "", time.Now().UnixMilli())
	id2, _, _ := buf.Insert("ICCID1", "+492", "delivered", "sha256:D222", "", time.Now().UnixMilli())

	buf.MarkDelivered(id2)

	rows, err := buf.PendingRows()
	if err != nil {
		t.Fatalf("PendingRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending row, got %d", len(rows))
	}
	if rows[0].ID != id1 {
		t.Errorf("wrong row returned: id=%d, want %d", rows[0].ID, id1)
	}
}

// ── Purge ─────────────────────────────────────────────────────────────────

func TestPurge_RemovesOldDelivered(t *testing.T) {
	buf := openTestBuffer(t)

	// The purge works on the created_at column (Unix ms).
	// To test "old" rows we need to insert and then update their created_at
	// to be older than the retention window.
	id, _, err := buf.Insert("ICCID1", "+491", "old msg", "sha256:OLD1", "", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	buf.MarkDelivered(id)

	// Manually back-date the created_at to 8 days ago.
	oldTs := time.Now().AddDate(0, 0, -8).UnixMilli()
	_, err = buf.db.Exec(`UPDATE sms_buffer SET created_at = ? WHERE id = ?`, oldTs, id)
	if err != nil {
		t.Fatalf("backdate: %v", err)
	}

	n, err := buf.Purge(7)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n != 1 {
		t.Errorf("Purge: deleted %d rows, want 1", n)
	}
}

func TestPurge_KeepsRecentDelivered(t *testing.T) {
	buf := openTestBuffer(t)

	id, _, _ := buf.Insert("ICCID1", "+491", "recent", "sha256:NEW1", "", time.Now().UnixMilli())
	buf.MarkDelivered(id)

	n, err := buf.Purge(7)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n != 0 {
		t.Errorf("Purge deleted recent delivered row (n=%d), should be 0", n)
	}
}

func TestPurge_NeverPurgesPending(t *testing.T) {
	buf := openTestBuffer(t)

	// Insert a PENDING row with a very old date.
	id, _, _ := buf.Insert("ICCID1", "+491", "old pending", "sha256:OLD2", "", time.Now().UnixMilli())

	oldTs := time.Now().AddDate(0, 0, -30).UnixMilli()
	buf.db.Exec(`UPDATE sms_buffer SET created_at = ? WHERE id = ?`, oldTs, id)

	n, _ := buf.Purge(7)
	if n != 0 {
		t.Errorf("Purge removed PENDING row — must never happen (n=%d)", n)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────

func TestConcurrentInserts(t *testing.T) {
	// Verify WAL mode allows concurrent readers without corruption.
	buf := openTestBuffer(t)
	const n = 20

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			hash := "sha256:" + string(rune('A'+i%26)) + string(rune('0'+i%10)) + "000"
			_, _, err := buf.Insert("ICCID1", "+491", "body", hash+string(rune(i)), "", time.Now().UnixMilli())
			if err != nil {
				t.Errorf("goroutine %d Insert: %v", i, err)
			}
		}()
	}
	wg.Wait()

	count, err := buf.PendingCount()
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != n {
		t.Errorf("PendingCount after concurrent inserts: got %d, want %d", count, n)
	}
}
