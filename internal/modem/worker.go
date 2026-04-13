// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package modem

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/signalroute/sms-gate/internal/at"
	"github.com/signalroute/sms-gate/internal/buffer"
	"github.com/signalroute/sms-gate/internal/metrics"
	"github.com/signalroute/sms-gate/internal/tunnel"

	"go.bug.st/serial"
)

// validE164 matches E.164 phone numbers: +<cc><subscriber> (7-15 digits).
var validE164 = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

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
	ICCID        string `json:"iccid"`
	Port         string `json:"port"`
	State        string `json:"state"`
	IMSI         string `json:"imsi"`
	Operator     string `json:"operator"`
	SignalRSSI   int    `json:"signal_rssi"`
	RegStatus    string `json:"reg_status"`
	Sent1H       int64  `json:"sent_1h"`
	Recv1H       int64  `json:"recv_1h"`
	LastActivity int64  `json:"last_activity_ms,omitempty"` // unix millis (#111)
}

// ── Worker ────────────────────────────────────────────────────────────────

// Worker manages one physical modem. It owns the AT Serializer for its port
// and implements the Modem State Machine from §5.1 of the spec.
type Worker struct {
	// Immutable after construction.
	port          string
	baud          int
	gatewayID     string
	expectedICCID string // optional ICCID guard (#135)
	// iccid, imsi, operator are set once in runInitSequence, before the worker
	// is registered in the Registry.  Registry.Register/Lookup use a mutex that
	// establishes the happens-before relationship between the write (init
	// goroutine) and any subsequent read (tunnel/heartbeat goroutine).
	iccid      string
	imsi       string
	operator   string

	// Concurrently updated.
	state      atomic.Int32
	signalRSSI atomic.Int32
	regStat    atomic.Int32
	sent1H     atomic.Int64
	recv1H     atomic.Int64

	// lastLoopNs is updated at the end of every main-loop iteration.
	// The watchdog goroutine uses this to detect a stuck worker (#112).
	lastLoopNs atomic.Int64

	// lastActivityNs records the last time the worker performed a meaningful
	// action (sent/received SMS, executed a task, etc.) for status reporting (#111).
	lastActivityNs atomic.Int64

	// Channels
	inboundCh chan InboundTask

	// Dependencies
	buf        *buffer.Buffer
	limiter    *RateLimiterRegistry
	eventFn    func(evt any) // push event upstream (SMSReceived, ModemAlert, TaskAck)
	rateCfg    RateLimitConfig

	// Health config
	keepaliveInterval    time.Duration
	simCapacityWarnPct   int
	simCapacityPurgePct  int
	stallDuration        time.Duration // how long without a loop iteration before marking Failed
	signalPollInterval   time.Duration // how often to poll AT+CSQ

	logger  *slog.Logger
	metrics *metrics.Gateway
}

// WorkerConfig holds constructor parameters.
type WorkerConfig struct {
	Port                string
	Baud                int
	GatewayID           string
	ExpectedICCID       string // optional: fail init if SIM ICCID doesn't match (#135)
	Buf                 *buffer.Buffer
	Limiter             *RateLimiterRegistry
	RateConfig          RateLimitConfig
	EventFn             func(evt any)
	KeepaliveInterval   time.Duration
	SIMCapacityWarnPct  int
	SIMCapacityPurgePct int
	// InboundQueueSize is the capacity of the per-worker inbound task channel.
	// Defaults to 64 when zero.  Increase for high-throughput deployments; the
	// metric smsgate_tasks_dropped_total will fire whenever it fills up.
	InboundQueueSize int
	// StallDuration is how long the worker main loop can block a single select
	// case handler before the watchdog goroutine declares a stall and
	// transitions the worker to Failed.  Defaults to 5 minutes when zero.
	StallDuration time.Duration
	// SignalPollInterval controls how often AT+CSQ is polled to update the
	// modem_signal_strength gauge.  Defaults to 30 seconds when zero.
	SignalPollInterval time.Duration
	Logger              *slog.Logger
	Metrics             *metrics.Gateway
}

// NewWorker constructs but does not start a Worker.
func NewWorker(cfg WorkerConfig) *Worker {
	queueSize := cfg.InboundQueueSize
	if queueSize <= 0 {
		queueSize = 64
	}
	stallDur := cfg.StallDuration
	if stallDur <= 0 {
		stallDur = 5 * time.Minute
	}
	signalPollInterval := cfg.SignalPollInterval
	if signalPollInterval <= 0 {
		signalPollInterval = 30 * time.Second
	}
	w := &Worker{
		port:                cfg.Port,
		baud:                cfg.Baud,
		gatewayID:           cfg.GatewayID,
		expectedICCID:       cfg.ExpectedICCID,
		buf:                 cfg.Buf,
		limiter:             cfg.Limiter,
		rateCfg:             cfg.RateConfig,
		eventFn:             cfg.EventFn,
		keepaliveInterval:   cfg.KeepaliveInterval,
		simCapacityWarnPct:  cfg.SIMCapacityWarnPct,
		simCapacityPurgePct: cfg.SIMCapacityPurgePct,
		stallDuration:       stallDur,
		signalPollInterval:  signalPollInterval,
		logger:              cfg.Logger,
		metrics:             cfg.Metrics,
		inboundCh:           make(chan InboundTask, queueSize),
	}
	w.state.Store(int32(StateInitializing))
	return w
}

// ICCID returns the SIM ICCID once initialization is complete.
func (w *Worker) ICCID() string { return w.iccid }

// QueueLen returns the number of tasks currently waiting in the inbound queue.
func (w *Worker) QueueLen() int { return len(w.inboundCh) }

// Status returns a snapshot of the worker's current status.
func (w *Worker) Status() WorkerStatus {
	return WorkerStatus{
		ICCID:        w.iccid,
		Port:         w.port,
		State:        State(w.state.Load()).String(),
		IMSI:         w.imsi,
		Operator:     w.operator,
		SignalRSSI:   int(w.signalRSSI.Load()),
		RegStatus:    tunnel.RegStatusString(int(w.regStat.Load())),
		Sent1H:       w.sent1H.Load(),
		Recv1H:       w.recv1H.Load(),
		LastActivity: w.lastActivityNs.Load() / int64(time.Millisecond),
	}
}

// Run starts the worker goroutine and blocks until ctx is cancelled or a fatal
// error occurs. Returns the final state.
func (w *Worker) Run(ctx context.Context, registry *Registry) State {
	log := w.logger.With("port", w.port, "component", "worker")
	log.Info("worker starting", "port", w.port, "baud", w.baud)

	// Open and configure serial port.
	ser, err := w.openPort()
	if err != nil {
		log.Error("failed to open serial port", "err", err)
		w.state.Store(int32(StateFailed))
		return StateFailed
	}
	defer ser.Close()
	w.metrics.ActiveSerialPorts.Inc()
	defer w.metrics.ActiveSerialPorts.Dec()

	atSer := at.NewSerializer(ser, log)
	defer atSer.Close()

	// Wire AT command latency into the smsgate_at_command_duration_seconds histogram.
	if w.metrics != nil {
		atSer.ObserveLatency = func(cmd string, dur time.Duration) {
			w.metrics.ATCommandDuration.WithLabelValues(cmd).Observe(dur.Seconds())
		}
	}

	// ── Init sequence (§4.1.2) ───────────────────────────────────────────

	initStart := time.Now()
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

	if w.metrics != nil {
		w.metrics.ModemInitDuration.WithLabelValues(w.iccid).Observe(time.Since(initStart).Seconds())
	}

	w.state.Store(int32(StateActive))
	w.metrics.ModemState.WithLabelValues(w.iccid).Set(float64(StateActive))
	w.metrics.ActiveConnections.Inc()

	keepaliveTicker := time.NewTicker(w.keepaliveInterval)
	capacityTicker := time.NewTicker(5 * time.Minute)
	signalTicker := time.NewTicker(w.signalPollInterval)
	defer keepaliveTicker.Stop()
	defer capacityTicker.Stop()
	defer signalTicker.Stop()

	// Seed the stall timer so the watchdog doesn't fire during init time.
	w.lastLoopNs.Store(time.Now().UnixNano())

	// Watchdog goroutine: if the main loop hasn't completed a select-case
	// handler in stallDuration, the worker is considered stuck and transitioned
	// to Failed so the gateway can restart or alert (#112).
	watchdogStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(w.stallDuration / 2)
		defer ticker.Stop()
		for {
			select {
			case <-watchdogStop:
				return
			case <-ticker.C:
				last := time.Unix(0, w.lastLoopNs.Load())
				if stall := time.Since(last); stall > w.stallDuration {
					log.Error("worker stall detected — transitioning to Failed",
						"stall_duration", stall,
						"threshold", w.stallDuration,
					)
					if w.metrics != nil {
						w.metrics.WorkerStalls.WithLabelValues(w.iccid).Inc()
					}
					w.state.Store(int32(StateFailed))
					return
				}
			}
		}
	}()
	defer close(watchdogStop)

	for {
		select {
		case <-ctx.Done():
			// ── Graceful shutdown ────────────────────────────────────────
			// Drain remaining inbound tasks and NACK them so the cloud
			// knows they were not executed and can retry.  This prevents
			// silent task loss on SIGTERM (issue #175).
			registry.Deregister(w.iccid)
			w.drainInboundCh(log)
			w.metrics.ActiveConnections.Dec()
			return StateActive

		case urc := <-atSer.URCCH:
			w.handleURC(ctx, atSer, urc, log)

		case task := <-w.inboundCh:
			log.Info("received task from inbound channel")
			w.executeTask(ctx, atSer, task, registry, log)

		case <-keepaliveTicker.C:
			w.runKeepalive(atSer, registry, log)

		case <-capacityTicker.C:
			w.checkCapacity(atSer, log)

		case <-signalTicker.C:
			w.pollSignalStrength(atSer, log)
		}

		// Stamp last-loop time so the watchdog can detect stalls.
		w.lastLoopNs.Store(time.Now().UnixNano())

		// If we entered a terminal state, exit.
		st := State(w.state.Load())
		if st == StateFailed || st == StateBanned {
			registry.Deregister(w.iccid)
			return st
		}
	}
}

// ── Serial port open ──────────────────────────────────────────────────────

// drainInboundCh reads all queued tasks from inboundCh and sends a NACK for
// each one, telling the cloud server that the task was not executed and should
// be retried.  Called on graceful shutdown to prevent silent task loss (#175).
func (w *Worker) drainInboundCh(log *slog.Logger) {
	for {
		select {
		case it := <-w.inboundCh:
			if log != nil {
				log.Warn("shutdown: NACKing unexecuted task",
					"task_id", it.Task.MessageID,
					"action", it.Task.Action,
				)
			}
			if it.AckFn != nil {
				it.AckFn(tunnel.TaskAckEvent{
					Envelope: tunnel.NewEnvelopeFrom(tunnel.TypeTaskAck, it.Task.MessageID),
					Status:   tunnel.StatusFailed,
					Error: &tunnel.TaskError{
						Code:    tunnel.ErrCodeModemUnresponsive,
						Message: "gateway shutting down — task was not executed",
					},
				})
			}
		default:
			return
		}
	}
}

// ── Serial port open ──────────────────────────────────────────────────────

// tcpPort wraps a net.Conn to satisfy the serial.Port interface (partially).
type tcpPort struct {
	net.Conn
}

func (p *tcpPort) SetMode(mode *serial.Mode) error { return nil }
func (p *tcpPort) Drain() error                   { return nil }
func (p *tcpPort) Break(time.Duration) error      { return nil }
func (p *tcpPort) SetDTR(bool) error              { return nil }
func (p *tcpPort) SetRTS(bool) error              { return nil }
func (p *tcpPort) SetReadTimeout(time.Duration) error { return nil }
func (p *tcpPort) ResetInputBuffer() error         { return nil }
func (p *tcpPort) ResetOutputBuffer() error        { return nil }
func (p *tcpPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}

func (w *Worker) openPort() (serial.Port, error) {
	// Heuristic: if port contains ':' and doesn't look like a Mac/Linux device path,
	// treat it as a TCP address.
	if strings.Contains(w.port, ":") && !strings.HasPrefix(w.port, "/") {
		w.logger.Info("opening TCP connection to modem", "addr", w.port)
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).Dial("tcp", w.port)
		if err != nil {
			return nil, fmt.Errorf("tcp dial %s: %w", w.port, err)
		}
		return &tcpPort{Conn: conn}, nil
	}

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
			if w.expectedICCID != "" && iccid != w.expectedICCID {
				return fmt.Errorf("ICCID mismatch: expected %s, got %s", w.expectedICCID, iccid)
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
			w.metrics.RegistrationFailures.WithLabelValues(w.iccid).Inc()
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
	w.lastActivityNs.Store(time.Now().UnixNano())

	log.Info("SMS received and decoded",
		"sender", decoded.Sender,
		"body_len", len(decoded.Body),
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
	w.lastActivityNs.Store(time.Now().UnixNano())

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
		if task.Action == tunnel.ActionSendSMS {
			w.metrics.MessagesFailedTotal.WithLabelValues(w.iccid, taskErr.Code).Inc()
		}
	} else if task.Action == tunnel.ActionSendSMS {
		w.metrics.MessagesSentTotal.WithLabelValues(w.iccid).Inc()
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
	if !validE164.MatchString(p.To) {
		return nil, &tunnel.TaskError{Code: tunnel.ErrCodeInvalidPayload, Message: fmt.Sprintf("invalid E.164 phone number: %q", p.To)}
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
		w.metrics.RegistrationFailures.WithLabelValues(w.iccid).Inc()
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
		w.metrics.RegistrationFailures.WithLabelValues(w.iccid).Inc()
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

// pollSignalStrength queries AT+CSQ and updates the modem_signal_strength gauge.
// Errors are logged at debug level and do not affect the worker state machine.
func (w *Worker) pollSignalStrength(s *at.Serializer, log *slog.Logger) {
	rssi, err := s.SignalQuality()
	if err != nil {
		log.Debug("signal poll failed", "err", err)
		return
	}
	w.signalRSSI.Store(int32(rssi))
	w.metrics.ModemSignalRSSI.WithLabelValues(w.iccid).Set(float64(rssi))
	w.metrics.ModemSignalStrength.WithLabelValues(w.iccid).Set(float64(rssi))
	log.Debug("signal poll", "rssi_dbm", rssi)
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
	// Re-verify PDU mode after recovery — modem may have reset to text mode (#142).
	if err := s.SetPDUMode(); err != nil {
		log.Error("PDU mode re-set after recovery failed", "err", err)
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
