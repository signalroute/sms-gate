// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package webhook delivers inbound-SMS notifications to an operator-configured
// HTTP endpoint.
//
// Configuration (environment variables):
//
//	WEBHOOK_URL     URL to POST notifications to (required to enable)
//	WEBHOOK_SECRET  HMAC-SHA256 signing key for the X-Signalroute-Signature header
//
// Payload (JSON):
//
//	{
//	  "event":     "sms.received",
//	  "iccid":     "<ICCID>",
//	  "from":      "<sender number>",
//	  "body":      "<message text>",
//	  "timestamp": "<RFC3339>"
//	}
//
// Each delivery is retried up to 3 times with exponential back-off (1 s base).
// The signature header is always included when WEBHOOK_SECRET is set.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/signalroute/sms-gate/internal/backoff"
)

const (
	maxAttempts  = 3
	signatureHdr = "X-Signalroute-Signature"
	contentType  = "application/json"
)

// Payload is the JSON body posted to the webhook endpoint.
type Payload struct {
	Event     string `json:"event"`
	ICCID     string `json:"iccid"`
	From      string `json:"from"`
	Body      string `json:"body"`
	Timestamp string `json:"timestamp"`
}

// Notifier sends webhook notifications for inbound SMS events.
type Notifier struct {
	url    string
	secret string
	client *http.Client
	log    *slog.Logger
}

// Config holds Notifier configuration.
type Config struct {
	// URL is the endpoint to POST to. Empty URL disables the notifier.
	URL string
	// Secret is the HMAC-SHA256 key for signing payloads. Empty means unsigned.
	Secret string
	// Client is the HTTP client to use. nil uses http.DefaultClient.
	Client *http.Client
	// Logger is used for retry/error logging. nil disables logging.
	Logger *slog.Logger
}

// New creates a Notifier. Returns nil if cfg.URL is empty (disabled).
func New(cfg Config) *Notifier {
	if cfg.URL == "" {
		return nil
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Notifier{
		url:    cfg.URL,
		secret: cfg.Secret,
		client: client,
		log:    log,
	}
}

// FromEnv creates a Notifier from WEBHOOK_URL and WEBHOOK_SECRET env vars.
// Returns nil when WEBHOOK_URL is unset.
func FromEnv(log *slog.Logger) *Notifier {
	return New(Config{
		URL:    os.Getenv("WEBHOOK_URL"),
		Secret: os.Getenv("WEBHOOK_SECRET"),
		Logger: log,
	})
}

// Notify sends an SMS-received notification to the configured endpoint.
// It retries up to maxAttempts times with exponential back-off on failure.
// ctx controls the overall deadline; individual attempts may still time out
// via the HTTP client's own timeout.
func (n *Notifier) Notify(ctx context.Context, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("webhook: context canceled: %w", err)
		}

		if err := n.post(ctx, body); err != nil {
			lastErr = err
			n.log.Warn("webhook delivery failed",
				"attempt", attempt,
				"max", maxAttempts,
				"err", err,
			)
			if attempt < maxAttempts {
				delay := backoff.Compute(attempt)
				select {
				case <-ctx.Done():
					return fmt.Errorf("webhook: context canceled during backoff: %w", ctx.Err())
				case <-time.After(delay):
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("webhook: all %d attempts failed: %w", maxAttempts, lastErr)
}

// post performs a single HTTP POST attempt.
func (n *Notifier) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)

	if n.secret != "" {
		sig := sign(body, n.secret)
		req.Header.Set(signatureHdr, "sha256="+sig)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// sign returns the hex-encoded HMAC-SHA256 of body keyed with secret.
func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
