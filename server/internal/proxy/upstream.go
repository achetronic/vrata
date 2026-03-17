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
// health state, balancer, and circuit breaker.
type Upstream struct {
	Destination      model.Destination
	Transport        *http.Transport
	Healthy          bool
	Balancer         Balancer
	CircuitBreaker   *CircuitBreaker
	OnResponse       func(destID string, statusCode int)
	mu               sync.RWMutex
	lastHealthAt     time.Time
}

func (u *Upstream) lastHealthCheck() time.Time {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.lastHealthAt
}

func (u *Upstream) setLastHealthCheck(t time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lastHealthAt = t
}

// NewUpstream creates an Upstream from a Destination, configuring TLS if needed.
func NewUpstream(d model.Destination) (*Upstream, error) {
	connectTimeout := 5 * time.Second
	if d.Options != nil && d.Options.ConnectTimeout != "" {
		if dur, err := time.ParseDuration(d.Options.ConnectTimeout); err == nil {
			connectTimeout = dur
		}
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 10,
		DialContext: (&net.Dialer{
			Timeout:   connectTimeout,
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
		if transport.TLSClientConfig != nil {
			transport.TLSClientConfig.NextProtos = append(transport.TLSClientConfig.NextProtos, "h2")
		}
	}

	return &Upstream{
		Destination:    d,
		Transport:      transport,
		Healthy:        true,
		Balancer:       buildBalancer(d),
		CircuitBreaker: buildCircuitBreaker(d),
	}, nil
}

// buildBalancer creates the appropriate balancer for a destination.
func buildBalancer(d model.Destination) Balancer {
	if d.Options == nil || d.Options.Balancing == nil {
		return nil // default weighted random handled by SelectBackend
	}
	switch d.Options.Balancing.Algorithm {
	case model.LBPolicyRingHash:
		min, max := 1024, 8388608
		if d.Options.Balancing.RingSize != nil {
			if d.Options.Balancing.RingSize.Min > 0 {
				min = int(d.Options.Balancing.RingSize.Min)
			}
			if d.Options.Balancing.RingSize.Max > 0 {
				max = int(d.Options.Balancing.RingSize.Max)
			}
		}
		return NewRingHashBalancer(min, max)
	case model.LBPolicyMaglev:
		size := 65537
		if d.Options.Balancing.MaglevTableSize > 0 {
			size = int(d.Options.Balancing.MaglevTableSize)
		}
		return NewMaglevBalancer(size)
	case model.LBPolicyLeastRequest:
		return NewLeastRequestBalancer()
	case model.LBPolicyRandom:
		return RandomBalancer{}
	default:
		return nil
	}
}

// buildCircuitBreaker creates a circuit breaker if configured.
func buildCircuitBreaker(d model.Destination) *CircuitBreaker {
	if d.Options == nil || d.Options.CircuitBreaker == nil {
		return nil
	}
	cb := d.Options.CircuitBreaker
	return NewCircuitBreaker(cb.MaxConnections, cb.MaxPendingRequests, cb.MaxRequests, cb.MaxRetries)
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
// When all weights are zero, backends are selected uniformly at random.
func SelectBackend(backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream {
	if len(backends) == 0 {
		return nil
	}
	if len(backends) == 1 {
		return upstreams[backends[0].DestinationID]
	}

	total := uint32(0)
	allZero := true
	for _, b := range backends {
		total += b.Weight
		if b.Weight > 0 {
			allZero = false
		}
	}

	if allZero {
		idx := rand.Intn(len(backends))
		return upstreams[backends[idx].DestinationID]
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
