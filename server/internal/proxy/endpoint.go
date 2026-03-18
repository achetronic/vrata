package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// Endpoint represents a single endpoint connection with its transport,
// health state, and identity. One Destination may have many Upstreams
// Endpoint is a live connection to a single resolved address. It embeds
// model.Endpoint (Host, Port) and adds runtime state: transport, health,
// and outlier detection callback.
type Endpoint struct {
	model.Endpoint
	ID           string
	Transport    *http.Transport
	Healthy      bool
	OnResponse   func(destID string, statusCode int)
	mu           sync.RWMutex
	lastHealthAt time.Time
}

func (u *Endpoint) lastHealthCheck() time.Time {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.lastHealthAt
}

func (u *Endpoint) setLastHealthCheck(t time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lastHealthAt = t
}

// NewEndpoint creates an Endpoint for a specific endpoint address, inheriting
// TLS and transport settings from the Destination.
func NewEndpoint(ep model.Endpoint, d model.Destination) (*Endpoint, error) {
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

	if d.Options != nil && d.Options.TLS != nil &&
		d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
		tlsCfg, err := buildTLSConfig(d)
		if err != nil {
			return nil, fmt.Errorf("building TLS config for %s: %w", d.ID, err)
		}
		transport.TLSClientConfig = tlsCfg
	}

	if d.Options != nil && d.Options.HTTP2 {
		transport.ForceAttemptHTTP2 = true
		if transport.TLSClientConfig != nil {
			transport.TLSClientConfig.NextProtos = append(transport.TLSClientConfig.NextProtos, "h2")
		}
	}

	return &Endpoint{
		Endpoint:  ep,
		ID:        fmt.Sprintf("%s:%d", ep.Host, ep.Port),
		Transport: transport,
		Healthy:   true,
	}, nil
}

// NewDestinationPool creates a DestinationPool from a Destination, creating
// one Endpoint per resolved endpoint.
func NewDestinationPool(d model.Destination) (*DestinationPool, error) {
	endpoints := d.ResolvedEndpoints()

	eps := make([]*Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		u, err := NewEndpoint(ep, d)
		if err != nil {
			return nil, err
		}
		eps = append(eps, u)
	}

	return &DestinationPool{
		Destination:    d,
		Endpoints: eps,
		Balancer:       buildBalancer(d),
		CircuitBreaker: buildCircuitBreaker(d),
	}, nil
}

// buildBalancer creates the appropriate balancer for a destination.
func buildBalancer(d model.Destination) Balancer {
	if d.Options == nil || d.Options.EndpointBalancing == nil {
		return nil
	}
	eb := d.Options.EndpointBalancing
	switch eb.Algorithm {
	case model.EndpointLBRingHash:
		min, max := 1024, 8388608
		if eb.RingHash != nil && eb.RingHash.RingSize != nil {
			if eb.RingHash.RingSize.Min > 0 {
				min = int(eb.RingHash.RingSize.Min)
			}
			if eb.RingHash.RingSize.Max > 0 {
				max = int(eb.RingHash.RingSize.Max)
			}
		}
		return NewRingHashBalancer(min, max)
	case model.EndpointLBMaglev:
		size := 65537
		if eb.Maglev != nil && eb.Maglev.TableSize > 0 {
			size = int(eb.Maglev.TableSize)
		}
		return NewMaglevBalancer(size)
	case model.EndpointLBLeastRequest:
		return NewLeastRequestBalancer()
	case model.EndpointLBRandom:
		return RandomBalancer{}
	case model.EndpointLBRoundRobin:
		return &RoundRobinBalancer{}
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

	if v, ok := tlsVersionMap[tlsOpts.MinVersion]; ok {
		cfg.MinVersion = v
	}
	if v, ok := tlsVersionMap[tlsOpts.MaxVersion]; ok {
		cfg.MaxVersion = v
	}

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

// SelectDestination picks a destination pool from weighted dests using weighted random.
func SelectDestination(dests []model.DestinationRef, pools map[string]*DestinationPool) *DestinationPool {
	if len(dests) == 0 {
		return nil
	}
	if len(dests) == 1 {
		return pools[dests[0].DestinationID]
	}

	total := uint32(0)
	allZero := true
	for _, b := range dests {
		total += b.Weight
		if b.Weight > 0 {
			allZero = false
		}
	}

	if allZero {
		idx := rand.Intn(len(dests))
		return pools[dests[idx].DestinationID]
	}

	r := rand.Uint32() % total
	cumulative := uint32(0)
	for _, b := range dests {
		cumulative += b.Weight
		if r < cumulative {
			return pools[b.DestinationID]
		}
	}
	return pools[dests[len(dests)-1].DestinationID]
}
