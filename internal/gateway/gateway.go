// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package gateway wires all subsystems together into the running gateway.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
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
	conf      *cfg.GatewayConfig
	log       *slog.Logger
	metrics   *metrics.Gateway
	buf       *buffer.Buffer
	reg       *modem.Registry
	limiter   *modem.RateLimiterRegistry
	mgr       *tunnel.Manager
	rtr       *router.Router
	promReg   *prometheus.Registry
	startTime time.Time
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
		conf:      conf,
		log:       log,
		metrics:   m,
		buf:       buf,
		reg:       reg,
		limiter:   limiter,
		promReg:   promReg,
		startTime: time.Now(),
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
		HandshakeTimeout:  time.Duration(conf.Tunnel.HandshakeTimeoutS) * time.Second,
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

	// Start metrics+health HTTP server — addr comes from config (which already
	// reflects the METRICS_ADDR env var override applied by config.Load).
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.HandlerFor(g.promReg))
	mux.HandleFunc("/health", g.healthHandler)
	mux.HandleFunc("/modems", g.modemsHandler)
	mux.HandleFunc("/modems/reset", g.modemResetHandler)

	// pprof endpoints for CPU/memory profiling (#79)
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	metricsSrv := &http.Server{
		Addr:    g.conf.Metrics.Addr,
		Handler: mux,
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

	// SIGUSR1 dumps all goroutine stacks to stderr (#77)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGUSR1)
	safe.Go(log, "sigusr1-handler", func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigCh:
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				_, _ = os.Stderr.Write(buf[:n])
				log.Info("goroutine dump written to stderr")
			}
		}
	})

	// Scrape queue depth and buffer stats every 15s and publish to Prometheus.
	safe.Go(log, "queue-depth-scraper", func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				g.metrics.QueueDepth.Set(float64(g.reg.TotalQueueDepth()))
				if pc, err := g.buf.PendingCount(); err == nil {
					g.metrics.BufferPendingCount.Set(float64(pc))
				}
				g.metrics.OutboxDepth.Set(float64(g.mgr.OutboxLen()))

				// Worker pool composition (#57)
				total, active, banned := g.reg.PoolCounts()
				g.metrics.WorkerPoolTotal.Set(float64(total))
				g.metrics.WorkerPoolActive.Set(float64(active))
				g.metrics.WorkerPoolBanned.Set(float64(banned))
			}
		}
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

		wg.Add(1)
		safe.GoWithWaitGroup(log, "modem-worker-"+mc.Port, &wg, func() {
			modem.RunSupervised(ctx, func() *modem.Worker {
				return modem.NewWorker(modem.WorkerConfig{
					Port:          mc.Port,
					Baud:          mc.Baud,
					GatewayID:     g.conf.Gateway.ID,
					ExpectedICCID: mc.ExpectedICCID,
					Buf:           g.buf,
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
					SignalPollInterval:  time.Duration(g.conf.Health.SignalPollIntervalS) * time.Second,
					Logger:              g.log,
					Metrics:             g.metrics,
				})
			}, g.reg, g.metrics, g.log)
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

// healthResponse is the JSON body returned by GET /health.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

// healthHandler serves GET /health.
// Returns 200 {"status":"ok",...} when the tunnel is connected, 503 otherwise.
func (g *Gateway) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := g.mgr.State()
	uptime := time.Since(g.startTime).Round(time.Second).String()

	resp := healthResponse{
		Version: agentVersion,
		Uptime:  uptime,
	}

	statusCode := http.StatusOK
	if state == tunnel.TunnelConnected {
		resp.Status = "ok"
	} else {
		resp.Status = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

// modemsHandler serves GET /modems — returns current modem statuses (#162).
func (g *Gateway) modemsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := g.reg.Snapshot()
	statuses := make([]modem.WorkerStatus, 0, len(snap))
	for _, s := range snap {
		statuses = append(statuses, s)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statuses)
}

// modemResetHandler serves POST /modems/reset?iccid=<ICCID> (#158).
// Triggers a soft reset on the modem matching the given ICCID.
func (g *Gateway) modemResetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	iccid := r.URL.Query().Get("iccid")
	if iccid == "" {
		http.Error(w, `{"error":"missing iccid query parameter"}`, http.StatusBadRequest)
		return
	}
	wk := g.reg.Get(iccid)
	if wk == nil {
		http.Error(w, `{"error":"modem not found"}`, http.StatusNotFound)
		return
	}
	wk.RequestReset()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"reset requested","iccid":"` + iccid + `"}`))
}
