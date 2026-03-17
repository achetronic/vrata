package proxy

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// OutlierDetector tracks error rates per upstream and ejects endpoints
// that exceed the configured thresholds.
type OutlierDetector struct {
	mu       sync.Mutex
	trackers map[string]*outlierTracker
	logger   *slog.Logger
	cancel   context.CancelFunc
}

type outlierTracker struct {
	consecutive5xx         atomic.Int64
	consecutiveGateway     atomic.Int64
	ejected                atomic.Bool
	ejectedAt              time.Time
	ejectionCount          int
	cfg                    *model.OutlierDetectionOptions
	upstream               *Upstream
}

// NewOutlierDetector creates an OutlierDetector.
func NewOutlierDetector(logger *slog.Logger) *OutlierDetector {
	return &OutlierDetector{
		trackers: make(map[string]*outlierTracker),
		logger:   logger,
	}
}

// Update replaces the set of upstreams to track.
func (od *OutlierDetector) Update(upstreams map[string]*Upstream) {
	od.mu.Lock()
	defer od.mu.Unlock()

	newTrackers := make(map[string]*outlierTracker, len(upstreams))
	for id, u := range upstreams {
		d := u.Destination
		if d.Options == nil || d.Options.OutlierDetection == nil {
			continue
		}
		if existing, ok := od.trackers[id]; ok {
			existing.upstream = u
			newTrackers[id] = existing
		} else {
			newTrackers[id] = &outlierTracker{
				cfg:      d.Options.OutlierDetection,
				upstream: u,
			}
		}
	}
	od.trackers = newTrackers
}

// RecordResponse records the response status for outlier detection.
func (od *OutlierDetector) RecordResponse(destID string, statusCode int) {
	od.mu.Lock()
	t, ok := od.trackers[destID]
	od.mu.Unlock()
	if !ok {
		return
	}

	if statusCode >= 500 {
		t.consecutive5xx.Add(1)
		if statusCode == 502 || statusCode == 503 || statusCode == 504 {
			t.consecutiveGateway.Add(1)
		}
	} else {
		t.consecutive5xx.Store(0)
		t.consecutiveGateway.Store(0)
	}

	threshold5xx := int64(5)
	if t.cfg.Consecutive5xx > 0 {
		threshold5xx = int64(t.cfg.Consecutive5xx)
	}

	thresholdGw := int64(0)
	if t.cfg.ConsecutiveGatewayErrors > 0 {
		thresholdGw = int64(t.cfg.ConsecutiveGatewayErrors)
	}

	shouldEject := false
	if t.consecutive5xx.Load() >= threshold5xx {
		shouldEject = true
	}
	if thresholdGw > 0 && t.consecutiveGateway.Load() >= thresholdGw {
		shouldEject = true
	}

	if shouldEject && !t.ejected.Load() {
		t.ejected.Store(true)
		t.ejectedAt = time.Now()
		t.ejectionCount++
		t.upstream.mu.Lock()
		t.upstream.Healthy = false
		t.upstream.mu.Unlock()
		od.logger.Warn("outlier detection: ejecting upstream",
			slog.String("destination", destID),
		)
	}
}

// Start begins the periodic check to un-eject endpoints.
func (od *OutlierDetector) Start(ctx context.Context) {
	ctx, od.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				od.checkEjections()
			}
		}
	}()
}

func (od *OutlierDetector) checkEjections() {
	od.mu.Lock()
	defer od.mu.Unlock()

	for id, t := range od.trackers {
		if !t.ejected.Load() {
			continue
		}

		baseTime := 30 * time.Second
		if t.cfg.BaseEjectionTime != "" {
			if d, err := time.ParseDuration(t.cfg.BaseEjectionTime); err == nil {
				baseTime = d
			}
		}

		ejectionDuration := baseTime * time.Duration(t.ejectionCount)
		if time.Since(t.ejectedAt) > ejectionDuration {
			t.ejected.Store(false)
			t.consecutive5xx.Store(0)
			t.consecutiveGateway.Store(0)
			t.upstream.mu.Lock()
			t.upstream.Healthy = true
			t.upstream.mu.Unlock()
			od.logger.Info("outlier detection: restoring upstream",
				slog.String("destination", id),
			)
		}
	}
}

// Stop stops the outlier detector.
func (od *OutlierDetector) Stop() {
	if od.cancel != nil {
		od.cancel()
	}
}
