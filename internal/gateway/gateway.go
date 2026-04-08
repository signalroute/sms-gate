// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 yanujz

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
	"github.com/yanujz/go-sms-gate/internal/buffer"
	cfg "github.com/yanujz/go-sms-gate/internal/config"
	"github.com/yanujz/go-sms-gate/internal/metrics"
	"github.com/yanujz/go-sms-gate/internal/modem"
	"github.com/yanujz/go-sms-gate/internal/router"
	"github.com/yanujz/go-sms-gate/internal/tunnel"
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
	rtr := router.New(reg, mgr.Push)
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
		Handler: metrics.HandlerFor(promReg),
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("metrics server listening", "addr", g.conf.Metrics.Addr)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server error", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsSrv.Shutdown(shutCtx)
	}()

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
		go func() {
			defer wg.Done()
			finalState := w.Run(ctx, g.reg)
			log.Info("worker exited", "port", mc.Port, "final_state", finalState)
		}()
	}

	// Start tunnel manager.
	wg.Add(1)
	go func() {
		defer wg.Done()
		g.mgr.Run(ctx)
		log.Info("tunnel manager exited")
	}()

	wg.Wait()
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
