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

// HealthChecker periodically checks the health of upstreams using active
// HTTP probes. Each destination can configure its own interval, timeout,
// and healthy/unhealthy thresholds.
type HealthChecker struct {
	mu        sync.Mutex
	upstreams map[string]*Upstream
	counters  map[string]*healthCounter
	cancel    context.CancelFunc
	logger    *slog.Logger
}

// healthCounter tracks consecutive successes and failures for threshold-based
// health transitions.
type healthCounter struct {
	successes uint32
	failures  uint32
}

// NewHealthChecker creates a HealthChecker.
func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		upstreams: make(map[string]*Upstream),
		counters:  make(map[string]*healthCounter),
		logger:    logger,
	}
}

// Update replaces the set of upstreams to check.
func (hc *HealthChecker) Update(upstreams map[string]*Upstream) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.upstreams = upstreams

	newCounters := make(map[string]*healthCounter, len(upstreams))
	for id := range upstreams {
		if c, ok := hc.counters[id]; ok {
			newCounters[id] = c
		} else {
			newCounters[id] = &healthCounter{}
		}
	}
	hc.counters = newCounters
}

// Start begins health checking in the background.
func (hc *HealthChecker) Start(ctx context.Context) {
	ctx, hc.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(time.Second)
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

		hc.mu.Lock()
		counter := hc.counters[id]
		if counter == nil {
			counter = &healthCounter{}
			hc.counters[id] = counter
		}
		hc.mu.Unlock()

		elapsed := time.Since(u.lastHealthCheck())
		if elapsed < interval {
			continue
		}
		u.setLastHealthCheck(time.Now())

		timeout := 5 * time.Second
		if hcCfg.Timeout != "" {
			if dur, err := time.ParseDuration(hcCfg.Timeout); err == nil {
				timeout = dur
			}
		}

		unhealthyThreshold := uint32(3)
		if hcCfg.UnhealthyThreshold > 0 {
			unhealthyThreshold = hcCfg.UnhealthyThreshold
		}

		healthyThreshold := uint32(2)
		if hcCfg.HealthyThreshold > 0 {
			healthyThreshold = hcCfg.HealthyThreshold
		}

		passed := hc.checkOne(ctx, u, hcCfg.Path, timeout)

		u.mu.RLock()
		wasHealthy := u.Healthy
		u.mu.RUnlock()

		if passed {
			counter.failures = 0
			counter.successes++
			if !wasHealthy && counter.successes >= healthyThreshold {
				u.mu.Lock()
				u.Healthy = true
				u.mu.Unlock()
				hc.logger.Info("health check passed",
					slog.String("destination", id),
					slog.String("host", d.Host),
				)
			}
		} else {
			counter.successes = 0
			counter.failures++
			if wasHealthy && counter.failures >= unhealthyThreshold {
				u.mu.Lock()
				u.Healthy = false
				u.mu.Unlock()
				hc.logger.Warn("health check failed",
					slog.String("destination", id),
					slog.String("host", d.Host),
				)
			}
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
