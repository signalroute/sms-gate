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

	// Tunnel
	TunnelState           prometheus.Gauge
	TunnelReconnectsTotal prometheus.Counter
	// ReconnectsTotal mirrors TunnelReconnectsTotal with the canonical name.
	ReconnectsTotal prometheus.Counter

	// QueueDepth is the number of tasks currently sitting in all worker inbound queues.
	QueueDepth prometheus.Gauge

	// AT command timing
	ATCmdDurationMs *prometheus.HistogramVec // labels: command

	// Reliability
	// TasksDropped counts tasks rejected because inboundCh was full (labels: iccid).
	TasksDropped *prometheus.CounterVec
	// WorkerStalls counts times a worker exceeded the stall duration without
	// completing a main-loop iteration (labels: iccid).
	WorkerStalls *prometheus.CounterVec
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
		g.TunnelState,
		g.TunnelReconnectsTotal,
		g.ReconnectsTotal,
		g.QueueDepth,
		g.ATCmdDurationMs,
		g.TasksDropped,
		g.WorkerStalls,
	)
	return g
}

// HandlerFor returns an http.Handler serving /metrics from the given registry.
// Always bind to 127.0.0.1 only — never expose on 0.0.0.0.
func HandlerFor(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
