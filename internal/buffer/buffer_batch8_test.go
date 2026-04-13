// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package buffer

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ── TestPurge_ConcurrentInserts (#48) ─────────────────────────────────────

func TestPurge_ConcurrentInserts(t *testing.T) {
	buf := openTestBuffer(t)

	var wg sync.WaitGroup
	// Insert 50 rows concurrently while calling Purge.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(fmt.Sprintf("concurrent-%d", i))))
			_, _, _ = buf.Insert("89490200001234567890", "+491", "body", hash, "", time.Now().UnixMilli())
		}(i)
	}

	// Concurrent purge.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = buf.Purge(0)
	}()

	wg.Wait()
	// No deadlock or panic means success.
}

// ── TestInsert_MarkDelivered_Atomicity (#58) ──────────────────────────────
// Verify that Insert followed by MarkDelivered works correctly even under
// concurrent pressure. This doesn't test crash recovery (would need process kill)
// but ensures the operations are properly serialized by SQLite.

func TestInsert_MarkDelivered_Atomicity(t *testing.T) {
	buf := openTestBuffer(t)

	var ids []int64
	for i := 0; i < 20; i++ {
		hash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(fmt.Sprintf("atomic-%d", i))))
		id, _, err := buf.Insert("89490200001234567890", "+491", "body", hash, "", time.Now().UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// Mark first 10 as delivered concurrently.
	var wg sync.WaitGroup
	for _, id := range ids[:10] {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			if err := buf.MarkDelivered(id); err != nil {
				t.Errorf("MarkDelivered(%d): %v", id, err)
			}
		}(id)
	}
	wg.Wait()

	pending, err := buf.PendingCount()
	if err != nil {
		t.Fatal(err)
	}
	if pending != 10 {
		t.Errorf("pending: got %d, want 10", pending)
	}
}

// ── TestBuffer_RetentionPolicy (#131) ─────────────────────────────────────
// Insert entries and backdate via direct SQL, then verify Purge.

func TestBuffer_RetentionPolicy(t *testing.T) {
	buf := openTestBuffer(t)

	now := time.Now().UnixMilli()
	old := now - 8*24*60*60*1000 // 8 days ago

	// Insert and deliver a row.
	hash1 := "sha256:0000000000000000000000000000000000000000000000000000000000000001"
	id1, _, err := buf.Insert("89490200001234567890", "+491", "old msg", hash1, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := buf.MarkDelivered(id1); err != nil {
		t.Fatal(err)
	}

	// Backdate created_at via the unexported db handle (same package).
	if _, err := buf.db.Exec("UPDATE sms_buffer SET created_at = ? WHERE id = ?", old, id1); err != nil {
		t.Fatal(err)
	}

	// Insert a recent row and deliver it too.
	hash2 := "sha256:0000000000000000000000000000000000000000000000000000000000000002"
	id2, _, err := buf.Insert("89490200001234567890", "+491", "new msg", hash2, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := buf.MarkDelivered(id2); err != nil {
		t.Fatal(err)
	}

	// Purge with 7-day retention — only the old row should be deleted.
	purged, err := buf.Purge(7)
	if err != nil {
		t.Fatal(err)
	}
	if purged != 1 {
		t.Errorf("purged: got %d, want 1 (only old row)", purged)
	}
}

// ── TestBuffer_InsertBenchmark (#107) ─────────────────────────────────────

func BenchmarkBuffer_Insert(b *testing.B) {
	dir := b.TempDir()
	buf, err := Open(dir+"/bench.db", nil)
	if err != nil {
		b.Fatal(err)
	}
	defer buf.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hash := fmt.Sprintf("sha256:%064x", i)
		iccid := fmt.Sprintf("8949020000123456%04d", i%10000)
		_, _, _ = buf.Insert(iccid, "+491234", "benchmark body", hash, "", time.Now().UnixMilli())
	}
}

func BenchmarkBuffer_PendingRows(b *testing.B) {
	dir := b.TempDir()
	buf, err := Open(dir+"/bench.db", nil)
	if err != nil {
		b.Fatal(err)
	}
	defer buf.Close()

	// Pre-populate.
	for i := 0; i < 1000; i++ {
		hash := fmt.Sprintf("sha256:%064x", i)
		_, _, _ = buf.Insert("89490200001234567890", "+491", "body", hash, "", time.Now().UnixMilli())
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = buf.PendingRows()
	}
}

// ── TestICCIDValidation (#35 extra coverage) ──────────────────────────────

func TestInsert_InvalidICCID(t *testing.T) {
	buf := openTestBuffer(t)

	cases := []struct {
		name  string
		iccid string
	}{
		{"empty", ""},
		{"letters", "ABCDEFGHIJKLMNOPQRST"},
		{"too_short", "894902000012"},
		{"too_long", "894902000012345678901234"},
		{"special_chars", "89490200001234!@#$"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := buf.Insert(tc.iccid, "+491", "body", "sha256:test", "", time.Now().UnixMilli())
			if err == nil {
				t.Errorf("expected error for ICCID %q", tc.iccid)
			}
		})
	}
}
