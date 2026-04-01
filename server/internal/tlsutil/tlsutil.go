// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package tlsutil builds tls.Config and http.Transport from config.TLSConfig.
package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/achetronic/vrata/internal/config"
)

// resolvePEM returns the PEM content from a value that is either inline PEM
// or a file path. If the value starts with "-----BEGIN", it is treated as
// inline PEM. Otherwise it is read from disk as a file path.
func resolvePEM(value string) ([]byte, error) {
	if strings.HasPrefix(strings.TrimSpace(value), "-----BEGIN") {
		return []byte(value), nil
	}
	data, err := os.ReadFile(value)
	if err != nil {
		return nil, fmt.Errorf("reading PEM from %q: %w", value, err)
	}
	return data, nil
}

// ServerConfig creates a *tls.Config for an HTTP server from TLSConfig.
// Cert and Key are required. CA + ClientAuth enable mutual TLS.
// Cert, Key, and CA values can be inline PEM or file paths.
func ServerConfig(tc *config.TLSConfig) (*tls.Config, error) {
	certPEM, err := resolvePEM(tc.Cert)
	if err != nil {
		return nil, fmt.Errorf("resolving server cert: %w", err)
	}
	keyPEM, err := resolvePEM(tc.Key)
	if err != nil {
		return nil, fmt.Errorf("resolving server key: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing server certificate: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if tc.CA != "" {
		caPEM, err := resolvePEM(tc.CA)
		if err != nil {
			return nil, fmt.Errorf("resolving CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parsing CA bundle: no valid certificates found")
		}
		tlsCfg.ClientCAs = pool

		switch tc.ClientAuth {
		case "optional":
			tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
		case "require":
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		default:
			tlsCfg.ClientAuth = tls.NoClientCert
		}
	}

	return tlsCfg, nil
}

// ClientTransport creates an *http.Transport with TLS configured for
// connecting to a server. CA verifies the server cert. Cert+Key enable
// mutual TLS (client presents a certificate). When tc is nil, returns
// a clone of http.DefaultTransport.
// Cert, Key, and CA values can be inline PEM or file paths.
func ClientTransport(tc *config.TLSConfig) (*http.Transport, error) {
	if tc == nil {
		return http.DefaultTransport.(*http.Transport).Clone(), nil
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if tc.CA != "" {
		caPEM, err := resolvePEM(tc.CA)
		if err != nil {
			return nil, fmt.Errorf("resolving CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parsing CA bundle: no valid certificates found")
		}
		tlsCfg.RootCAs = pool
	}

	if tc.Cert != "" && tc.Key != "" {
		certPEM, err := resolvePEM(tc.Cert)
		if err != nil {
			return nil, fmt.Errorf("resolving client cert: %w", err)
		}
		keyPEM, err := resolvePEM(tc.Key)
		if err != nil {
			return nil, fmt.Errorf("resolving client key: %w", err)
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("parsing client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg
	return transport, nil
}
