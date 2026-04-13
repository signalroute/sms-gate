// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package dedup provides duplicate SMS detection using an in-memory sliding
// window keyed on (iccid, to, body).
//
// A message is considered a duplicate when an identical (iccid, to, body)
// tuple was seen within the configured time window and the ring buffer still
// holds that entry. Both conditions must be true: the window must not have
// expired AND the ring buffer must not have been full enough to evict the
// earlier entry.
package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// DefaultMaxSize is the number of message fingerprints kept in the ring buffer.
	DefaultMaxSize = 100
	// DefaultWindow is the time after which an identical message is no longer
	// considered a duplicate.
	DefaultWindow = time.Minute
)

// Dedup detects duplicate SMS messages.
//
// It maintains a fixed-size ring buffer of the most recent message
// fingerprints together with their arrival timestamps. A new message is a
// duplicate when its fingerprint already exists in the buffer and was recorded
// within the time window.
type Dedup struct {
	mu      sync.Mutex
	entries []entry
	maxSize int
	window  time.Duration
}

type entry struct {
	key string    // SHA-256 of (iccid + "\x00" + to + "\x00" + body)
	ts  time.Time // when the message was first seen
}

// New returns a Dedup with the given ring-buffer size and deduplication window.
func New(maxSize int, window time.Duration) *Dedup {
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	return &Dedup{
		maxSize: maxSize,
		window:  window,
		entries: make([]entry, 0, maxSize),
	}
}

// NewDefault returns a Dedup with DefaultMaxSize (100) and DefaultWindow (1 minute).
func NewDefault() *Dedup {
	return New(DefaultMaxSize, DefaultWindow)
}

// IsDuplicate reports whether the message (iccid, to, body) is a duplicate of
// a message seen within the deduplication window.
//
// If it is not a duplicate the message fingerprint is added to the ring buffer
// so that future identical messages are detected. If the buffer is full the
// oldest entry is evicted first.
func (d *Dedup) IsDuplicate(iccid, to, body string) bool {
	key := fingerprint(iccid, to, body)
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check for an existing, non-expired entry with the same key.
	for _, e := range d.entries {
		if e.key == key && now.Sub(e.ts) < d.window {
			return true
		}
	}

	// Not a duplicate — record the fingerprint.
	if len(d.entries) >= d.maxSize {
		// Evict the oldest entry (index 0) by shifting.
		d.entries = d.entries[1:]
	}
	d.entries = append(d.entries, entry{key: key, ts: now})
	return false
}

// fingerprint returns a short hex digest uniquely representing the (iccid, to, body) tuple.
func fingerprint(iccid, to, body string) string {
	h := sha256.New()
	h.Write([]byte(iccid))
	h.Write([]byte{0})
	h.Write([]byte(to))
	h.Write([]byte{0})
	h.Write([]byte(body))
	return hex.EncodeToString(h.Sum(nil)[:16]) // first 128 bits is sufficient
}
