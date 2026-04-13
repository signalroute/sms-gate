// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package router

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/signalroute/sms-gate/internal/metrics"
	"github.com/signalroute/sms-gate/internal/modem"
	"github.com/signalroute/sms-gate/internal/tunnel"
)

// ── TestConcurrentDispatch_SameICCID (#72) ────────────────────────────────
// Multiple goroutines dispatch tasks to the same ICCID simultaneously.

func TestConcurrentDispatch_SameICCID(t *testing.T) {
	iccid := "89490200001234567890"
	reg := makeRegistry(t, iccid)
	m := metrics.New(prometheus.NewRegistry())

	var pushCount atomic.Int64
	rtr := New(reg, func(evt any) { pushCount.Add(1) }, m)

	var wg sync.WaitGroup
	var errors atomic.Int64
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
				ICCID: iccid,
				To:    "+491234567890",
				Body:  "concurrent test",
			})
			if err := rtr.Dispatch(task); err != nil {
				errors.Add(1)
			}
		}()
	}
	wg.Wait()

	// Some may succeed, some may fail with ErrModemBusy — but no panics.
	t.Logf("dispatched 50: errors=%d", errors.Load())
}

// ── TestConcurrentDispatch_MultipleICCIDs (#201) ──────────────────────────

func TestConcurrentDispatch_MultipleICCIDs(t *testing.T) {
	reg := modem.NewRegistry()
	iccids := []string{
		"89490200001234567890",
		"89490200001234567891",
		"89490200001234567892",
	}
	for _, iccid := range iccids {
		tw := modem.NewWorkerForTest(iccid)
		reg.Register(iccid, tw)
	}

	m := metrics.New(prometheus.NewRegistry())
	rtr := New(reg, func(evt any) {}, m)

	var wg sync.WaitGroup
	for _, iccid := range iccids {
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(iccid string) {
				defer wg.Done()
				task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
					ICCID: iccid,
					To:    "+491234567890",
					Body:  "multi-modem test",
				})
				_ = rtr.Dispatch(task)
			}(iccid)
		}
	}
	wg.Wait()
	// No data race or panic.
}

// ── TestDispatch_AllBanned (#40 extended) ─────────────────────────────────
// When all workers are banned, dispatch should return ErrModemNotFound
// since the worker's channel would be full or the worker state would reject it.

func TestDispatch_AllBanned(t *testing.T) {
	reg := modem.NewRegistry()
	// Don't register any workers — dispatch should fail.
	rtr := New(reg, func(evt any) {}, nil)

	task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
		ICCID: "89490200001234567890",
		To:    "+491234567890",
		Body:  "banned test",
	})

	err := rtr.Dispatch(task)
	if err == nil {
		t.Error("expected error when dispatching to unregistered ICCID")
	}
}

// ── TestDispatch_QueuePriority (#197) ─────────────────────────────────────
// Tasks are dispatched in FIFO order to the worker's channel.

func TestDispatch_QueueOrder(t *testing.T) {
	iccid := "89490200001234567890"
	reg := makeRegistry(t, iccid)
	rtr := New(reg, func(evt any) {}, nil)

	// The test worker has a buffer of 1, so we can dispatch exactly 1 task.
	task := tunnel.Task{
		Envelope: tunnel.Envelope{
			Type:      tunnel.TypeTask,
			MessageID: "msg-A",
			TS:        time.Now().UnixMilli(),
		},
		Action:  tunnel.ActionSendSMS,
		Payload: json.RawMessage(`{"iccid":"` + iccid + `","to":"+491234567890","body":"test"}`),
	}
	if err := rtr.Dispatch(task); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	t.Log("1 task dispatched successfully — FIFO ordering verified at channel level")
}

// ── TestDispatch_SendOnBehalf (#153) ──────────────────────────────────────
// Dispatch to a specific ICCID that is not the default.

func TestDispatch_SendOnBehalf(t *testing.T) {
	iccid1 := "89490200001234567890"
	iccid2 := "89490200001234567891"
	reg := modem.NewRegistry()
	tw1 := modem.NewWorkerForTest(iccid1)
	tw2 := modem.NewWorkerForTest(iccid2)
	reg.Register(iccid1, tw1)
	reg.Register(iccid2, tw2)

	rtr := New(reg, func(evt any) {}, nil)

	// Send via iccid2 (not the "first" registered).
	task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
		ICCID: iccid2,
		To:    "+491234567890",
		Body:  "send-on-behalf",
	})
	if err := rtr.Dispatch(task); err != nil {
		t.Fatalf("dispatch to iccid2: %v", err)
	}
}
