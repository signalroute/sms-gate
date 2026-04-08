// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

package modem

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/yanujz/go-sms-gate/internal/at"
	"github.com/yanujz/go-sms-gate/internal/buffer"
	"github.com/yanujz/go-sms-gate/internal/metrics"
	"github.com/yanujz/go-sms-gate/internal/tunnel"

	"go.bug.st/serial"
)

// ── Worker state ──────────────────────────────────────────────────────────

type State int32

const (
	StateInitializing State = iota
	StateActive
	StateExecuting
	StateRecovering
	StateResetting
	StateBanned
	StateFailed
)

func (s State) String() string {
	switch s {
	case StateInitializing:
		return "INITIALIZING"
	case StateActive:
		return "ACTIVE"
	case StateExecuting:
		return "EXECUTING"
	case StateRecovering:
		return "RECOVERING"
	case StateResetting:
		return "RESETTING"
	case StateBanned:
		return "BANNED"
	case StateFailed:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

// ── InboundTask wraps a Task from the cloud ───────────────────────────────

// InboundTask is dispatched from the Task Router to a specific Worker.
type InboundTask struct {
	Task   tunnel.Task
	AckFn  func(ack tunnel.TaskAckEvent) // called to push TASK_ACK upstream
	AlertFn func(alert tunnel.ModemAlertEvent)
}

// ── WorkerStatus is a snapshot for heartbeats ─────────────────────────────

type WorkerStatus struct {
	ICCID      string
	Port       string
	State      string
	IMSI       string
	Operator   string
	SignalRSSI int
	RegStatus  string
	Sent1H     int64
	Recv1H     int64
}

// ── Worker ────────────────────────────────────────────────────────────────

// Worker manages one physical modem. It owns the AT Serializer for its port
// and implements the Modem State Machine from §5.1 of the spec.
type Worker struct {
	// Immutable after construction.
	port       string
	baud       int
	gatewayID  string
	iccid      string // set after init
	imsi       string
	operator   string

	// Concurrently updated.
	state      atomic.Int32
	signalRSSI atomic.Int32
	regStat    atomic.Int32
	sent1H     atomic.Int64
	recv1H     atomic.Int64

	// Channels
	taskCh  chan InboundTask // size 1 — one pending task at a time

	// Dependencies
	buf        *buffer.Buffer
	limiter    *RateLimiterRegistry
	eventFn    func(evt any) // push event upstream (SMSReceived, ModemAlert, TaskAck)
	rateCfg    RateLimitConfig

	// Health config
	keepaliveInterval    time.Duration
	simCapacityWarnPct   int
	simCapacityPurgePct  int

	logger  *slog.Logger
	metrics *metrics.Gateway
}

// WorkerConfig holds constructor parameters.
type WorkerConfig struct {
	Port                string
	Baud                int
	GatewayID           string
	Buf                 *buffer.Buffer
	Limiter             *RateLimiterRegistry
	RateConfig          RateLimitConfig
	EventFn             func(evt any)
	KeepaliveInterval   time.Duration
	SIMCapacityWarnPct  int
	SIMCapacityPurgePct int
	Logger              *slog.Logger
	Metrics             *metrics.Gateway
}

// NewWorker constructs but does not start a Worker.
func NewWorker(cfg WorkerConfig) *Worker {
	w := &Worker{
		port:                cfg.Port,
		baud:                cfg.Baud,
		gatewayID:           cfg.GatewayID,
		buf:                 cfg.Buf,
		limiter:             cfg.Limiter,
		rateCfg:             cfg.RateConfig,
		eventFn:             cfg.EventFn,
		keepaliveInterval:   cfg.KeepaliveInterval,
		simCapacityWarnPct:  cfg.SIMCapacityWarnPct,
		simCapacityPurgePct: cfg.SIMCapacityPurgePct,
		logger:              cfg.Logger,
		metrics:             cfg.Metrics,
		taskCh:              make(chan InboundTask, 1),
	}
	w.state.Store(int32(StateInitializing))
	return w
}

// ICCID returns the SIM ICCID once initialization is complete.
func (w *Worker) ICCID() string { return w.iccid }

// Status returns a snapshot of the worker's current status.
func (w *Worker) Status() WorkerStatus {
	return WorkerStatus{
		ICCID:      w.iccid,
		Port:       w.port,
		State:      State(w.state.Load()).String(),
		IMSI:       w.imsi,
		Operator:   w.operator,
		SignalRSSI: int(w.signalRSSI.Load()),
		RegStatus:  tunnel.RegStatusString(int(w.regStat.Load())),
		Sent1H:     w.sent1H.Load(),
		Recv1H:     w.recv1H.Load(),
	}
}

// Run starts the worker goroutine and blocks until ctx is cancelled or a fatal
// error occurs. Returns the final state.
func (w *Worker) Run(ctx context.Context, registry *Registry) State {
	log := w.logger.With("port", w.port, "component", "worker")

	// Open and configure serial port.
	ser, err := w.openPort()
	if err != nil {
		log.Error("failed to open serial port", "err", err)
		w.state.Store(int32(StateFailed))
		return StateFailed
	}
	defer ser.Close()

	atSer := at.NewSerializer(ser)
	defer atSer.Close()

	// ── Init sequence (§4.1.2) ───────────────────────────────────────────

	if err := w.runInitSequence(atSer, log); err != nil {
		log.Error("init sequence failed", "err", err)
		w.state.Store(int32(StateFailed))
		return StateFailed
	}

	// Register in Worker Registry.
	registry.Register(w.iccid, w)
	w.limiter.Register(w.iccid, w.rateCfg)
	log = log.With("iccid", w.iccid)
	log.Info("worker initialized and registered")

	w.state.Store(int32(StateActive))
	w.metrics.ModemState.WithLabelValues(w.iccid).Set(float64(StateActive))

	// ── Main loop ─────────────────────────────────────────────────────────

	keepaliveTicker := time.NewTicker(w.keepaliveInterval)
	capacityTicker := time.NewTicker(5 * time.Minute)
	defer keepaliveTicker.Stop()
	defer capacityTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			registry.Deregister(w.iccid)
			return StateActive

		case urc := <-atSer.URCCH:
			w.handleURC(ctx, atSer, urc, log)

		case task := <-w.taskCh:
			w.executeTask(ctx, atSer, task, registry, log)

		case <-keepaliveTicker.C:
			w.runKeepalive(atSer, registry, log)

		case <-capacityTicker.C:
			w.checkCapacity(atSer, log)
		}

		// If we entered a terminal state, exit.
		st := State(w.state.Load())
		if st == StateFailed || st == StateBanned {
			registry.Deregister(w.iccid)
			return st
		}
	}
}

// ── Serial port open ──────────────────────────────────────────────────────

func (w *Worker) openPort() (serial.Port, error) {
	mode := &serial.Mode{
		BaudRate: w.baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	for attempt := 1; attempt <= 3; attempt++ {
		p, err := serial.Open(w.port, mode)
		if err == nil {
			return p, nil
		}
		w.logger.Warn("serial open failed", "port", w.port, "attempt", attempt, "err", err)
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return nil, fmt.Errorf("could not open %s after 3 attempts", w.port)
}

// ── Init sequence (§4.1.2) ────────────────────────────────────────────────

func (w *Worker) runInitSequence(s *at.Serializer, log *slog.Logger) error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"ping", func() error { return retryN(3, func() error { return s.Ping() }) }},
		{"disable echo", func() error { return s.DisableEcho() }},
		{"PDU mode", func() error { return s.SetPDUMode() }},
		{"enable CMTI URCs", func() error { return s.EnableCMTIURCs() }},
		{"read ICCID", func() error {
			iccid, err := retryNVal(3, s.ReadICCID)
			if err != nil {
				return err
			}
			w.iccid = iccid
			return nil
		}},
		{"read IMSI", func() error {
			imsi, err := s.ReadIMSI()
			if err != nil {
				return err // non-fatal; log and continue
			}
			w.imsi = imsi
			return nil
		}},
		{"read operator", func() error {
			op, err := s.ReadOperator()
			if err != nil {
				return err
			}
			w.operator = op
			return nil
		}},
	}

	for _, step := range steps {
		log.Debug("init step", "step", step.name)
		if err := step.fn(); err != nil {
			if step.name == "read IMSI" || step.name == "read operator" {
				log.Warn("non-fatal init step failed", "step", step.name, "err", err)
				continue
			}
			return fmt.Errorf("init step %q: %w", step.name, err)
		}
	}
	return nil
}

// ── URC handling ──────────────────────────────────────────────────────────

func (w *Worker) handleURC(ctx context.Context, s *at.Serializer, urc string, log *slog.Logger) {
	switch {
	case len(urc) > 5 && urc[:6] == "+CMTI:":
		idx, err := at.ParseCMTI(urc)
		if err != nil {
			log.Warn("malformed CMTI", "urc", urc, "err", err)
			return
		}
		w.receiveSMS(ctx, s, idx, log)

	case len(urc) > 6 && urc[:6] == "+CREG:":
		stat := at.ParseCREG(urc)
		w.regStat.Store(int32(stat))
		if stat == 3 {
			w.enterBanned(s, urc, log)
		}
	}
}

// receiveSMS implements the safe-deletion ordering from §2.3.1 and §5.2.1.
func (w *Worker) receiveSMS(ctx context.Context, s *at.Serializer, index int, log *slog.Logger) {
	log = log.With("sms_index", index)

	// Step 1: Read PDU from SIM.
	pduHex, err := s.ReadSMS(index)
	if err != nil {
		log.Error("failed to read SMS", "err", err)
		return
	}

	// Step 2: Decode PDU.
	decoded, err := at.DecodePDU(pduHex)
	if err != nil {
		log.Error("failed to decode PDU", "err", err)
		// Still delete from SIM to avoid storage leak.
		_ = s.DeleteSMS(index)
		return
	}

	// Step 3: Persist to SQLite BEFORE deleting from SIM.
	id, isDuplicate, err := w.buf.Insert(
		w.iccid, decoded.Sender, decoded.Body,
		decoded.PDUHash, decoded.SMSC,
		decoded.Timestamp.UnixMilli(),
	)
	if err != nil {
		log.Error("failed to persist SMS to buffer", "err", err)
		// Do NOT delete from SIM if we can't persist — retryable.
		return
	}

	// Step 4: Delete from SIM (irreversible, happens after local persistence).
	if err := s.DeleteSMS(index); err != nil {
		log.Warn("failed to delete SMS from SIM", "err", err)
		// Not fatal; the message is safe in SQLite.
	}

	if isDuplicate {
		log.Info("duplicate SMS discarded", "pdu_hash", decoded.PDUHash)
		return
	}

	w.recv1H.Add(1)
	w.metrics.SMSReceived.WithLabelValues(w.iccid).Inc()

	log.Info("SMS received",
		"sender", decoded.Sender,
		"buffer_id", id,
		"pdu_hash", decoded.PDUHash,
	)

	// Step 5: Push event upstream via eventFn.
	w.eventFn(tunnel.SMSReceivedEvent{
		Envelope: tunnel.NewEnvelope(tunnel.TypeSMSReceived),
		GatewayID:  w.gatewayID,
		ICCID:      w.iccid,
		Sender:     decoded.Sender,
		Body:       decoded.Body,
		ReceivedAt: decoded.Timestamp.UnixMilli(),
		SMSC:       decoded.SMSC,
		PDUHash:    decoded.PDUHash,
		BufferID:   id,
	})
}

// ── Task execution ────────────────────────────────────────────────────────

func (w *Worker) executeTask(ctx context.Context, s *at.Serializer, it InboundTask, reg *Registry, log *slog.Logger) {
	task := it.Task
	log = log.With("task_id", task.MessageID, "action", task.Action)
	log.Info("executing task")

	prev := State(w.state.Load())
	w.state.Store(int32(StateExecuting))
	defer w.state.Store(int32(prev))

	var (
		resultJSON []byte
		taskErr    *tunnel.TaskError
	)

	switch task.Action {
	case tunnel.ActionSendSMS:
		resultJSON, taskErr = w.doSendSMS(s, task, log)
	case tunnel.ActionRebootModem:
		taskErr = w.doRebootModem(ctx, s, task, log)
	case tunnel.ActionCheckSignal:
		resultJSON, taskErr = w.doCheckSignal(s, log)
	case tunnel.ActionDeleteAllSMS:
		taskErr = w.doDeleteAllSMS(s, log)
	default:
		taskErr = &tunnel.TaskError{Code: tunnel.ErrCodeUnsupportedAction, Message: fmt.Sprintf("unknown action: %q", task.Action)}
	}

	status := tunnel.StatusSuccess
	if taskErr != nil {
		status = tunnel.StatusFailed
	}

	ack := tunnel.TaskAckEvent{
		Envelope: tunnel.NewEnvelopeFrom(tunnel.TypeTaskAck, task.MessageID),
		Status:   status,
		Error:    taskErr,
	}
	if resultJSON != nil {
		ack.Result = resultJSON
	}

	it.AckFn(ack)
}

func (w *Worker) doSendSMS(s *at.Serializer, task tunnel.Task, log *slog.Logger) ([]byte, *tunnel.TaskError) {
	var p tunnel.SendSMSPayload
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return nil, &tunnel.TaskError{Code: tunnel.ErrCodeInvalidPayload, Message: err.Error()}
	}
	if p.ICCID == "" || p.To == "" || p.Body == "" {
		return nil, &tunnel.TaskError{Code: tunnel.ErrCodeInvalidPayload, Message: "iccid, to, and body are required"}
	}

	if !w.limiter.Allow(w.iccid) {
		return nil, &tunnel.TaskError{Code: tunnel.ErrCodeRateLimited, Message: "per-SIM rate limit exceeded"}
	}

	parts, err := at.EncodePDU(p.To, p.Body, p.Encoding)
	if err != nil {
		return nil, &tunnel.TaskError{Code: tunnel.ErrCodeSendFailed, Message: err.Error()}
	}

	for i, part := range parts {
		_, err := s.ExecuteSend(part.HexStr, part.Length, at.TimeoutSend)
		if err != nil {
			return nil, &tunnel.TaskError{
				Code:    tunnel.ErrCodeSendFailed,
				Message: fmt.Sprintf("part %d/%d: %v", i+1, len(parts), err),
			}
		}
	}

	w.sent1H.Add(1)
	w.metrics.SMSSent.WithLabelValues(w.iccid, "success").Inc()
	log.Info("SMS sent", "to", p.To, "parts", len(parts))
	return nil, nil
}

func (w *Worker) doRebootModem(ctx context.Context, s *at.Serializer, task tunnel.Task, log *slog.Logger) *tunnel.TaskError {
	var p tunnel.RebootModemPayload
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return &tunnel.TaskError{Code: tunnel.ErrCodeInvalidPayload, Message: err.Error()}
	}

	if p.Hard {
		log.Info("hard reset requested")
		if err := s.HardReset(); err != nil {
			return &tunnel.TaskError{Code: tunnel.ErrCodeModemUnresponsive, Message: err.Error()}
		}
	} else {
		log.Info("radio cycle requested")
		_ = s.RadioOff()
		time.Sleep(2 * time.Second)
		if err := s.RadioOn(); err != nil {
			return &tunnel.TaskError{Code: tunnel.ErrCodeModemUnresponsive, Message: err.Error()}
		}
	}
	return nil
}

func (w *Worker) doCheckSignal(s *at.Serializer, log *slog.Logger) ([]byte, *tunnel.TaskError) {
	rssi, err := s.SignalQuality()
	if err != nil {
		return nil, &tunnel.TaskError{Code: tunnel.ErrCodeModemUnresponsive, Message: err.Error()}
	}
	stat, err := s.RegistrationStatus()
	if err != nil {
		return nil, &tunnel.TaskError{Code: tunnel.ErrCodeModemUnresponsive, Message: err.Error()}
	}
	w.signalRSSI.Store(int32(rssi))
	w.regStat.Store(int32(stat))
	w.metrics.ModemSignalRSSI.WithLabelValues(w.iccid).Set(float64(rssi))

	res := tunnel.CheckSignalResult{RSSI: rssi, RegStatus: tunnel.RegStatusString(stat)}
	b, _ := json.Marshal(res)
	log.Info("signal check", "rssi", rssi, "reg_status", tunnel.RegStatusString(stat))
	return b, nil
}

func (w *Worker) doDeleteAllSMS(s *at.Serializer, log *slog.Logger) *tunnel.TaskError {
	log.Warn("DELETE_ALL_SMS requested — this bypasses safe-deletion ordering")
	if err := s.DeleteAllSMS(); err != nil {
		return &tunnel.TaskError{Code: tunnel.ErrCodeSendFailed, Message: err.Error()}
	}
	return nil
}

// ── Keep-alive ────────────────────────────────────────────────────────────

func (w *Worker) runKeepalive(s *at.Serializer, reg *Registry, log *slog.Logger) {
	stat, err := s.RegistrationStatus()
	if err != nil {
		log.Warn("keepalive: registration query failed, entering recovery", "err", err)
		w.enterRecovery(s, reg, log)
		return
	}

	w.regStat.Store(int32(stat))

	rssi, err := s.SignalQuality()
	if err == nil {
		w.signalRSSI.Store(int32(rssi))
		w.metrics.ModemSignalRSSI.WithLabelValues(w.iccid).Set(float64(rssi))
	}

	if stat == 3 { // registration denied
		w.enterBanned(s, fmt.Sprintf("+CREG: %d (Registration denied)", stat), log)
	}
}

func (w *Worker) checkCapacity(s *at.Serializer, log *slog.Logger) {
	used, total, err := s.StorageStatus()
	if err != nil || total == 0 {
		return
	}
	pct := used * 100 / total
	if pct >= w.simCapacityPurgePct {
		log.Warn("SIM storage critical, autonomous purge", "used", used, "total", total, "pct", pct)
		w.eventFn(tunnel.ModemAlertEvent{
			Envelope:  tunnel.NewEnvelope(tunnel.TypeModemAlert),
			ICCID:     w.iccid,
			AlertCode: tunnel.AlertSIMFull,
			Detail:    fmt.Sprintf("SIM at %d%% capacity (%d/%d), purging", pct, used, total),
		})
		_ = s.DeleteAllSMS()
	} else if pct >= w.simCapacityWarnPct {
		log.Warn("SIM storage warning", "used", used, "total", total, "pct", pct)
		w.eventFn(tunnel.ModemAlertEvent{
			Envelope:  tunnel.NewEnvelope(tunnel.TypeModemAlert),
			ICCID:     w.iccid,
			AlertCode: tunnel.AlertSIMFull,
			Detail:    fmt.Sprintf("SIM at %d%% capacity (%d/%d)", pct, used, total),
		})
	}
}

// ── State transitions ─────────────────────────────────────────────────────

func (w *Worker) enterBanned(s *at.Serializer, detail string, log *slog.Logger) {
	log.Error("SIM BANNED", "detail", detail)
	w.state.Store(int32(StateBanned))
	w.metrics.ModemState.WithLabelValues(w.iccid).Set(float64(StateBanned))
	w.eventFn(tunnel.ModemAlertEvent{
		Envelope:  tunnel.NewEnvelope(tunnel.TypeModemAlert),
		ICCID:     w.iccid,
		AlertCode: tunnel.AlertSIMBanned,
		Detail:    detail,
	})
}

func (w *Worker) enterRecovery(s *at.Serializer, reg *Registry, log *slog.Logger) {
	log.Warn("entering recovery: radio cycle")
	w.state.Store(int32(StateRecovering))
	w.metrics.ModemState.WithLabelValues(w.iccid).Set(float64(StateRecovering))

	if err := s.RadioOff(); err != nil {
		log.Error("radio off failed, escalating to reset", "err", err)
		w.enterReset(s, log)
		return
	}
	time.Sleep(2 * time.Second)
	if err := s.RadioOn(); err != nil {
		log.Error("radio on failed, escalating to reset", "err", err)
		w.enterReset(s, log)
		return
	}
	time.Sleep(5 * time.Second)
	if err := s.Ping(); err != nil {
		log.Error("ping after radio cycle failed, escalating to reset", "err", err)
		w.enterReset(s, log)
		return
	}
	log.Info("radio cycle succeeded, back to ACTIVE")
	w.state.Store(int32(StateActive))
	w.metrics.ModemState.WithLabelValues(w.iccid).Set(float64(StateActive))
}

func (w *Worker) enterReset(s *at.Serializer, log *slog.Logger) {
	log.Warn("entering RESETTING: hard reset")
	w.state.Store(int32(StateResetting))
	w.metrics.ModemState.WithLabelValues(w.iccid).Set(float64(StateResetting))

	if err := s.HardReset(); err != nil {
		log.Error("hard reset failed, worker entering FAILED", "err", err)
		w.state.Store(int32(StateFailed))
		w.metrics.ModemState.WithLabelValues(w.iccid).Set(float64(StateFailed))
		w.eventFn(tunnel.ModemAlertEvent{
			Envelope:  tunnel.NewEnvelope(tunnel.TypeModemAlert),
			ICCID:     w.iccid,
			AlertCode: tunnel.AlertModemHang,
			Detail:    "hard reset failed",
		})
		return
	}
	// Wait for USB re-enumeration.
	time.Sleep(5 * time.Second)
	log.Info("hard reset complete, back to INITIALIZING")
	w.state.Store(int32(StateInitializing))
	w.metrics.ModemState.WithLabelValues(w.iccid).Set(float64(StateInitializing))
}

// ── Helpers ───────────────────────────────────────────────────────────────

func retryN(n int, fn func() error) error {
	var err error
	for i := 0; i < n; i++ {
		if err = fn(); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

func retryNVal(n int, fn func() (string, error)) (string, error) {
	var (
		v   string
		err error
	)
	for i := 0; i < n; i++ {
		if v, err = fn(); err == nil {
			return v, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", err
}
