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
	SMSReceived  *prometheus.CounterVec   // labels: iccid
	SMSDelivered *prometheus.CounterVec   // labels: iccid
	SMSSent      *prometheus.CounterVec   // labels: iccid, status
	SMSPendingCount prometheus.Gauge

	// Modem state
	ModemState      *prometheus.GaugeVec     // labels: iccid
	ModemSignalRSSI *prometheus.GaugeVec     // labels: iccid

	// Tunnel
	TunnelState           prometheus.Gauge
	TunnelReconnectsTotal prometheus.Counter

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

		TunnelState: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "smsgate_tunnel_state",
			Help: "1 = CONNECTED, 0 = disconnected.",
		}),

		TunnelReconnectsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "smsgate_tunnel_reconnects_total",
			Help: "Total tunnel reconnection attempts.",
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
		g.SMSPendingCount,
		g.ModemState,
		g.ModemSignalRSSI,
		g.TunnelState,
		g.TunnelReconnectsTotal,
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
