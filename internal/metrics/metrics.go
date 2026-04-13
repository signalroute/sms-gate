// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Gateway holds all Prometheus metrics for the gateway process.
type Gateway struct {
	// SMS counters
	SMSReceived  *prometheus.CounterVec // labels: iccid
	SMSDelivered *prometheus.CounterVec // labels: iccid
	SMSSent      *prometheus.CounterVec // labels: iccid, status

	// smsgate_messages_sent_total and smsgate_messages_failed_total follow the
	// naming convention requested by operators for alerting dashboards.
	MessagesSentTotal   *prometheus.CounterVec // labels: iccid
	MessagesFailedTotal *prometheus.CounterVec // labels: iccid, reason

	SMSPendingCount prometheus.Gauge

	// Modem state
	ModemState      *prometheus.GaugeVec // labels: iccid
	ModemSignalRSSI *prometheus.GaugeVec // labels: iccid

	// Active modem connections (workers in ACTIVE or EXECUTING state).
	ActiveConnections prometheus.Gauge

	// RegistrationFailures counts network registration failures (stat==3 or
	// recovery triggered from keepalive timeout), by iccid.
	RegistrationFailures *prometheus.CounterVec

	// Tunnel
	TunnelState           prometheus.Gauge
	TunnelReconnectsTotal prometheus.Counter
	// ReconnectsTotal mirrors TunnelReconnectsTotal with the canonical name.
	ReconnectsTotal prometheus.Counter

	// QueueDepth is the number of tasks currently sitting in all worker inbound queues.
	QueueDepth prometheus.Gauge

	// AT command timing
	ATCmdDurationMs *prometheus.HistogramVec // labels: command

	// ATCommandDuration is the canonical AT command latency histogram in seconds.
	ATCommandDuration *prometheus.HistogramVec // labels: command

	// ModemInitDuration tracks how long the modem initialisation sequence takes.
	ModemInitDuration *prometheus.HistogramVec // labels: iccid

	// Reliability
	// TasksDropped counts tasks rejected because inboundCh was full (labels: iccid).
	TasksDropped *prometheus.CounterVec
	// WorkerStalls counts times a worker exceeded the stall duration without
	// completing a main-loop iteration (labels: iccid).
	WorkerStalls *prometheus.CounterVec

	// ModemReconnectTotal counts modem reconnection attempts (labels: port).
	ModemReconnectTotal *prometheus.CounterVec

	// ModemSignalStrength is the current AT+CSQ signal quality in dBm per iccid.
	ModemSignalStrength *prometheus.GaugeVec

	// ActiveSerialPorts tracks the number of opened serial port connections.
	ActiveSerialPorts prometheus.Gauge

	// BufferPendingCount tracks the number of pending rows in the SQLite buffer.
	BufferPendingCount prometheus.Gauge

	// OutboxDepth is the number of events waiting in the tunnel outbox channel.
	OutboxDepth prometheus.Gauge

	// TaskRoundTripDuration tracks the end-to-end time from task dispatch
	// to ACK being sent back, in seconds (labels: action).
	TaskRoundTripDuration *prometheus.HistogramVec

	// WorkerPoolTotal/Active/Banned track the modem worker pool composition (#57).
	WorkerPoolTotal  prometheus.Gauge
	WorkerPoolActive prometheus.Gauge
	WorkerPoolBanned prometheus.Gauge
}

// New creates and registers all metrics with the given registry.
func New(reg prometheus.Registerer) *Gateway {
	g := &Gateway{
		SMSReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_sms_received_total",
			Help: "Total SMS messages received, by iccid.",
		}, []string{"iccid"}),

		SMSDelivered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_sms_delivered_total",
			Help: "Total SMS messages ACKed by cloud, by iccid.",
		}, []string{"iccid"}),

		SMSSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_sms_sent_total",
			Help: "SEND_SMS tasks completed, by iccid and status.",
		}, []string{"iccid", "status"}),

		MessagesSentTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_messages_sent_total",
			Help: "Total outbound SMS messages successfully sent, by iccid.",
		}, []string{"iccid"}),

		MessagesFailedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_messages_failed_total",
			Help: "Total outbound SMS messages that failed, by iccid and reason.",
		}, []string{"iccid", "reason"}),

		SMSPendingCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_sms_pending_count",
			Help: "Current PENDING rows in SQLite buffer.",
		}),

		ModemState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "smsgate_modem_state",
			Help: "Current modem FSM state as numeric enum, by iccid.",
		}, []string{"iccid"}),

		ModemSignalRSSI: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "smsgate_modem_signal_rssi",
			Help: "Current RSSI in dBm, by iccid.",
		}, []string{"iccid"}),

		ActiveConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_active_connections",
			Help: "Number of modem workers currently in ACTIVE or EXECUTING state.",
		}),

		RegistrationFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_registration_failures_total",
			Help: "Network registration failures (denied or recovery triggered), by iccid.",
		}, []string{"iccid"}),

		TunnelState: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_tunnel_state",
			Help: "1 = CONNECTED, 0 = disconnected.",
		}),

		TunnelReconnectsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "smsgate_tunnel_reconnects_total",
			Help: "Total tunnel reconnection attempts.",
		}),

		ReconnectsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "smsgate_reconnects_total",
			Help: "Total tunnel reconnection attempts (canonical name).",
		}),

		QueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_queue_depth",
			Help: "Total tasks queued across all modem worker inbound channels.",
		}),

		ATCmdDurationMs: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "smsgate_at_cmd_duration_ms",
			Help:    "AT command round-trip time by command type.",
			Buckets: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000},
		}, []string{"command"}),

		TasksDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_tasks_dropped_total",
			Help: "Tasks rejected because the worker inbound queue was full, by iccid.",
		}, []string{"iccid"}),

		WorkerStalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_worker_stalls_total",
			Help: "Times a worker exceeded the stall threshold without progress, by iccid.",
		}, []string{"iccid"}),

		ModemReconnectTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smsgate_modem_reconnect_total",
			Help: "Total modem reconnection attempts, by port.",
		}, []string{"port"}),

		ModemSignalStrength: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "smsgate_modem_signal_strength",
			Help: "Current AT+CSQ signal strength in dBm, by iccid.",
		}, []string{"iccid"}),

		ActiveSerialPorts: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_active_serial_ports",
			Help: "Number of currently open modem serial port connections.",
		}),

		BufferPendingCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_buffer_pending_count",
			Help: "Number of pending rows in the SQLite buffer awaiting flush.",
		}),

		OutboxDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_outbox_depth",
			Help: "Number of events waiting in the tunnel outbox channel.",
		}),

		TaskRoundTripDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "smsgate_task_round_trip_seconds",
			Help:    "End-to-end task execution time from dispatch to ACK, by action.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
		}, []string{"action"}),

		ATCommandDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "smsgate_at_command_duration_seconds",
			Help:    "AT command round-trip latency in seconds, by command.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0},
		}, []string{"command"}),

		ModemInitDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "smsgate_modem_init_duration_seconds",
			Help:    "Duration of the modem initialisation sequence, by iccid.",
			Buckets: []float64{0.5, 1.0, 2.0, 5.0, 10.0, 30.0, 60.0},
		}, []string{"iccid"}),

		WorkerPoolTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_worker_pool_total",
			Help: "Total configured modem workers.",
		}),

		WorkerPoolActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_worker_pool_active",
			Help: "Modem workers currently in ACTIVE or EXECUTING state.",
		}),

		WorkerPoolBanned: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_worker_pool_banned",
			Help: "Modem workers currently in BANNED state.",
		}),
	}

	reg.MustRegister(
		g.SMSReceived,
		g.SMSDelivered,
		g.SMSSent,
		g.MessagesSentTotal,
		g.MessagesFailedTotal,
		g.SMSPendingCount,
		g.ModemState,
		g.ModemSignalRSSI,
		g.ActiveConnections,
		g.RegistrationFailures,
		g.TunnelState,
		g.TunnelReconnectsTotal,
		g.ReconnectsTotal,
		g.QueueDepth,
		g.ATCmdDurationMs,
		g.TasksDropped,
		g.WorkerStalls,
		g.ModemReconnectTotal,
		g.ModemSignalStrength,
		g.ActiveSerialPorts,
		g.BufferPendingCount,
		g.OutboxDepth,
		g.TaskRoundTripDuration,
		g.ATCommandDuration,
		g.ModemInitDuration,
		g.WorkerPoolTotal,
		g.WorkerPoolActive,
		g.WorkerPoolBanned,
	)
	return g
}

// HandlerFor returns an http.Handler serving /metrics from the given registry.
// Always bind to 127.0.0.1 only — never expose on 0.0.0.0.
func HandlerFor(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
