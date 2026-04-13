// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package tunnel

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/signalroute/sms-gate/internal/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

func newTestManagerConfig() ManagerConfig {
	m := metrics.New(prometheus.NewRegistry())
	return ManagerConfig{
		GatewayID: "gw-test",
		URL:       "wss://example.com/ws",
		Token:     "test-token",
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Metrics:   m,
	}
}

// ── TestEnvelope_InvalidJSON (#110) ───────────────────────────────────────

func TestEnvelope_InvalidJSON(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"garbage", "not-json{"},
		{"array", "[]"},
		{"number", "42"},
		{"incomplete", `{"type":"sms","message_id":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var msg Envelope
			err := json.Unmarshal([]byte(tc.data), &msg)
			if err == nil && tc.data != "null" && tc.data != "{}" {
				if msg.Type != "" {
					t.Errorf("expected empty type, got %q", msg.Type)
				}
			}
		})
	}
}

// ── TestSendSMSResult_MultiPart (#115) ────────────────────────────────────

func TestSendSMSResult_MultiPart(t *testing.T) {
	result := SendSMSResult{
		Parts:     3,
		SignalCSQ: 22,
		RegStatus: "REGISTERED_HOME",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SendSMSResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Parts != 3 {
		t.Errorf("parts: got %d, want 3", decoded.Parts)
	}
	if decoded.SignalCSQ != 22 {
		t.Errorf("csq: got %d, want 22", decoded.SignalCSQ)
	}
	if decoded.RegStatus != "REGISTERED_HOME" {
		t.Errorf("reg_status: got %q", decoded.RegStatus)
	}
}

// ── TestOutboxFull (#32) ──────────────────────────────────────────────────

func TestOutboxFull(t *testing.T) {
	cfg := newTestManagerConfig()
	m := NewManager(cfg)

	// Fill outbox to capacity (256).
	for i := 0; i < 256; i++ {
		m.Push(HeartbeatEvent{Envelope: NewEnvelope(TypeHeartbeat)})
	}

	if m.OutboxLen() != 256 {
		t.Errorf("outbox should be at capacity: got %d", m.OutboxLen())
	}

	// Next push should be dropped (no panic, no block).
	m.Push(HeartbeatEvent{Envelope: NewEnvelope(TypeHeartbeat)})
	if m.OutboxLen() != 256 {
		t.Errorf("outbox should still be 256 after drop: got %d", m.OutboxLen())
	}
}

// ── TestOutboxLen (#60) ──────────────────────────────────────────────────

func TestOutboxLen(t *testing.T) {
	cfg := newTestManagerConfig()
	m := NewManager(cfg)

	for i := 0; i < 25; i++ {
		m.Push(HeartbeatEvent{Envelope: NewEnvelope(TypeHeartbeat)})
	}

	depth := m.OutboxLen()
	if depth != 25 {
		t.Errorf("outbox depth: got %d, want 25", depth)
	}
}

// ── TestEnvelope_AllTypes ─────────────────────────────────────────────────

func TestEnvelope_AllTypes(t *testing.T) {
	types := []string{
		TypeHello, TypeHeartbeat, TypeSMSReceived,
		TypeTaskAck, TypeModemAlert,
	}
	for _, typ := range types {
		t.Run(typ, func(t *testing.T) {
			env := NewEnvelope(typ)
			data, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded Envelope
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if decoded.Type != typ {
				t.Errorf("type: got %q, want %q", decoded.Type, typ)
			}
			if decoded.MessageID == "" {
				t.Error("message_id should not be empty")
			}
		})
	}
}

// ── TestTaskAckEvent_Success_Roundtrip ────────────────────────────────────

func TestTaskAckEvent_Success_Roundtrip(t *testing.T) {
	result, _ := json.Marshal(SendSMSResult{Parts: 2, SignalCSQ: 15, RegStatus: "REGISTERED_HOME"})
	ack := TaskAckEvent{
		Envelope: NewEnvelope(TypeTaskAck),
		Status:   "ok",
		Result:   result,
	}
	data, err := json.Marshal(ack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded TaskAckEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Status != "ok" {
		t.Errorf("status: got %q", decoded.Status)
	}
}

// ── TestTaskAckEvent_ErrorField ───────────────────────────────────────────

func TestTaskAckEvent_ErrorField(t *testing.T) {
	ack := TaskAckEvent{
		Envelope: NewEnvelope(TypeTaskAck),
		Status:   "error",
		Error:    &TaskError{Code: "MODEM_BUSY", Message: "task queue full"},
	}
	data, err := json.Marshal(ack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded TaskAckEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Error == nil {
		t.Fatal("error field should be present")
	}
	if decoded.Error.Code != "MODEM_BUSY" {
		t.Errorf("error code: got %q", decoded.Error.Code)
	}
}

// ── TestSendSMSPayload_Roundtrip ─────────────────────────────────────────

func TestSendSMSPayload_Roundtrip(t *testing.T) {
	p := SendSMSPayload{
		ICCID:    "89490200001234567890",
		To:       "+491234567890",
		Body:     "Hello, World!",
		Encoding: "GSM7",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SendSMSPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ICCID != p.ICCID {
		t.Errorf("iccid: got %q", decoded.ICCID)
	}
	if decoded.Body != p.Body {
		t.Errorf("body: got %q", decoded.Body)
	}
}

// ── TestRegStatusString_Extended ─────────────────────────────────────────

func TestRegStatusString_Extended(t *testing.T) {
	cases := []struct {
		stat int
		want string
	}{
		{0, "NOT_REGISTERED"},
		{1, "REGISTERED_HOME"},
		{2, "SEARCHING"},
		{3, "REGISTRATION_DENIED"},
		{4, "UNKNOWN"},
		{5, "REGISTERED_ROAMING"},
		{99, "UNKNOWN"},
		{-1, "UNKNOWN"},
	}
	for _, tc := range cases {
		got := RegStatusString(tc.stat)
		if got != tc.want {
			t.Errorf("RegStatusString(%d): got %q, want %q", tc.stat, got, tc.want)
		}
	}
}

// ── TestModemAlertEvent_Roundtrip ────────────────────────────────────────

func TestModemAlertEvent_Roundtrip(t *testing.T) {
	alert := ModemAlertEvent{
		Envelope:  NewEnvelope(TypeModemAlert),
		ICCID:     "89490200001234567890",
		AlertCode: "MODEM_BANNED",
		Detail:    "exceeded retry threshold",
	}
	data, err := json.Marshal(alert)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ModemAlertEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.AlertCode != "MODEM_BANNED" {
		t.Errorf("alert_code: got %q", decoded.AlertCode)
	}
}

// ── TestCheckSignalResult_Roundtrip ──────────────────────────────────────

func TestCheckSignalResult_Roundtrip(t *testing.T) {
	r := CheckSignalResult{
		RSSI:      18,
		RegStatus: "REGISTERED_HOME",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded CheckSignalResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.RSSI != 18 {
		t.Errorf("rssi: got %d", decoded.RSSI)
	}
}

// ── TestMaxPendingACKs_Constant ──────────────────────────────────────────

func TestMaxPendingACKs_Constant(t *testing.T) {
	if MaxPendingACKs <= 0 {
		t.Errorf("MaxPendingACKs should be positive: %d", MaxPendingACKs)
	}
	if MaxPendingACKs != 1000 {
		t.Errorf("MaxPendingACKs: got %d, want 1000", MaxPendingACKs)
	}
}
