// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package tunnel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ── NewEnvelope ───────────────────────────────────────────────────────────

func TestNewEnvelope_Fields(t *testing.T) {
	before := time.Now().UnixMilli()
	env := NewEnvelope(TypeHeartbeat)
	after := time.Now().UnixMilli()

	if env.Type != TypeHeartbeat {
		t.Errorf("Type: got %q, want %q", env.Type, TypeHeartbeat)
	}
	if env.MessageID == "" {
		t.Error("MessageID should not be empty")
	}
	// UUID v4 format: 8-4-4-4-12
	parts := strings.Split(env.MessageID, "-")
	if len(parts) != 5 {
		t.Errorf("MessageID %q is not UUID format", env.MessageID)
	}
	if env.TS < before || env.TS > after {
		t.Errorf("TS=%d not in [%d, %d]", env.TS, before, after)
	}
}

func TestNewEnvelope_UniqueIDs(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		env := NewEnvelope(TypeSMSReceived)
		if ids[env.MessageID] {
			t.Fatalf("duplicate MessageID at iteration %d: %q", i, env.MessageID)
		}
		ids[env.MessageID] = true
	}
}

func TestNewEnvelopeFrom_PreservesID(t *testing.T) {
	original := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	env := NewEnvelopeFrom(TypeTaskAck, original)
	if env.MessageID != original {
		t.Errorf("MessageID: got %q, want %q", env.MessageID, original)
	}
	if env.Type != TypeTaskAck {
		t.Errorf("Type: got %q, want %q", env.Type, TypeTaskAck)
	}
}

// ── JSON round-trips ──────────────────────────────────────────────────────

func TestSMSReceivedEvent_Marshal(t *testing.T) {
	evt := SMSReceivedEvent{
		Envelope:   NewEnvelope(TypeSMSReceived),
		GatewayID:  "gw-bavaria-01",
		ICCID:      "89490200001234567890",
		Sender:     "+4915198765432",
		Body:       "Your OTP is 391827",
		ReceivedAt: 1712345679050,
		SMSC:       "+491722270333",
		PDUHash:    "sha256:a3f100000000000000000000000000000000000000000000000000000000",
		BufferID:   42,
	}

	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got SMSReceivedEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	checks := []struct{ name, got, want string }{
		{"Type", got.Type, TypeSMSReceived},
		{"GatewayID", got.GatewayID, "gw-bavaria-01"},
		{"ICCID", got.ICCID, "89490200001234567890"},
		{"Sender", got.Sender, "+4915198765432"},
		{"Body", got.Body, "Your OTP is 391827"},
		{"SMSC", got.SMSC, "+491722270333"},
		{"PDUHash", got.PDUHash, evt.PDUHash},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
	if got.BufferID != 42 {
		t.Errorf("BufferID: got %d, want 42", got.BufferID)
	}
	if got.ReceivedAt != 1712345679050 {
		t.Errorf("ReceivedAt: got %d", got.ReceivedAt)
	}
}

func TestSMSReceivedEvent_OmitsSMSCWhenEmpty(t *testing.T) {
	evt := SMSReceivedEvent{
		Envelope: NewEnvelope(TypeSMSReceived),
		// SMSC intentionally left empty
	}
	b, _ := json.Marshal(evt)
	if strings.Contains(string(b), `"smsc"`) {
		t.Error("empty SMSC field should be omitted from JSON (omitempty)")
	}
}

func TestTaskAckEvent_Success_Marshal(t *testing.T) {
	resultJSON := json.RawMessage(`{"rssi":-71,"reg_status":"REGISTERED_HOME"}`)
	ack := TaskAckEvent{
		Envelope: NewEnvelopeFrom(TypeTaskAck, "orig-msg-id"),
		Status:   StatusSuccess,
		Result:   resultJSON,
	}

	b, err := json.Marshal(ack)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got TaskAckEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Status != StatusSuccess {
		t.Errorf("Status: got %q", got.Status)
	}
	if got.Error != nil {
		t.Errorf("Error should be nil for SUCCESS, got %+v", got.Error)
	}
	if string(got.Result) != string(resultJSON) {
		t.Errorf("Result: got %s, want %s", got.Result, resultJSON)
	}
}

func TestTaskAckEvent_Failed_Marshal(t *testing.T) {
	ack := TaskAckEvent{
		Envelope: NewEnvelopeFrom(TypeTaskAck, "orig-msg-id"),
		Status:   StatusFailed,
		Error: &TaskError{
			Code:    ErrCodeModemNotFound,
			Message: "ICCID not in registry",
		},
	}

	b, err := json.Marshal(ack)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got TaskAckEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Status != StatusFailed {
		t.Errorf("Status: got %q", got.Status)
	}
	if got.Error == nil {
		t.Fatal("Error should not be nil for FAILED")
	}
	if got.Error.Code != ErrCodeModemNotFound {
		t.Errorf("Error.Code: got %q", got.Error.Code)
	}
}

func TestModemAlertEvent_Marshal(t *testing.T) {
	evt := ModemAlertEvent{
		Envelope:  NewEnvelope(TypeModemAlert),
		ICCID:     "89490200001234567890",
		AlertCode: AlertSIMBanned,
		Detail:    "+CREG: 3 (Registration denied)",
	}

	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got ModemAlertEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.AlertCode != AlertSIMBanned {
		t.Errorf("AlertCode: got %q", got.AlertCode)
	}
	if got.Detail != evt.Detail {
		t.Errorf("Detail: got %q", got.Detail)
	}
}

func TestHeartbeatEvent_Marshal(t *testing.T) {
	evt := HeartbeatEvent{
		Envelope:    NewEnvelope(TypeHeartbeat),
		GatewayID:   "gw-bavaria-01",
		UptimeS:     86412,
		PendingMsgs: 3,
		Modems: []ModemStatus{
			{
				ICCID:      "89490200001234567890",
				Port:       "/dev/ttyUSB0",
				State:      "ACTIVE",
				SignalRSSI: -71,
				RegStatus:  "REGISTERED_HOME",
				Sent1H:     4,
				Recv1H:     12,
			},
		},
	}

	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got HeartbeatEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.UptimeS != 86412 {
		t.Errorf("UptimeS: got %d", got.UptimeS)
	}
	if got.PendingMsgs != 3 {
		t.Errorf("PendingMsgs: got %d", got.PendingMsgs)
	}
	if len(got.Modems) != 1 {
		t.Fatalf("Modems: got %d", len(got.Modems))
	}
	if got.Modems[0].SignalRSSI != -71 {
		t.Errorf("SignalRSSI: got %d", got.Modems[0].SignalRSSI)
	}
}

func TestTask_Unmarshal_SendSMS(t *testing.T) {
	raw := `{
		"type":       "TASK",
		"message_id": "a1b2c3d4-0000-0000-0000-000000000000",
		"ts":         1712345678000,
		"action":     "SEND_SMS",
		"payload": {
			"iccid":    "89490200001234567890",
			"to":       "+4915112345678",
			"body":     "Your OTP is 391827",
			"encoding": "GSM7"
		}
	}`

	var task Task
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatalf("Unmarshal Task: %v", err)
	}
	if task.Action != ActionSendSMS {
		t.Errorf("Action: got %q", task.Action)
	}

	var payload SendSMSPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if payload.ICCID != "89490200001234567890" {
		t.Errorf("ICCID: got %q", payload.ICCID)
	}
	if payload.To != "+4915112345678" {
		t.Errorf("To: got %q", payload.To)
	}
	if payload.Encoding != "GSM7" {
		t.Errorf("Encoding: got %q", payload.Encoding)
	}
}

func TestSMSDeliveredAck_Unmarshal(t *testing.T) {
	raw := `{
		"type":       "SMS_DELIVERED_ACK",
		"message_id": "f3e2d1c0-0000-0000-0000-000000000000",
		"ts":         1712345679500,
		"buffer_id":  42
	}`

	var ack SMSDeliveredAck
	if err := json.Unmarshal([]byte(raw), &ack); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ack.BufferID != 42 {
		t.Errorf("BufferID: got %d, want 42", ack.BufferID)
	}
	if ack.Type != TypeSMSDeliveredAck {
		t.Errorf("Type: got %q", ack.Type)
	}
}

// ── RegStatusString ───────────────────────────────────────────────────────

func TestRegStatusString(t *testing.T) {
	cases := []struct {
		stat int
		want string
	}{
		{0, "NOT_REGISTERED"},
		{1, "REGISTERED_HOME"},
		{2, "SEARCHING"},
		{3, "REGISTRATION_DENIED"},
		{5, "REGISTERED_ROAMING"},
		{99, "UNKNOWN"},
	}
	for _, tc := range cases {
		got := RegStatusString(tc.stat)
		if got != tc.want {
			t.Errorf("RegStatusString(%d) = %q, want %q", tc.stat, got, tc.want)
		}
	}
}
