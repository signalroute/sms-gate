// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

// NewWorkerForTest creates a Worker with only its inboundCh initialized.
// The worker is NOT started — it has no serial port or AT serializer.
// Use it to populate a Registry for unit-testing the Task Router.
func NewWorkerForTest(iccid string) *Worker {
	return &Worker{
		iccid:     iccid,
		inboundCh: make(chan InboundTask, 1),
	}
}

// TaskCh exposes the worker's inbound task channel for test inspection.
// Production code must never call this — use Registry.Dispatch instead.
func (w *Worker) TaskCh() chan InboundTask {
	return w.inboundCh
}
