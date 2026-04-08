// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// OutlierDetector tracks error rates per endpoint and ejects those
// that exceed the configured thresholds.
type OutlierDetector struct {
	mu         sync.RWMutex
	trackers   map[string]*outlierTracker
	logger     *slog.Logger
	cancel     context.CancelFunc
	updateCh   chan struct{}
}

// outlierTracker tracks consecutive errors for a single endpoint.
type outlierTracker struct {
	consecutive5xx     atomic.Int64
	consecutiveGateway atomic.Int64
	ejected            atomic.Bool
	ejectedAt          time.Time
	ejectionCount      int
	cfg                *model.OutlierDetectionOptions
	endpoint           *Endpoint
	destID             string
}

// NewOutlierDetector creates an OutlierDetector.
func NewOutlierDetector(logger *slog.Logger) *OutlierDetector {
	return &OutlierDetector{
		trackers: make(map[string]*outlierTracker),
		logger:   logger,
		updateCh: make(chan struct{}, 1),
	}
}

// Update replaces the set of destination pools to track.
func (od *OutlierDetector) Update(pools map[string]*DestinationPool) {
	od.mu.Lock()
	defer od.mu.Unlock()

	newTrackers := make(map[string]*outlierTracker)
	for _, pool := range pools {
		d := pool.Destination
		if d.Options == nil || d.Options.OutlierDetection == nil {
			continue
		}
		for _, ep := range pool.Endpoints {
			key := d.ID + "/" + ep.ID
			if existing, ok := od.trackers[key]; ok {
				existing.endpoint = ep
				newTrackers[key] = existing
			} else {
				newTrackers[key] = &outlierTracker{
					cfg:      d.Options.OutlierDetection,
					endpoint: ep,
					destID:   d.ID,
				}
			}
		}
	}
	od.trackers = newTrackers

	select {
	case od.updateCh <- struct{}{}:
	default:
	}
}

// RecordResponse records the response status for outlier detection.
// destID is the Destination ID and epID is the endpoint ID (host:port).
func (od *OutlierDetector) RecordResponse(destID, epID string, statusCode int) {
	key := destID + "/" + epID
	od.mu.RLock()
	t, ok := od.trackers[key]
	od.mu.RUnlock()
	if !ok {
		return
	}

	if statusCode >= 500 {
		t.consecutive5xx.Add(1)
		t.endpoint.Consecutive5xx.Store(t.consecutive5xx.Load())
		if statusCode == 502 || statusCode == 503 || statusCode == 504 {
			t.consecutiveGateway.Add(1)
		}
	} else {
		t.consecutive5xx.Store(0)
		t.consecutiveGateway.Store(0)
		t.endpoint.Consecutive5xx.Store(0)
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
		if od.maxEjectionReached(destID, t.cfg) {
			return
		}
		t.ejected.Store(true)
		od.mu.Lock()
		t.ejectedAt = time.Now()
		t.ejectionCount++
		od.mu.Unlock()
		t.endpoint.mu.Lock()
		t.endpoint.Healthy = false
		t.endpoint.mu.Unlock()
		od.logger.Warn("outlier detection: ejecting endpoint",
			slog.String("destination", destID),
			slog.String("endpoint", t.endpoint.ID),
		)
	}
}

// maxEjectionReached returns true if ejecting another endpoint in this
// destination would exceed the configured MaxEjectionPercent.
func (od *OutlierDetector) maxEjectionReached(destID string, cfg *model.OutlierDetectionOptions) bool {
	if cfg.MaxEjectionPercent == 0 {
		return false
	}
	od.mu.Lock()
	defer od.mu.Unlock()

	total := 0
	ejected := 0
	for _, t := range od.trackers {
		if t.destID == destID {
			total++
			if t.ejected.Load() {
				ejected++
			}
		}
	}
	if total == 0 {
		return false
	}
	currentPercent := uint32((ejected + 1) * 100 / total)
	return currentPercent > cfg.MaxEjectionPercent
}

// Start begins the periodic check to un-eject endpoints.
func (od *OutlierDetector) Start(ctx context.Context) {
	ctx, od.cancel = context.WithCancel(ctx)

	go func() {
		interval := od.resolveInterval()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				od.checkEjections()
			case <-od.updateCh:
				newInterval := od.resolveInterval()
				if newInterval != interval {
					interval = newInterval
					ticker.Reset(interval)
				}
			}
		}
	}()
}

// resolveInterval returns the smallest configured outlier detection interval
// across all tracked destinations, defaulting to 10s.
func (od *OutlierDetector) resolveInterval() time.Duration {
	od.mu.Lock()
	defer od.mu.Unlock()

	minInterval := 10 * time.Second
	for _, t := range od.trackers {
		if t.cfg.Interval != "" {
			if d, err := time.ParseDuration(t.cfg.Interval); err == nil && d > 0 && d < minInterval {
				minInterval = d
			}
		}
	}
	return minInterval
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
			t.endpoint.mu.Lock()
			t.endpoint.Healthy = true
			t.endpoint.mu.Unlock()
			od.logger.Info("outlier detection: restoring endpoint",
				slog.String("id", id),
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
