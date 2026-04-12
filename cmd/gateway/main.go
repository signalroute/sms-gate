// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Command gateway is the Go-SMS-Gate headless modem daemon.
//
// Usage:
//
//	go-sms-gate --config /etc/go-sms-gate/config.yaml
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/signalroute/sms-gate/internal/config"
	"github.com/signalroute/sms-gate/internal/gateway"
)

// version is injected at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// ── Flags ──────────────────────────────────────────────────────────────
	configPath := flag.String("config", "config.yaml", "path to config.yaml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-sms-gate %s\n", version)
		return nil
	}

	// ── Config ─────────────────────────────────────────────────────────────
	conf, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config %q: %w", *configPath, err)
	}

	// ── Structured logger ──────────────────────────────────────────────────
	// Env vars LOG_LEVEL and LOG_FORMAT override values from the config file.
	logLevel := conf.Gateway.LogLevel
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		logLevel = v
	}
	logFormat := conf.Gateway.LogFormat
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		logFormat = v
	}
	log := buildLogger(logLevel, logFormat)
	log.Info("go-sms-gate starting",
		"version", version,
		"gateway_id", conf.Gateway.ID,
		"modems", len(conf.Modems),
	)

	// ── Context + signal handling ──────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Build and run gateway ──────────────────────────────────────────────
	gw, err := gateway.New(conf, log)
	if err != nil {
		return fmt.Errorf("build gateway: %w", err)
	}

	if err := gw.Run(ctx); err != nil {
		return fmt.Errorf("gateway exited with error: %w", err)
	}

	log.Info("shutdown complete")
	return nil
}

// buildLogger constructs a structured logger at the given level and format.
// format must be "json" or "text" (default). JSON is suitable for log
// aggregation systems (Loki, Datadog); text is human-readable for terminals.
func buildLogger(level, format string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     slogLevel,
		AddSource: slogLevel == slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().UTC().Format("2006-01-02T15:04:05.000Z"))
			}
			return a
		},
	}

	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
