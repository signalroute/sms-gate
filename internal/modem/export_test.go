// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package modem

// This file exposes minimal test hooks used by router_test and other
// cross-package tests.  It must not be compiled into production builds
// but Go does not support build tags on non-_test files cleanly, so we
// keep the surface minimal and clearly documented.

// NewWorkerForTest creates a Worker with only its taskCh initialised.
// The worker is NOT started — it has no serial port or AT serializer.
// Use it to populate a Registry for unit-testing the Task Router.
func NewWorkerForTest(iccid string) *Worker {
	w := &Worker{
		iccid:  iccid,
		taskCh: make(chan InboundTask, 1),
	}
	return w
}

// TaskCh exposes the worker's task channel for test inspection.
// Production code must never call this — use Registry.Dispatch instead.
func (w *Worker) TaskCh() chan InboundTask {
	return w.taskCh
}
