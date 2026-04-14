// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

// Package tlsconfig builds a *tls.Config from environment variables for the
// cloud WebSocket connection.
//
// Environment variables:
//
//	CLOUD_TLS_CERT  path to a PEM-encoded client certificate (mutual TLS)
//	CLOUD_TLS_KEY   path to a PEM-encoded private key   (mutual TLS)
//	CLOUD_TLS_CA    path to a PEM-encoded CA certificate (server certificate pinning)
//
// Behavior matrix:
//
//	CERT+KEY set, CA set   → mutual TLS with custom CA trust
//	CERT+KEY set, CA unset → mutual TLS with system trust store
//	CA set only            → server cert pinned to CA, no client cert
//	none set               → standard TLS using the system trust store
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// FromEnv reads CLOUD_TLS_CERT, CLOUD_TLS_KEY, and CLOUD_TLS_CA from the
// environment and returns a *tls.Config appropriate for those settings.
// Returns nil if none of the variables are set (callers may use nil to
// accept standard library defaults).
func FromEnv() (*tls.Config, error) {
	certPath := os.Getenv("CLOUD_TLS_CERT")
	keyPath := os.Getenv("CLOUD_TLS_KEY")
	caPath := os.Getenv("CLOUD_TLS_CA")

	if certPath == "" && keyPath == "" && caPath == "" {
		return nil, nil
	}

	cfg := &tls.Config{}

	if caPath != "" {
		pool, err := loadCACert(caPath)
		if err != nil {
			return nil, fmt.Errorf("tlsconfig: load CA %q: %w", caPath, err)
		}
		cfg.RootCAs = pool
	}

	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return nil, fmt.Errorf("tlsconfig: CLOUD_TLS_CERT and CLOUD_TLS_KEY must both be set for mutual TLS")
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("tlsconfig: load key pair: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	cfg.MinVersion = tls.VersionTLS12
	return cfg, nil
}

// loadCACert parses a PEM file and returns a certificate pool containing it.
func loadCACert(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no valid certificates found in %q", path)
	}
	return pool, nil
}
