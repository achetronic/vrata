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
	mu       sync.Mutex
	trackers map[string]*outlierTracker
	logger   *slog.Logger
	cancel   context.CancelFunc
}

type outlierTracker struct {
	consecutive5xx     atomic.Int64
	consecutiveGateway atomic.Int64
	ejected            atomic.Bool
	ejectedAt          time.Time
	ejectionCount      int
	cfg                *model.OutlierDetectionOptions
	endpoint           *Endpoint
}

// NewOutlierDetector creates an OutlierDetector.
func NewOutlierDetector(logger *slog.Logger) *OutlierDetector {
	return &OutlierDetector{
		trackers: make(map[string]*outlierTracker),
		logger:   logger,
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
				}
			}
		}
	}
	od.trackers = newTrackers
}

// RecordResponse records the response status for outlier detection.
// destID is the Destination ID and epID is the endpoint ID (host:port).
func (od *OutlierDetector) RecordResponse(destID, epID string, statusCode int) {
	key := destID + "/" + epID
	od.mu.Lock()
	t, ok := od.trackers[key]
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

// Start begins the periodic check to un-eject endpoints. The loop ticks
// every second and evaluates each tracker against its configured interval.
func (od *OutlierDetector) Start(ctx context.Context) {
	ctx, od.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(time.Second)
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
