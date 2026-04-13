// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Signalroute

package tlsconfig_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signalroute/sms-gate/internal/tlsconfig"
)

// writePEM writes PEM data to a temp file in dir and returns the path.
func writePEM(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// generateSelfSigned creates a minimal self-signed ECDSA cert and returns
// (certPEM, keyPEM).
func generateSelfSigned(t *testing.T) ([]byte, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"CLOUD_TLS_CERT", "CLOUD_TLS_KEY", "CLOUD_TLS_CA"} {
		t.Setenv(k, "")
	}
}

// TestFromEnv_NoVars returns nil when no env vars are set.
func TestFromEnv_NoVars(t *testing.T) {
	clearEnv(t)
	cfg, err := tlsconfig.FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config, got %+v", cfg)
	}
}

// TestFromEnv_CAOnly pins the server cert via a custom CA.
func TestFromEnv_CAOnly(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	certPEM, _ := generateSelfSigned(t)
	caPath := writePEM(t, dir, "ca.pem", certPEM)
	t.Setenv("CLOUD_TLS_CA", caPath)

	cfg, err := tlsconfig.FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
	if len(cfg.Certificates) != 0 {
		t.Errorf("expected no client cert, got %d", len(cfg.Certificates))
	}
}

// TestFromEnv_MutualTLS loads a client cert+key and verifies they are included.
func TestFromEnv_MutualTLS(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	certPEM, keyPEM := generateSelfSigned(t)
	certPath := writePEM(t, dir, "cert.pem", certPEM)
	keyPath := writePEM(t, dir, "key.pem", keyPEM)
	t.Setenv("CLOUD_TLS_CERT", certPath)
	t.Setenv("CLOUD_TLS_KEY", keyPath)

	cfg, err := tlsconfig.FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 client cert, got %d", len(cfg.Certificates))
	}
	if cfg.RootCAs != nil {
		t.Error("expected RootCAs to be nil when no CA provided")
	}
}

// TestFromEnv_MutualTLSWithCA loads cert+key AND a CA cert.
func TestFromEnv_MutualTLSWithCA(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	certPEM, keyPEM := generateSelfSigned(t)
	certPath := writePEM(t, dir, "cert.pem", certPEM)
	keyPath := writePEM(t, dir, "key.pem", keyPEM)
	caPath := writePEM(t, dir, "ca.pem", certPEM) // reuse the same cert as CA
	t.Setenv("CLOUD_TLS_CERT", certPath)
	t.Setenv("CLOUD_TLS_KEY", keyPath)
	t.Setenv("CLOUD_TLS_CA", caPath)

	cfg, err := tlsconfig.FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 client cert, got %d", len(cfg.Certificates))
	}
	if cfg.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}
}

// TestFromEnv_MissingCertFile returns an error when CLOUD_TLS_CERT points to a nonexistent file.
func TestFromEnv_MissingCertFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("CLOUD_TLS_CERT", "/nonexistent/cert.pem")
	t.Setenv("CLOUD_TLS_KEY", "/nonexistent/key.pem")

	_, err := tlsconfig.FromEnv()
	if err == nil {
		t.Fatal("expected error for missing cert file")
	}
}

// TestFromEnv_MissingCAFile returns an error when CLOUD_TLS_CA points to a nonexistent file.
func TestFromEnv_MissingCAFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("CLOUD_TLS_CA", "/nonexistent/ca.pem")

	_, err := tlsconfig.FromEnv()
	if err == nil {
		t.Fatal("expected error for missing CA file")
	}
}

// TestFromEnv_InvalidCA returns an error when the CA file contains no valid PEM cert.
func TestFromEnv_InvalidCA(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	caPath := writePEM(t, dir, "bad-ca.pem", []byte("this is not a certificate"))
	t.Setenv("CLOUD_TLS_CA", caPath)

	_, err := tlsconfig.FromEnv()
	if err == nil {
		t.Fatal("expected error for invalid CA PEM")
	}
}

// TestFromEnv_CertWithoutKey returns an error when only CLOUD_TLS_CERT is set.
func TestFromEnv_CertWithoutKey(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	certPEM, _ := generateSelfSigned(t)
	certPath := writePEM(t, dir, "cert.pem", certPEM)
	t.Setenv("CLOUD_TLS_CERT", certPath)

	_, err := tlsconfig.FromEnv()
	if err == nil {
		t.Fatal("expected error when only cert is set without key")
	}
}

// TestFromEnv_TLSConfigIsUsable verifies the returned *tls.Config is usable for dialing.
func TestFromEnv_TLSConfigIsUsable(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	certPEM, keyPEM := generateSelfSigned(t)
	certPath := writePEM(t, dir, "cert.pem", certPEM)
	keyPath := writePEM(t, dir, "key.pem", keyPEM)
	t.Setenv("CLOUD_TLS_CERT", certPath)
	t.Setenv("CLOUD_TLS_KEY", keyPath)

	cfg, err := tlsconfig.FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify that the certificate is parseable (i.e., the config is usable).
	cert := cfg.Certificates[0]
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}
	if leaf.Subject.CommonName != "test" {
		t.Errorf("unexpected CN %q", leaf.Subject.CommonName)
	}
	// Verify it satisfies the tls.Certificate interface expectation.
	_ = tls.Certificate(cert)
}
