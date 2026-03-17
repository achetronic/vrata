package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// HealthChecker periodically checks the health of upstreams and marks them
// healthy or unhealthy.
type HealthChecker struct {
	mu       sync.Mutex
	upstreams map[string]*Upstream
	cancel   context.CancelFunc
	logger   *slog.Logger
}

// NewHealthChecker creates a HealthChecker.
func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		upstreams: make(map[string]*Upstream),
		logger:    logger,
	}
}

// Update replaces the set of upstreams to check.
func (hc *HealthChecker) Update(upstreams map[string]*Upstream) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.upstreams = upstreams
}

// Start begins health checking in the background.
func (hc *HealthChecker) Start(ctx context.Context) {
	ctx, hc.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hc.checkAll(ctx)
			}
		}
	}()
}

// Stop stops health checking.
func (hc *HealthChecker) Stop() {
	if hc.cancel != nil {
		hc.cancel()
	}
}

func (hc *HealthChecker) checkAll(ctx context.Context) {
	hc.mu.Lock()
	targets := make(map[string]*Upstream, len(hc.upstreams))
	for k, v := range hc.upstreams {
		targets[k] = v
	}
	hc.mu.Unlock()

	for id, u := range targets {
		d := u.Destination
		if d.Options == nil || d.Options.HealthCheck == nil {
			continue
		}
		hcCfg := d.Options.HealthCheck

		interval := 10 * time.Second
		if hcCfg.Interval != "" {
			if dur, err := time.ParseDuration(hcCfg.Interval); err == nil {
				interval = dur
			}
		}
		_ = interval // used by ticker, not per-check

		timeout := 5 * time.Second
		if hcCfg.Timeout != "" {
			if dur, err := time.ParseDuration(hcCfg.Timeout); err == nil {
				timeout = dur
			}
		}

		healthy := hc.checkOne(ctx, u, hcCfg.Path, timeout)

		u.mu.Lock()
		u.Healthy = healthy
		u.mu.Unlock()

		if !healthy {
			hc.logger.Warn("health check failed",
				slog.String("destination", id),
				slog.String("host", d.Host),
			)
		}
	}
}

func (hc *HealthChecker) checkOne(ctx context.Context, u *Upstream, path string, timeout time.Duration) bool {
	d := u.Destination
	scheme := "http"
	if d.Options != nil && d.Options.TLS != nil &&
		d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s:%d%s", scheme, d.Host, d.Port, path)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	client := &http.Client{Transport: u.Transport}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
