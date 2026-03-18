package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// HealthChecker periodically checks the health of endpoints using active
// HTTP probes. Each destination can configure its own interval, timeout,
// and healthy/unhealthy thresholds.
type HealthChecker struct {
	mu       sync.Mutex
	pools    map[string]*DestinationPool
	counters map[string]*healthCounter
	cancel   context.CancelFunc
	logger   *slog.Logger
}

type healthCounter struct {
	successes uint32
	failures  uint32
}

// NewHealthChecker creates a HealthChecker.
func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		pools:    make(map[string]*DestinationPool),
		counters: make(map[string]*healthCounter),
		logger:   logger,
	}
}

// Update replaces the set of destination pools to check.
func (hc *HealthChecker) Update(pools map[string]*DestinationPool) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.pools = pools

	newCounters := make(map[string]*healthCounter, len(pools)*2)
	for _, pool := range pools {
		for _, ep := range pool.Endpoints {
			key := pool.Destination.ID + "/" + ep.ID
			if c, ok := hc.counters[key]; ok {
				newCounters[key] = c
			} else {
				newCounters[key] = &healthCounter{}
			}
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
	snapshot := make(map[string]*DestinationPool, len(hc.pools))
	for k, v := range hc.pools {
		snapshot[k] = v
	}
	hc.mu.Unlock()

	for _, pool := range snapshot {
		d := pool.Destination
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

		for _, ep := range pool.Endpoints {
			elapsed := time.Since(ep.lastHealthCheck())
			if elapsed < interval {
				continue
			}
			ep.setLastHealthCheck(time.Now())

			key := d.ID + "/" + ep.ID

			hc.mu.Lock()
			counter := hc.counters[key]
			if counter == nil {
				counter = &healthCounter{}
				hc.counters[key] = counter
			}
			hc.mu.Unlock()

			passed := hc.checkEndpoint(ctx, ep, d, hcCfg.Path, timeout)

			ep.mu.RLock()
			wasHealthy := ep.Healthy
			ep.mu.RUnlock()

			if passed {
				counter.failures = 0
				counter.successes++
				if !wasHealthy && counter.successes >= healthyThreshold {
					ep.mu.Lock()
					ep.Healthy = true
					ep.mu.Unlock()
					hc.logger.Info("health check passed",
						slog.String("destination", d.ID),
						slog.String("endpoint", ep.ID),
					)
				}
			} else {
				counter.successes = 0
				counter.failures++
				if wasHealthy && counter.failures >= unhealthyThreshold {
					ep.mu.Lock()
					ep.Healthy = false
					ep.mu.Unlock()
					hc.logger.Warn("health check failed",
						slog.String("destination", d.ID),
						slog.String("endpoint", ep.ID),
					)
				}
			}
		}
	}
}

func (hc *HealthChecker) checkEndpoint(ctx context.Context, ep *Endpoint, d model.Destination, path string, timeout time.Duration) bool {
	scheme := "http"
	if d.Options != nil && d.Options.TLS != nil &&
		d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s:%d%s", scheme, ep.Host, ep.Port, path)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	client := &http.Client{Transport: ep.Transport}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
