// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package dedup_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/dedup"
)

// TestFirstMessageNotDuplicate verifies that the very first occurrence of a
// message is never flagged as a duplicate.
func TestFirstMessageNotDuplicate(t *testing.T) {
	d := dedup.NewDefault()
	if d.IsDuplicate("iccid1", "+491234567890", "Hello") {
		t.Error("first message should not be a duplicate")
	}
}

// TestSameMessageWithinWindowIsDuplicate verifies that an identical
// (iccid, to, body) arriving within the window is flagged as a duplicate.
func TestSameMessageWithinWindowIsDuplicate(t *testing.T) {
	d := dedup.NewDefault()
	d.IsDuplicate("iccid1", "+491234567890", "Hello") // first — not a dup
	if !d.IsDuplicate("iccid1", "+491234567890", "Hello") {
		t.Error("second identical message within window should be a duplicate")
	}
}

// TestSameMessageAfterWindowNotDuplicate verifies that the same message
// arriving after the window expires is allowed through.
func TestSameMessageAfterWindowNotDuplicate(t *testing.T) {
	d := dedup.New(100, 20*time.Millisecond)
	d.IsDuplicate("iccid1", "+491234567890", "Hello") // first — not a dup

	time.Sleep(30 * time.Millisecond) // let the window expire

	if d.IsDuplicate("iccid1", "+491234567890", "Hello") {
		t.Error("message after window expiry should not be a duplicate")
	}
}

// TestDifferentBodyNotDuplicate verifies that a different body is not flagged.
func TestDifferentBodyNotDuplicate(t *testing.T) {
	d := dedup.NewDefault()
	d.IsDuplicate("iccid1", "+491234567890", "Hello")
	if d.IsDuplicate("iccid1", "+491234567890", "World") {
		t.Error("message with different body should not be a duplicate")
	}
}

// TestDifferentICCIDNotDuplicate verifies that the same (to, body) on a
// different ICCID is not flagged.
func TestDifferentICCIDNotDuplicate(t *testing.T) {
	d := dedup.NewDefault()
	d.IsDuplicate("iccid1", "+491234567890", "Hello")
	if d.IsDuplicate("iccid2", "+491234567890", "Hello") {
		t.Error("same message on different ICCID should not be a duplicate")
	}
}

// TestDifferentRecipientNotDuplicate verifies that the same (iccid, body)
// to a different number is not flagged.
func TestDifferentRecipientNotDuplicate(t *testing.T) {
	d := dedup.NewDefault()
	d.IsDuplicate("iccid1", "+491111111111", "Hello")
	if d.IsDuplicate("iccid1", "+492222222222", "Hello") {
		t.Error("same message to different recipient should not be a duplicate")
	}
}

// TestRingBufferEvictsOldest verifies that when the buffer reaches maxSize the
// oldest entry is evicted and a previously-seen message is no longer detected
// as a duplicate.
func TestRingBufferEvictsOldest(t *testing.T) {
	const size = 5
	d := dedup.New(size, time.Minute)

	// First message: key "0".
	d.IsDuplicate("iccid1", "+49000", "msg-0")

	// Fill the rest of the buffer (keys "1"–"4").
	for i := 1; i < size; i++ {
		d.IsDuplicate("iccid1", "+49000", fmt.Sprintf("msg-%d", i))
	}

	// Adding one more should evict "msg-0".
	d.IsDuplicate("iccid1", "+49000", "msg-5")

	// "msg-0" should no longer be a duplicate (evicted).
	if d.IsDuplicate("iccid1", "+49000", "msg-0") {
		t.Error("evicted entry should not be detected as a duplicate")
	}
}

// TestConcurrentSafety exercises IsDuplicate under concurrent goroutines to
// detect data races when run with -race.
func TestConcurrentSafety(t *testing.T) {
	d := dedup.New(50, 50*time.Millisecond)
	done := make(chan struct{})

	for i := 0; i < 8; i++ {
		i := i
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					d.IsDuplicate(fmt.Sprintf("iccid%d", i%3), "+4912345", "body")
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(done)
}
