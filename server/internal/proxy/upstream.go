package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// Upstream represents a destination with its reverse proxy, TLS config,
// and health state.
type Upstream struct {
	Destination model.Destination
	Transport   *http.Transport
	Healthy     bool
	mu          sync.RWMutex
}

// NewUpstream creates an Upstream from a Destination, configuring TLS if needed.
func NewUpstream(d model.Destination) (*Upstream, error) {
	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 10,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	if d.Options != nil && d.Options.MaxRequestsPerConnection > 0 {
		transport.MaxConnsPerHost = int(d.Options.MaxRequestsPerConnection)
	}

	// TLS upstream.
	if d.Options != nil && d.Options.TLS != nil &&
		d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
		tlsCfg, err := buildTLSConfig(d)
		if err != nil {
			return nil, fmt.Errorf("building TLS config for %s: %w", d.ID, err)
		}
		transport.TLSClientConfig = tlsCfg
	}

	// HTTP/2.
	if d.Options != nil && d.Options.HTTP2 {
		transport.ForceAttemptHTTP2 = true
	}

	return &Upstream{
		Destination: d,
		Transport:   transport,
		Healthy:     true,
	}, nil
}

// buildTLSConfig creates a tls.Config from Destination TLS options.
func buildTLSConfig(d model.Destination) (*tls.Config, error) {
	tlsOpts := d.Options.TLS
	cfg := &tls.Config{
		ServerName: d.Host,
	}

	if tlsOpts.SNI != "" {
		cfg.ServerName = tlsOpts.SNI
	}

	// Min/max TLS version.
	if v, ok := tlsVersionMap[tlsOpts.MinVersion]; ok {
		cfg.MinVersion = v
	}
	if v, ok := tlsVersionMap[tlsOpts.MaxVersion]; ok {
		cfg.MaxVersion = v
	}

	// CA certificate.
	caFile := tlsOpts.CAFile
	if caFile == "" {
		caFile = "/etc/ssl/certs/ca-certificates.crt"
	}
	caCert, err := os.ReadFile(caFile)
	if err == nil {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caCert)
		cfg.RootCAs = pool
	}

	// Client certificate (mTLS).
	if tlsOpts.Mode == model.TLSModeMTLS && tlsOpts.CertFile != "" && tlsOpts.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tlsOpts.CertFile, tlsOpts.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}

var tlsVersionMap = map[string]uint16{
	"TLSv1_0": tls.VersionTLS10,
	"TLSv1_1": tls.VersionTLS11,
	"TLSv1_2": tls.VersionTLS12,
	"TLSv1_3": tls.VersionTLS13,
}

// ReverseProxy creates an httputil.ReverseProxy targeting this upstream.
func (u *Upstream) ReverseProxy() *httputil.ReverseProxy {
	d := u.Destination
	scheme := "http"
	if d.Options != nil && d.Options.TLS != nil &&
		d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
		scheme = "https"
	}
	target := &url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", d.Host, d.Port),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = u.Transport
	return proxy
}

// SelectBackend picks a backend from weighted backends using weighted random.
func SelectBackend(backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream {
	if len(backends) == 0 {
		return nil
	}
	if len(backends) == 1 {
		return upstreams[backends[0].DestinationID]
	}

	// Weighted random selection.
	total := uint32(0)
	for _, b := range backends {
		total += b.Weight
	}
	if total == 0 {
		total = uint32(len(backends))
	}

	r := rand.Uint32() % total
	cumulative := uint32(0)
	for _, b := range backends {
		cumulative += b.Weight
		if r < cumulative {
			return upstreams[b.DestinationID]
		}
	}
	return upstreams[backends[len(backends)-1].DestinationID]
}
