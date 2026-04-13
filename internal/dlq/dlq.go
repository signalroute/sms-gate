// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package dlq provides a simple in-memory dead-letter queue for SMS tasks
// that have exhausted all retry attempts.
package dlq

import (
	"sync"
	"time"
)

const defaultMaxSize = 1000

// Entry represents a failed SMS task placed in the dead-letter queue.
type Entry struct {
	ID        string
	To        string
	Body      string
	ICCID     string
	Reason    string
	CreatedAt time.Time
	Retries   int
}

// Queue is a bounded, thread-safe dead-letter queue.
// When the queue is full, the oldest entry is evicted to make room (FIFO eviction).
type Queue struct {
	mu      sync.RWMutex
	entries []Entry
	maxSize int
}

// New returns a new Queue with the given capacity. If maxSize is 0, it defaults
// to 1000.
func New(maxSize int) *Queue {
	if maxSize <= 0 {
		maxSize = defaultMaxSize
	}
	return &Queue{
		entries: make([]Entry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Push adds an entry to the queue. If the queue is already at capacity, the
// oldest entry is dropped first.
func (q *Queue) Push(e Entry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) >= q.maxSize {
		// drop the oldest entry (index 0)
		q.entries = q.entries[1:]
	}
	q.entries = append(q.entries, e)
}

// Drain returns all current entries and clears the queue.
func (q *Queue) Drain() []Entry {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Entry, len(q.entries))
	copy(out, q.entries)
	q.entries = q.entries[:0]
	return out
}

// Len returns the current number of entries in the queue.
func (q *Queue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.entries)
}
