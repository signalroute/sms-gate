// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package router

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/signalroute/sms-gate/internal/metrics"
	"github.com/signalroute/sms-gate/internal/modem"
	"github.com/signalroute/sms-gate/internal/tunnel"
)

// ── Test helpers ──────────────────────────────────────────────────────────

// makeTask builds a tunnel.Task with a JSON payload.
func makeTask(action string, payload any) tunnel.Task {
	b, _ := json.Marshal(payload)
	return tunnel.Task{
		Envelope: tunnel.Envelope{
			Type:      tunnel.TypeTask,
			MessageID: "test-msg-id",
			TS:        time.Now().UnixMilli(),
		},
		Action:  action,
		Payload: json.RawMessage(b),
	}
}

// makeRegistry creates a Registry with one live worker for a given ICCID.
func makeRegistry(t *testing.T, iccid string) *modem.Registry {
	t.Helper()
	reg := modem.NewRegistry()
	w := &modem.Worker{}
	// Expose taskCh via the exported field — we wire it via the struct literal.
	// Since Worker is unexported-field-heavy, use the registry test helper path.
	// The registry only needs a *Worker pointer with a reachable taskCh.
	// We'll use a workaround: build the worker via NewWorker with a nil config
	// (not started, just registered) and drain from taskCh ourselves.
	// Actually modem.NewWorker requires non-nil dependencies.
	// The cleanest approach is to add a test-only exported constructor.
	// Since we can't modify router_test.go's package, use the same approach
	// as registry_test.go: build a bare worker with a buffered taskCh.
	_ = w
	tw := modem.NewWorkerForTest(iccid)
	reg.Register(iccid, tw)
	return reg
}

func TestRouter_Dispatch_SendSMS(t *testing.T) {
	iccid := "89490200001234567890"
	reg := makeRegistry(t, iccid)

	var pushedEvt any
	rtr := New(reg, func(evt any) { pushedEvt = evt }, nil)

	task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
		ICCID: iccid,
		To:    "+4915112345678",
		Body:  "Test message",
	})
	err := rtr.Dispatch(task)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// The task should land in the worker's channel.
	w, _ := reg.Lookup(iccid)
	select {
	case got := <-w.TaskCh():
		if got.Task.Action != tunnel.ActionSendSMS {
			t.Errorf("Action: got %q", got.Task.Action)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("task was not enqueued in worker's taskCh")
	}
	_ = pushedEvt
}

func TestRouter_Dispatch_RebootModem(t *testing.T) {
	iccid := "89490200001234567890"
	reg := makeRegistry(t, iccid)
	rtr := New(reg, func(evt any) {}, nil)

	task := makeTask(tunnel.ActionRebootModem, tunnel.RebootModemPayload{
		ICCID: iccid,
		Hard:  false,
	})
	if err := rtr.Dispatch(task); err != nil {
		t.Fatalf("Dispatch REBOOT_MODEM: %v", err)
	}
}

func TestRouter_Dispatch_CheckSignal(t *testing.T) {
	iccid := "89490200001234567890"
	reg := makeRegistry(t, iccid)
	rtr := New(reg, func(evt any) {}, nil)

	task := makeTask(tunnel.ActionCheckSignal, tunnel.CheckSignalPayload{ICCID: iccid})
	if err := rtr.Dispatch(task); err != nil {
		t.Fatalf("Dispatch CHECK_SIGNAL: %v", err)
	}
}

func TestRouter_Dispatch_DeleteAllSMS(t *testing.T) {
	iccid := "89490200001234567890"
	reg := makeRegistry(t, iccid)
	rtr := New(reg, func(evt any) {}, nil)

	task := makeTask(tunnel.ActionDeleteAllSMS, tunnel.DeleteAllSMSPayload{ICCID: iccid})
	if err := rtr.Dispatch(task); err != nil {
		t.Fatalf("Dispatch DELETE_ALL_SMS: %v", err)
	}
}

func TestRouter_Dispatch_ModemNotFound(t *testing.T) {
	reg := modem.NewRegistry() // empty registry
	rtr := New(reg, func(evt any) {}, nil)

	task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
		ICCID: "NOTREGISTERED",
		To:    "+49151",
		Body:  "test",
	})
	err := rtr.Dispatch(task)
	if err == nil {
		t.Fatal("expected error for unregistered ICCID, got nil")
	}
	if !errors.Is(err, modem.ErrModemNotFound) {
		t.Errorf("expected ErrModemNotFound in chain, got %v", err)
	}
}

func TestRouter_Dispatch_ModemBusy(t *testing.T) {
	iccid := "89490200001234567890"
	reg := makeRegistry(t, iccid)
	rtr := New(reg, func(evt any) {}, nil)

	// Fill the worker's task channel (size 1) first.
	w, _ := reg.Lookup(iccid)
	w.TaskCh() <- modem.InboundTask{} // occupy the slot

	task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
		ICCID: iccid, To: "+49151", Body: "busy test",
	})
	err := rtr.Dispatch(task)
	if err == nil {
		t.Fatal("expected ErrModemBusy, got nil")
	}
	if !errors.Is(err, modem.ErrModemBusy) {
		t.Errorf("expected ErrModemBusy, got %v", err)
	}
}

func TestRouter_Dispatch_UnsupportedAction(t *testing.T) {
	reg := modem.NewRegistry()
	rtr := New(reg, func(evt any) {}, nil)

	task := makeTask("UNKNOWN_ACTION", map[string]string{"iccid": "123"})
	err := rtr.Dispatch(task)
	if err == nil {
		t.Error("expected error for unsupported action, got nil")
	}
}

func TestRouter_Dispatch_MissingICCID(t *testing.T) {
	reg := modem.NewRegistry()
	rtr := New(reg, func(evt any) {}, nil)

	// Payload has no iccid field.
	task := makeTask(tunnel.ActionSendSMS, map[string]string{
		"to": "+49151", "body": "no iccid",
	})
	err := rtr.Dispatch(task)
	if err == nil {
		t.Error("expected error for missing ICCID in payload, got nil")
	}
}

func TestRouter_Dispatch_InvalidPayloadJSON(t *testing.T) {
	reg := modem.NewRegistry()
	rtr := New(reg, func(evt any) {}, nil)

	task := tunnel.Task{
		Envelope: tunnel.Envelope{Type: tunnel.TypeTask, MessageID: "x"},
		Action:   tunnel.ActionSendSMS,
		Payload:  json.RawMessage(`{not valid json`),
	}
	err := rtr.Dispatch(task)
	if err == nil {
		t.Error("expected error for invalid JSON payload, got nil")
	}
}

// ── AckFn wiring ──────────────────────────────────────────────────────────

func TestRouter_AckFn_IsWiredToWorker(t *testing.T) {
	iccid := "89490200001234567890"
	reg := makeRegistry(t, iccid)

	var ackReceived tunnel.TaskAckEvent
	rtr := New(reg, func(evt any) {
		if ack, ok := evt.(tunnel.TaskAckEvent); ok {
			ackReceived = ack
		}
	}, nil)

	task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
		ICCID: iccid, To: "+49151", Body: "ack test",
	})
	_ = rtr.Dispatch(task)

	// Simulate the worker calling AckFn.
	w, _ := reg.Lookup(iccid)
	select {
	case it := <-w.TaskCh():
		it.AckFn(tunnel.TaskAckEvent{
			Envelope: tunnel.NewEnvelopeFrom(tunnel.TypeTaskAck, task.MessageID),
			Status:   tunnel.StatusSuccess,
		})
	case <-time.After(100 * time.Millisecond):
		t.Fatal("no task in worker channel")
	}

	if ackReceived.Status != tunnel.StatusSuccess {
		t.Errorf("AckFn not wired correctly: got status %q", ackReceived.Status)
	}
}

// TestRouter_Dispatch_ModemBusy_EmitsMetric verifies that when the worker's
// inboundCh is full, the TasksDropped counter is incremented for the correct
// ICCID (#11).
func TestRouter_Dispatch_ModemBusy_EmitsMetric(t *testing.T) {
iccid := "89490200001234567891"
reg := makeRegistry(t, iccid)

// Build a real (but isolated) metrics registry so we can read the counter.
promReg := prometheus.NewRegistry()
m := metrics.New(promReg)
rtr := New(reg, func(evt any) {}, m)

// Fill the single-slot worker channel.
w, _ := reg.Lookup(iccid)
w.TaskCh() <- modem.InboundTask{}

task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
ICCID: iccid, To: "+49151", Body: "metric test",
})
_ = rtr.Dispatch(task) // expect ErrModemBusy

// Gather the counter value.
mfs, err := promReg.Gather()
if err != nil {
t.Fatalf("prometheus gather: %v", err)
}
var dropped float64
for _, mf := range mfs {
if mf.GetName() == "smsgate_tasks_dropped_total" {
for _, m := range mf.GetMetric() {
for _, lp := range m.GetLabel() {
if lp.GetName() == "iccid" && lp.GetValue() == iccid {
dropped = m.GetCounter().GetValue()
}
}
}
}
}
if dropped != 1 {
t.Fatalf("expected smsgate_tasks_dropped_total{iccid=%q}=1, got %v", iccid, dropped)
}
}
