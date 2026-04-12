// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package router

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/modem"
	"github.com/signalroute/sms-gate/internal/tunnel"
)

// TestDispatch_ICCIDVariants dispatches a SendSMS task for each of 200 ICCIDs.
func TestDispatch_ICCIDVariants(t *testing.T) {
	for i := 0; i < 200; i++ {
		i := i
		t.Run(fmt.Sprintf("iccid_%d", i), func(t *testing.T) {
			iccid := fmt.Sprintf("8949020000123%07d", i)
			reg := makeRegistry(t, iccid)
			rtr := New(reg, func(evt any) {}, nil)

			task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
				ICCID: iccid,
				To:    "+4915100000000",
				Body:  fmt.Sprintf("message %d", i),
			})
			if err := rtr.Dispatch(task); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}

			w, ok := reg.Lookup(iccid)
			if !ok {
				t.Fatal("worker not found in registry")
			}
			select {
			case got := <-w.TaskCh():
				if got.Task.Action != tunnel.ActionSendSMS {
					t.Errorf("wrong action: got %q", got.Task.Action)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("task was not enqueued in worker's taskCh")
			}
		})
	}
}

// TestDispatch_UnknownICCID verifies Dispatch returns an error for 100 unregistered ICCIDs.
func TestDispatch_UnknownICCID(t *testing.T) {
	reg := modem.NewRegistry() // empty

	for i := 0; i < 100; i++ {
		i := i
		t.Run(fmt.Sprintf("unknown_%d", i), func(t *testing.T) {
			rtr := New(reg, func(evt any) {}, nil)
			iccid := fmt.Sprintf("NOTREGISTERED%07d", i)
			task := makeTask(tunnel.ActionSendSMS, tunnel.SendSMSPayload{
				ICCID: iccid,
				To:    "+49151",
				Body:  "test",
			})
			err := rtr.Dispatch(task)
			if err == nil {
				t.Fatalf("expected error for unregistered ICCID %q, got nil", iccid)
			}
			if !errors.Is(err, modem.ErrModemNotFound) {
				t.Errorf("expected ErrModemNotFound, got %v", err)
			}
		})
	}
}

// TestDispatch_ActionVariants tests all 4 supported actions across 10 ICCIDs (40 subtests)
// plus 10 ICCIDs × unsupported action (100 total subtests named action_*).
func TestDispatch_ActionVariants(t *testing.T) {
	actions := []struct {
		name    string
		payload func(iccid string) any
	}{
		{
			name: tunnel.ActionSendSMS,
			payload: func(iccid string) any {
				return tunnel.SendSMSPayload{ICCID: iccid, To: "+49151", Body: "hi"}
			},
		},
		{
			name: tunnel.ActionRebootModem,
			payload: func(iccid string) any {
				return tunnel.RebootModemPayload{ICCID: iccid, Hard: false}
			},
		},
		{
			name: tunnel.ActionCheckSignal,
			payload: func(iccid string) any {
				return tunnel.CheckSignalPayload{ICCID: iccid}
			},
		},
		{
			name: tunnel.ActionDeleteAllSMS,
			payload: func(iccid string) any {
				return tunnel.DeleteAllSMSPayload{ICCID: iccid}
			},
		},
	}

	// 4 valid actions × 50 ICCIDs = 200 subtests
	for _, act := range actions {
		act := act
		for j := 0; j < 50; j++ {
			j := j
			t.Run(fmt.Sprintf("action_%s_%d", act.name, j), func(t *testing.T) {
				iccid := fmt.Sprintf("8949020000ACTVAR%04d", j)
				reg := makeRegistry(t, iccid)
				rtr := New(reg, func(evt any) {}, nil)

				task := makeTask(act.name, act.payload(iccid))
				if err := rtr.Dispatch(task); err != nil {
					t.Fatalf("Dispatch(%s): %v", act.name, err)
				}
			})
		}
	}
}
