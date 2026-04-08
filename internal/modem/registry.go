// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package modem

import (
	"errors"
	"sync"
)

// ErrModemNotFound is returned when an ICCID has no registered worker.
var ErrModemNotFound = errors.New("modem not found: ICCID not in registry")

// ErrModemBusy is returned when a task channel is full.
var ErrModemBusy = errors.New("modem busy: task queue full")

// Registry maps ICCIDs to active Modem Workers.
type Registry struct {
	mu      sync.RWMutex
	workers map[string]*Worker
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{workers: make(map[string]*Worker)}
}

// Register adds a worker for the given ICCID.
func (r *Registry) Register(iccid string, w *Worker) {
	r.mu.Lock()
	r.workers[iccid] = w
	r.mu.Unlock()
}

// Deregister removes the worker for the given ICCID.
func (r *Registry) Deregister(iccid string) {
	r.mu.Lock()
	delete(r.workers, iccid)
	r.mu.Unlock()
}

// Lookup returns the worker for the given ICCID.
func (r *Registry) Lookup(iccid string) (*Worker, bool) {
	r.mu.RLock()
	w, ok := r.workers[iccid]
	r.mu.RUnlock()
	return w, ok
}

// Dispatch enqueues a task on the target worker's task channel.
// Returns ErrModemNotFound if the ICCID is not registered.
// Returns ErrModemBusy if the task channel is full.
func (r *Registry) Dispatch(iccid string, task InboundTask) error {
	r.mu.RLock()
	w, ok := r.workers[iccid]
	r.mu.RUnlock()
	if !ok {
		return ErrModemNotFound
	}
	select {
	case w.taskCh <- task:
		return nil
	default:
		return ErrModemBusy
	}
}

// Snapshot returns a copy of the ICCID→WorkerStatus map for heartbeat reporting.
func (r *Registry) Snapshot() map[string]WorkerStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]WorkerStatus, len(r.workers))
	for iccid, w := range r.workers {
		out[iccid] = w.Status()
	}
	return out
}
