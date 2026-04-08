// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

// Package router implements the Task Router (§4.3 of the spec).
package router

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yanujz/go-sms-gate/internal/modem"
	"github.com/yanujz/go-sms-gate/internal/tunnel"
)

// Router dispatches inbound Tasks to the appropriate Modem Worker.
type Router struct {
	registry *modem.Registry
	pushFn   func(evt any) // push events (TASK_ACK, MODEM_ALERT) back to tunnel
}

// New creates a Router.
func New(registry *modem.Registry, pushFn func(evt any)) *Router {
	return &Router{registry: registry, pushFn: pushFn}
}

// Dispatch routes a Task to the target Modem Worker.
// This is set as tunnel.Manager.InboundTaskFn.
func (r *Router) Dispatch(task tunnel.Task) error {
	iccid, err := extractICCID(task)
	if err != nil {
		return fmt.Errorf("%w: %v", modem.ErrModemNotFound, err)
	}

	it := modem.InboundTask{
		Task: task,
		AckFn: func(ack tunnel.TaskAckEvent) {
			r.pushFn(ack)
		},
		AlertFn: func(alert tunnel.ModemAlertEvent) {
			r.pushFn(alert)
		},
	}

	if err := r.registry.Dispatch(iccid, it); err != nil {
		switch {
		case errors.Is(err, modem.ErrModemNotFound):
			return fmt.Errorf("%s: %w", tunnel.ErrCodeModemNotFound, err)
		case errors.Is(err, modem.ErrModemBusy):
			return fmt.Errorf("%s: %w", tunnel.ErrCodeModemBusy, err)
		}
		return err
	}
	return nil
}

// extractICCID pulls the ICCID from the task payload for routing.
// All task payloads include an iccid field.
func extractICCID(task tunnel.Task) (string, error) {
	switch task.Action {
	case tunnel.ActionSendSMS:
		var p tunnel.SendSMSPayload
		if err := json.Unmarshal(task.Payload, &p); err != nil {
			return "", fmt.Errorf("parse SEND_SMS payload: %w", err)
		}
		if p.ICCID == "" {
			return "", fmt.Errorf("payload.iccid is required")
		}
		return p.ICCID, nil

	case tunnel.ActionRebootModem:
		var p tunnel.RebootModemPayload
		if err := json.Unmarshal(task.Payload, &p); err != nil {
			return "", fmt.Errorf("parse REBOOT_MODEM payload: %w", err)
		}
		if p.ICCID == "" {
			return "", fmt.Errorf("payload.iccid is required")
		}
		return p.ICCID, nil

	case tunnel.ActionCheckSignal:
		var p tunnel.CheckSignalPayload
		if err := json.Unmarshal(task.Payload, &p); err != nil {
			return "", fmt.Errorf("parse CHECK_SIGNAL payload: %w", err)
		}
		if p.ICCID == "" {
			return "", fmt.Errorf("payload.iccid is required")
		}
		return p.ICCID, nil

	case tunnel.ActionDeleteAllSMS:
		var p tunnel.DeleteAllSMSPayload
		if err := json.Unmarshal(task.Payload, &p); err != nil {
			return "", fmt.Errorf("parse DELETE_ALL_SMS payload: %w", err)
		}
		if p.ICCID == "" {
			return "", fmt.Errorf("payload.iccid is required")
		}
		return p.ICCID, nil

	default:
		return "", fmt.Errorf("unsupported action: %q", task.Action)
	}
}
