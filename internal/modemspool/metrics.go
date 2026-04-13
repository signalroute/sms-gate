// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package modemspool tracks per-modem send/error metrics in memory.
package modemspool

import (
	"sync"
	"time"
)

// ModemMetrics holds runtime counters for a single modem identified by ICCID.
type ModemMetrics struct {
	ICCID      string
	SentCount  uint64
	ErrCount   uint64
	LastActive time.Time
}

// Pool is a thread-safe registry of per-modem metrics.
type Pool struct {
	mu     sync.RWMutex
	modems map[string]*ModemMetrics
}

// NewPool returns an empty Pool.
func NewPool() *Pool {
	return &Pool{modems: make(map[string]*ModemMetrics)}
}

func (p *Pool) getOrCreate(iccid string) *ModemMetrics {
	if m, ok := p.modems[iccid]; ok {
		return m
	}
	m := &ModemMetrics{ICCID: iccid}
	p.modems[iccid] = m
	return m
}

// RecordSent increments the sent counter for the given ICCID and updates LastActive.
// The ICCID is auto-registered if not already present.
func (p *Pool) RecordSent(iccid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.getOrCreate(iccid)
	m.SentCount++
	m.LastActive = time.Now()
}

// RecordError increments the error counter for the given ICCID and updates LastActive.
// The ICCID is auto-registered if not already present.
func (p *Pool) RecordError(iccid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.getOrCreate(iccid)
	m.ErrCount++
	m.LastActive = time.Now()
}

// Snapshot returns a copy of all current metrics as a value slice.
func (p *Pool) Snapshot() []ModemMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ModemMetrics, 0, len(p.modems))
	for _, m := range p.modems {
		out = append(out, *m) // copy by value
	}
	return out
}
