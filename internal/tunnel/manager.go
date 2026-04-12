// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package tunnel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/signalroute/sms-gate/internal/buffer"
	"github.com/signalroute/sms-gate/internal/metrics"
	"github.com/signalroute/sms-gate/internal/safe"
)

// NewEnvelope creates a new message envelope with a fresh UUID and current timestamp.
func NewEnvelope(msgType string) Envelope {
	return Envelope{
		Type:      msgType,
		MessageID: uuid.New().String(),
		TS:        time.Now().UnixMilli(),
	}
}

// NewEnvelopeFrom creates an envelope that re-uses the original message_id
// (for ACK correlation).
func NewEnvelopeFrom(msgType, originalID string) Envelope {
	return Envelope{
		Type:      msgType,
		MessageID: originalID,
		TS:        time.Now().UnixMilli(),
	}
}

// ── Tunnel FSM states ─────────────────────────────────────────────────────

type TunnelState int32

const (
	TunnelDisconnected TunnelState = iota
	TunnelConnecting
	TunnelConnected
	TunnelBackingOff
	TunnelReconnecting
)

func (s TunnelState) String() string {
	switch s {
	case TunnelDisconnected:
		return "DISCONNECTED"
	case TunnelConnecting:
		return "CONNECTING"
	case TunnelConnected:
		return "CONNECTED"
	case TunnelBackingOff:
		return "BACKING_OFF"
	case TunnelReconnecting:
		return "RECONNECTING"
	}
	return "UNKNOWN"
}

// ── Manager config ────────────────────────────────────────────────────────

type ManagerConfig struct {
	GatewayID        string
	AgentVersion     string
	URL              string
	Token            string
	PingInterval     time.Duration
	PingTimeout      time.Duration
	HeartbeatInterval time.Duration
	ACKTimeout       time.Duration
	ReconnectBase    time.Duration
	ReconnectMax     time.Duration
	Buf              *buffer.Buffer
	RetentionDays    int
	FlushInterval    time.Duration
	StatusFn         func() []ModemStatus // called for HELLO and heartbeats
	Logger           *slog.Logger
	Metrics          *metrics.Gateway
	// TLSSkipVerify disables server certificate verification.
	// INSECURE — only set this in local development environments.
	// The gateway logs a prominent warning when this is true (#177).
	TLSSkipVerify bool
}

// Manager manages the WebSocket tunnel lifecycle.
type Manager struct {
	cfg   ManagerConfig
	log   *slog.Logger

	state     atomic.Int32
	attempt   atomic.Int64
	startTime time.Time

	// Outbox: events pushed by modem workers.
	outbox chan any

	// ACK tracking: buffer_id → ack channel.
	ackMu   sync.Mutex
	pending map[int64]chan struct{}

	// Inject event for routing (Task Router sets this).
	InboundTaskFn func(task Task) error
}

// NewManager creates a Tunnel Manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		cfg:       cfg,
		log:       cfg.Logger.With("component", "tunnel"),
		outbox:    make(chan any, 256),
		pending:   make(map[int64]chan struct{}),
		startTime: time.Now(),
	}
}

// Push enqueues an event for delivery to the Cloud Server.
// Called by modem workers and the router.
func (m *Manager) Push(evt any) {
	select {
	case m.outbox <- evt:
	default:
		m.log.Warn("outbox full, dropping event")
	}
}

// AckDelivered marks a buffer row as delivered when the cloud ACKs it.
func (m *Manager) AckDelivered(bufferID int64) {
	m.ackMu.Lock()
	ch, ok := m.pending[bufferID]
	if ok {
		delete(m.pending, bufferID)
	}
	m.ackMu.Unlock()
	if ok {
		close(ch)
	}
	// Update SQLite.
	if err := m.cfg.Buf.MarkDelivered(bufferID); err != nil {
		m.log.Error("mark delivered failed", "buffer_id", bufferID, "err", err)
	}
}

// Run is the Manager's main loop. It connects, reconnects, and drives all I/O.
func (m *Manager) Run(ctx context.Context) {
	m.state.Store(int32(TunnelConnecting))

	for {
		select {
		case <-ctx.Done():
			m.state.Store(int32(TunnelDisconnected))
			return
		default:
		}

		conn, err := m.dial(ctx)
		if err != nil {
			m.log.Warn("dial failed", "attempt", m.attempt.Load(), "err", err)
			m.metrics().TunnelReconnectsTotal.Inc()
			m.backOff(ctx)
			continue
		}

		m.state.Store(int32(TunnelConnected))
		m.attempt.Store(0)
		m.metrics().TunnelState.Set(1)
		m.log.Info("tunnel connected")

		m.runSession(ctx, conn)

		m.state.Store(int32(TunnelReconnecting))
		m.metrics().TunnelState.Set(0)
		m.log.Info("tunnel session ended, reconnecting")
		m.backOff(ctx)
	}
}

// ── Dialling ──────────────────────────────────────────────────────────────

func (m *Manager) dial(ctx context.Context) (*websocket.Conn, error) {
	m.state.Store(int32(TunnelConnecting))

	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+m.cfg.Token)
	hdr.Set("X-Gateway-ID", m.cfg.GatewayID)
	hdr.Set("X-Protocol-Version", fmt.Sprintf("%d", ProtocolVersion))

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	if m.cfg.TLSSkipVerify {
		// Log a loud warning so it's impossible to miss in production logs.
		m.log.Warn("⚠ TLS verification disabled — INSECURE, do NOT use in production",
			"setting", "tls_skip_verify=true",
		)
		dialer.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // intentional dev-only opt-in
		}
	}

	conn, resp, err := dialer.DialContext(ctx, m.cfg.URL, hdr)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			m.log.Error("authentication rejected — check gateway token", "status", resp.StatusCode)
			// Don't retry immediately; use a very long backoff.
			time.Sleep(30 * time.Second)
		}
		return nil, err
	}
	return conn, nil
}

// ── Session ───────────────────────────────────────────────────────────────

func (m *Manager) runSession(ctx context.Context, conn *websocket.Conn) {
	defer conn.Close()

	// Send HELLO.
	if err := m.sendHello(conn); err != nil {
		m.log.Error("failed to send HELLO", "err", err)
		return
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	// Writer goroutine.
	wg.Add(1)
	safe.GoWithWaitGroup(m.log, "tunnel-writer", &wg, func() {
		m.writer(sessionCtx, conn, cancel)
	})

	// Reader goroutine.
	wg.Add(1)
	safe.GoWithWaitGroup(m.log, "tunnel-reader", &wg, func() {
		m.reader(sessionCtx, conn, cancel)
	})

	// Flush offline buffer on connect.
	safe.Go(m.log, "tunnel-flush-on-connect", func() { m.flushBuffer(sessionCtx, conn) })

	// Periodic flush (defensive replay for missed ACKs).
	safe.Go(m.log, "tunnel-periodic-flush", func() {
		ticker := time.NewTicker(m.cfg.FlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				m.flushBuffer(sessionCtx, conn)
			}
		}
	})

	// Purge old delivered rows periodically.
	safe.Go(m.log, "tunnel-purge", func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				_, _ = m.cfg.Buf.Purge(m.cfg.RetentionDays)
			}
		}
	})

	wg.Wait()
}

// ── Writer goroutine ──────────────────────────────────────────────────────

func (m *Manager) writer(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) {
	pingTicker := time.NewTicker(m.cfg.PingInterval)
	heartbeatTicker := time.NewTicker(m.cfg.HeartbeatInterval)
	defer pingTicker.Stop()
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case evt := <-m.outbox:
			if err := m.writeJSON(conn, evt); err != nil {
				m.log.Error("write event failed", "err", err)
				cancel()
				return
			}

		case <-pingTicker.C:
			deadline := time.Now().Add(m.cfg.PingTimeout)
			conn.SetReadDeadline(deadline)
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				m.log.Error("ping failed", "err", err)
				cancel()
				return
			}

		case <-heartbeatTicker.C:
			if err := m.sendHeartbeat(conn); err != nil {
				m.log.Error("heartbeat failed", "err", err)
				cancel()
				return
			}
		}
	}
}

// ── Reader goroutine ──────────────────────────────────────────────────────

func (m *Manager) reader(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) {
	conn.SetPongHandler(func(data string) error {
		// Reset read deadline on pong receipt.
		conn.SetReadDeadline(time.Now().Add(m.cfg.PingInterval + m.cfg.PingTimeout))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.log.Error("read error", "err", err)
			cancel()
			return
		}

		if err := m.handleInbound(msg); err != nil {
			m.log.Warn("inbound message error", "err", err)
		}
	}
}

func (m *Manager) handleInbound(raw []byte) error {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("unmarshal envelope: %w", err)
	}

	switch env.Type {
	case TypeSMSDeliveredAck:
		var ack SMSDeliveredAck
		if err := json.Unmarshal(raw, &ack); err != nil {
			return fmt.Errorf("unmarshal SMS_DELIVERED_ACK: %w", err)
		}
		m.AckDelivered(ack.BufferID)

	case TypeTask:
		var task Task
		if err := json.Unmarshal(raw, &task); err != nil {
			return fmt.Errorf("unmarshal task: %w", err)
		}
		if m.InboundTaskFn != nil {
			if err := m.InboundTaskFn(task); err != nil {
				m.log.Warn("task routing failed", "task_id", task.MessageID, "err", err)
				// Push a TASK_ACK FAILED for the routing error.
				m.Push(m.makeErrorAck(task.MessageID, err))
			}
		}

	default:
		m.log.Warn("unknown inbound message type", "type", env.Type)
	}
	return nil
}

// ── HELLO / Heartbeat ─────────────────────────────────────────────────────

func (m *Manager) sendHello(conn *websocket.Conn) error {
	evt := HelloEvent{
		Envelope:        NewEnvelope(TypeHello),
		GatewayID:       m.cfg.GatewayID,
		ProtocolVersion: ProtocolVersion,
		AgentVersion:    m.cfg.AgentVersion,
		Modems:          m.cfg.StatusFn(),
	}
	return m.writeJSON(conn, evt)
}

func (m *Manager) sendHeartbeat(conn *websocket.Conn) error {
	pending, _ := m.cfg.Buf.PendingCount()
	m.metrics().SMSPendingCount.Set(float64(pending))

	evt := HeartbeatEvent{
		Envelope:    NewEnvelope(TypeHeartbeat),
		GatewayID:   m.cfg.GatewayID,
		UptimeS:     int64(time.Since(m.startTime).Seconds()),
		PendingMsgs: pending,
		Modems:      m.cfg.StatusFn(),
	}
	return m.writeJSON(conn, evt)
}

// ── Offline buffer flush ──────────────────────────────────────────────────

func (m *Manager) flushBuffer(ctx context.Context, conn *websocket.Conn) {
	rows, err := m.cfg.Buf.PendingRows()
	if err != nil {
		m.log.Error("flush: query pending rows", "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	m.log.Info("flushing offline buffer", "count", len(rows))

	for _, row := range rows {
		select {
		case <-ctx.Done():
			return
		default:
		}

		evt := SMSReceivedEvent{
			Envelope:   NewEnvelope(TypeSMSReceived),
			GatewayID:  m.cfg.GatewayID,
			ICCID:      row.ICCID,
			Sender:     row.Sender,
			Body:       row.Body,
			ReceivedAt: row.ReceivedAt,
			SMSC:       row.SMSC,
			PDUHash:    row.PDUHash,
			BufferID:   row.ID,
		}

		if err := m.writeJSON(conn, evt); err != nil {
			m.log.Error("flush: write event failed", "buffer_id", row.ID, "err", err)
			return
		}

		// Track pending ACK.
		ackCh := make(chan struct{}, 1)
		m.ackMu.Lock()
		m.pending[row.ID] = ackCh
		m.ackMu.Unlock()

		// Wait for ACK up to ACKTimeout.
		select {
		case <-ackCh:
			m.metrics().SMSDelivered.WithLabelValues(row.ICCID).Inc()
		case <-time.After(m.cfg.ACKTimeout):
			m.log.Warn("flush: ACK timeout", "buffer_id", row.ID)
			m.ackMu.Lock()
			delete(m.pending, row.ID)
			m.ackMu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

// ── Backoff ───────────────────────────────────────────────────────────────

func (m *Manager) backOff(ctx context.Context) {
	attempt := m.attempt.Add(1)
	m.state.Store(int32(TunnelBackingOff))

	base := m.cfg.ReconnectBase.Seconds()
	maxDelay := m.cfg.ReconnectMax.Seconds()
	delay := math.Min(base*math.Pow(2, float64(attempt-1)), maxDelay)
	jitter := delay * 0.3 * rand.Float64()
	total := time.Duration((delay + jitter) * float64(time.Second))

	if attempt > 20 {
		m.log.Error("tunnel: reconnection attempt high, backing off", "attempt", attempt, "delay", total)
	} else {
		m.log.Info("tunnel: backing off", "attempt", attempt, "delay", total)
	}

	select {
	case <-time.After(total):
	case <-ctx.Done():
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────

func (m *Manager) writeJSON(conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return conn.WriteMessage(websocket.TextMessage, b)
}

func (m *Manager) makeErrorAck(taskID string, err error) TaskAckEvent {
	code := ErrCodeModemNotFound
	if err.Error() == ErrModemNotFoundMsg {
		code = ErrCodeModemNotFound
	}
	return TaskAckEvent{
		Envelope: NewEnvelopeFrom(TypeTaskAck, taskID),
		Status:   StatusFailed,
		Error:    &TaskError{Code: code, Message: err.Error()},
	}
}

const ErrModemNotFoundMsg = "modem not found: ICCID not in registry"

func (m *Manager) metrics() *metrics.Gateway {
	return m.cfg.Metrics
}
