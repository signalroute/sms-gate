// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package tunnel implements the WebSocket tunnel protocol (§3 of the spec).
package tunnel

import "encoding/json"

// ProtocolVersion is the current wire protocol version.
const ProtocolVersion = 2

// ── Message type constants ─────────────────────────────────────────────────

const (
	TypeHello          = "HELLO"
	TypeTask           = "TASK"
	TypeSMSReceived    = "SMS_RECEIVED"
	TypeSMSDeliveredAck = "SMS_DELIVERED_ACK"
	TypeTaskAck        = "TASK_ACK"
	TypeModemAlert     = "MODEM_ALERT"
	TypeHeartbeat      = "HEARTBEAT"
)

// ── Task actions ───────────────────────────────────────────────────────────

const (
	ActionSendSMS     = "SEND_SMS"
	ActionRebootModem = "REBOOT_MODEM"
	ActionCheckSignal = "CHECK_SIGNAL"
	ActionDeleteAllSMS = "DELETE_ALL_SMS"
)

// ── Alert codes ───────────────────────────────────────────────────────────

const (
	AlertSIMBanned     = "SIM_BANNED"
	AlertSIMFull       = "SIM_FULL"
	AlertModemHang     = "MODEM_HANG"
	AlertModemRemoved  = "MODEM_REMOVED"
	AlertSignalLost    = "SIGNAL_LOST"
)

// ── Task status ───────────────────────────────────────────────────────────

const (
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
)

// ── Error codes ───────────────────────────────────────────────────────────

const (
	ErrCodeModemNotFound    = "MODEM_NOT_FOUND"
	ErrCodeModemBusy        = "MODEM_BUSY"
	ErrCodeModemUnresponsive = "MODEM_UNRESPONSIVE"
	ErrCodeSIMBanned        = "SIM_BANNED"
	ErrCodeSIMFull          = "SIM_FULL"
	ErrCodeSendFailed       = "SEND_FAILED"
	ErrCodeRateLimited      = "RATE_LIMITED"
	ErrCodeInvalidPayload   = "INVALID_PAYLOAD"
	ErrCodeUnsupportedAction = "UNSUPPORTED_ACTION"
)

// ── Envelope ──────────────────────────────────────────────────────────────

// Envelope is the minimal top-level fields every message must contain.
type Envelope struct {
	Type      string `json:"type"`
	MessageID string `json:"message_id"`
	TS        int64  `json:"ts"`
}

// ── Inbound: Tasks (Cloud → Gateway) ─────────────────────────────────────

// Task is received from the Cloud Server.
type Task struct {
	Envelope
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload"`
}

// SendSMSPayload is the payload for ActionSendSMS.
type SendSMSPayload struct {
	ICCID    string `json:"iccid"`
	To       string `json:"to"`
	Body     string `json:"body"`
	Encoding string `json:"encoding"` // "GSM7" | "UCS2" | ""
}

// RebootModemPayload is the payload for ActionRebootModem.
type RebootModemPayload struct {
	ICCID string `json:"iccid"`
	Hard  bool   `json:"hard"`
}

// CheckSignalPayload is the payload for ActionCheckSignal.
type CheckSignalPayload struct {
	ICCID string `json:"iccid"`
}

// DeleteAllSMSPayload is the payload for ActionDeleteAllSMS.
type DeleteAllSMSPayload struct {
	ICCID string `json:"iccid"`
}

// ── Outbound: Events (Gateway → Cloud Server) ─────────────────────────────

// HelloEvent is sent immediately after WebSocket upgrade.
type HelloEvent struct {
	Envelope
	GatewayID       string        `json:"gateway_id"`
	ProtocolVersion int           `json:"protocol_version"`
	AgentVersion    string        `json:"agent_version"`
	Modems          []ModemStatus `json:"modems"`
}

// ModemStatus is used in HelloEvent and HeartbeatEvent.
type ModemStatus struct {
	ICCID      string `json:"iccid"`
	Port       string `json:"port"`
	State      string `json:"state"`
	IMSI       string `json:"imsi,omitempty"`
	Operator   string `json:"operator,omitempty"`
	SignalRSSI int    `json:"signal_rssi"`
	RegStatus  string `json:"reg_status"`
	Sent1H     int64  `json:"sent_1h"`
	Recv1H     int64  `json:"recv_1h"`
}

// SMSReceivedEvent is emitted when a modem receives an SMS.
type SMSReceivedEvent struct {
	Envelope
	GatewayID  string `json:"gateway_id"`
	ICCID      string `json:"iccid"`
	Sender     string `json:"sender"`
	Body       string `json:"body"`
	ReceivedAt int64  `json:"received_at"`
	SMSC       string `json:"smsc,omitempty"`
	PDUHash    string `json:"pdu_hash"`
	BufferID   int64  `json:"buffer_id"`
}

// SMSDeliveredAck is received from the Cloud Server acknowledging an SMSReceivedEvent.
type SMSDeliveredAck struct {
	Envelope
	BufferID int64 `json:"buffer_id"`
}

// TaskAckEvent reports the result of a Task to the Cloud Server.
type TaskAckEvent struct {
	Envelope
	Status string          `json:"status"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *TaskError      `json:"error,omitempty"`
}

// TaskError carries error details in a failed TaskAckEvent.
type TaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ModemAlertEvent is pushed when a critical modem condition is detected.
type ModemAlertEvent struct {
	Envelope
	ICCID     string `json:"iccid"`
	AlertCode string `json:"alert_code"`
	Detail    string `json:"detail,omitempty"`
}

// HeartbeatEvent is pushed every HeartbeatInterval seconds.
type HeartbeatEvent struct {
	Envelope
	GatewayID   string        `json:"gateway_id"`
	UptimeS     int64         `json:"uptime_s"`
	PendingMsgs int           `json:"pending_msgs"`
	Modems      []ModemStatus `json:"modems"`
}

// CheckSignalResult is the result field in a successful CHECK_SIGNAL TaskAck.
type CheckSignalResult struct {
	RSSI      int    `json:"rssi"`
	RegStatus string `json:"reg_status"`
}

// regStatusString converts a +CREG stat integer to a human-readable string.
func RegStatusString(stat int) string {
	switch stat {
	case 0:
		return "NOT_REGISTERED"
	case 1:
		return "REGISTERED_HOME"
	case 2:
		return "SEARCHING"
	case 3:
		return "REGISTRATION_DENIED"
	case 5:
		return "REGISTERED_ROAMING"
	default:
		return "UNKNOWN"
	}
}
