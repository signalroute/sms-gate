// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package gateway wires all subsystems together into the running gateway.
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/signalroute/sms-gate/internal/buffer"
	cfg "github.com/signalroute/sms-gate/internal/config"
	"github.com/signalroute/sms-gate/internal/metrics"
	"github.com/signalroute/sms-gate/internal/modem"
	"github.com/signalroute/sms-gate/internal/router"
	"github.com/signalroute/sms-gate/internal/safe"
	"github.com/signalroute/sms-gate/internal/tunnel"
)

const agentVersion = "2.0.0"

// Gateway is the top-level runtime object.
type Gateway struct {
	conf    *cfg.GatewayConfig
	log     *slog.Logger
	metrics *metrics.Gateway
	buf     *buffer.Buffer
	reg     *modem.Registry
	limiter *modem.RateLimiterRegistry
	mgr     *tunnel.Manager
	rtr     *router.Router
	promReg *prometheus.Registry
}

// New constructs the Gateway, opening the SQLite buffer and wiring all subsystems.
// It does not start any goroutines — call Run() for that.
func New(conf *cfg.GatewayConfig, log *slog.Logger) (*Gateway, error) {
	promReg := prometheus.NewRegistry()
	m := metrics.New(promReg)

	buf, err := buffer.Open(conf.Buffer.DBPath, log)
	if err != nil {
		return nil, fmt.Errorf("open buffer: %w", err)
	}

	reg := modem.NewRegistry()
	limiter := modem.NewRateLimiterRegistry()

	g := &Gateway{
		conf:    conf,
		log:     log,
		metrics: m,
		buf:     buf,
		reg:     reg,
		limiter: limiter,
		promReg: promReg,
	}

	// Tunnel Manager — eventFn is wired below after rtr is built.
	mgr := tunnel.NewManager(tunnel.ManagerConfig{
		GatewayID:         conf.Gateway.ID,
		AgentVersion:      agentVersion,
		URL:               conf.Tunnel.URL,
		Token:             conf.Tunnel.Token,
		PingInterval:      time.Duration(conf.Tunnel.PingIntervalS) * time.Second,
		PingTimeout:       time.Duration(conf.Tunnel.PingTimeoutS) * time.Second,
		HeartbeatInterval: time.Duration(conf.Tunnel.HeartbeatIntervalS) * time.Second,
		ACKTimeout:        time.Duration(conf.Tunnel.ACKTimeoutS) * time.Second,
		ReconnectBase:     time.Duration(conf.Tunnel.ReconnectBaseS) * time.Second,
		ReconnectMax:      time.Duration(conf.Tunnel.ReconnectMaxS) * time.Second,
		Buf:               buf,
		RetentionDays:     conf.Buffer.RetentionDays,
		FlushInterval:     time.Duration(conf.Buffer.FlushIntervalM) * time.Minute,
		StatusFn:          g.modemStatuses,
		Logger:            log,
		Metrics:           m,
	})
	g.mgr = mgr

	// Task Router — dispatches inbound tasks to workers and pushes ACKs via mgr.Push.
	rtr := router.New(reg, mgr.Push, m)
	mgr.InboundTaskFn = rtr.Dispatch
	g.rtr = rtr

	return g, nil
}

// Run starts all goroutines and blocks until ctx is cancelled.
func (g *Gateway) Run(ctx context.Context) error {
	log := g.log.With("component", "gateway")
	log.Info("starting", "gateway_id", g.conf.Gateway.ID, "version", agentVersion)

	var wg sync.WaitGroup

	// Start metrics HTTP server (loopback only).
	metricsSrv := &http.Server{
		Addr:    g.conf.Metrics.Addr,
		Handler: metrics.HandlerFor(g.promReg),
	}
	wg.Add(1)
	safe.GoWithWaitGroup(log, "metrics-server", &wg, func() {
		log.Info("metrics server listening", "addr", g.conf.Metrics.Addr)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server error", "err", err)
		}
	})
	safe.Go(log, "metrics-shutdown", func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsSrv.Shutdown(shutCtx)
	})

	// Build eventFn: workers push events into the tunnel manager's outbox.
	// Alerts also log at ERROR level.
	eventFn := func(evt any) {
		switch e := evt.(type) {
		case tunnel.ModemAlertEvent:
			log.Error("modem alert",
				"iccid", e.ICCID,
				"alert_code", e.AlertCode,
				"detail", e.Detail,
			)
		}
		g.mgr.Push(evt)
	}

	// Start one modem worker per configured port.
	for _, mc := range g.conf.Modems {
		mc := mc // capture
		w := modem.NewWorker(modem.WorkerConfig{
			Port:      mc.Port,
			Baud:      mc.Baud,
			GatewayID: g.conf.Gateway.ID,
			Buf:       g.buf,
			Limiter:   g.limiter,
			RateConfig: modem.RateLimitConfig{
				PerMin:  mc.RateLimit.PerMin,
				PerHour: mc.RateLimit.PerHour,
				PerDay:  mc.RateLimit.PerDay,
			},
			EventFn:             eventFn,
			KeepaliveInterval:   time.Duration(g.conf.Health.KeepaliveIntervalS) * time.Second,
			SIMCapacityWarnPct:  g.conf.Health.SIMCapacityWarnPct,
			SIMCapacityPurgePct: g.conf.Health.SIMCapacityPurgePct,
			Logger:              g.log,
			Metrics:             g.metrics,
		})

		wg.Add(1)
		safe.GoWithWaitGroup(log, "modem-worker-"+mc.Port, &wg, func() {
			finalState := w.Run(ctx, g.reg)
			log.Info("worker exited", "port", mc.Port, "final_state", finalState)
		})
	}

	// Start tunnel manager.
	wg.Add(1)
	safe.GoWithWaitGroup(log, "tunnel-manager", &wg, func() {
		g.mgr.Run(ctx)
		log.Info("tunnel manager exited")
	})

	// Wait for all goroutines to finish, with a shutdown timeout (#175, #176).
	// If workers take longer than shutdownTimeout, log a warning and proceed
	// with buffer close so we don't hang the process indefinitely.
	const shutdownTimeout = 30 * time.Second
	done := make(chan struct{})
	safe.Go(log, "shutdown-watcher", func() {
		wg.Wait()
		close(done)
	})
	select {
	case <-done:
		log.Info("all goroutines exited cleanly")
	case <-time.After(shutdownTimeout):
		log.Warn("shutdown timeout reached; some goroutines may still be running",
			"timeout", shutdownTimeout)
	}
	return g.buf.Close()
}

// modemStatuses collects WorkerStatus from all registered workers for heartbeats.
func (g *Gateway) modemStatuses() []tunnel.ModemStatus {
	snap := g.reg.Snapshot()
	out := make([]tunnel.ModemStatus, 0, len(snap))
	for _, s := range snap {
		out = append(out, tunnel.ModemStatus{
			ICCID:      s.ICCID,
			Port:       s.Port,
			State:      s.State,
			IMSI:       s.IMSI,
			Operator:   s.Operator,
			SignalRSSI: s.SignalRSSI,
			RegStatus:  s.RegStatus,
			Sent1H:     s.Sent1H,
			Recv1H:     s.Recv1H,
		})
	}
	return out
}
