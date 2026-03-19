// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package metrics provides Prometheus metrics for the controller.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all controller Prometheus metrics.
type Metrics struct {
	registry *prometheus.Registry

	// ReconcileDuration tracks how long each reconcile cycle takes.
	ReconcileDuration *prometheus.HistogramVec

	// ReconcileErrors counts reconcile errors by resource type.
	ReconcileErrors *prometheus.CounterVec

	// ReconcileTotal counts total reconcile operations by resource type and result.
	ReconcileTotal *prometheus.CounterVec

	// SnapshotsCreated counts snapshots created by the batcher.
	SnapshotsCreated prometheus.Counter

	// PendingChanges tracks the current number of unsnapshotted changes.
	PendingChanges prometheus.Gauge

	// OverlapsDetected counts route overlap detections.
	OverlapsDetected prometheus.Counter

	// OverlapsRejected counts routes rejected due to overlaps.
	OverlapsRejected prometheus.Counter

	// RefGrantDenied counts cross-namespace refs denied by missing ReferenceGrant.
	RefGrantDenied prometheus.Counter
}

// New creates and registers all controller metrics.
func New() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,
		ReconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "vrata_controller_reconcile_duration_seconds",
			Help:    "Duration of reconcile cycles.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"resource"}),
		ReconcileErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_controller_reconcile_errors_total",
			Help: "Total reconcile errors.",
		}, []string{"resource"}),
		ReconcileTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_controller_reconcile_total",
			Help: "Total reconcile operations.",
		}, []string{"resource", "result"}),
		SnapshotsCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "vrata_controller_snapshots_created_total",
			Help: "Total snapshots created.",
		}),
		PendingChanges: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vrata_controller_pending_changes",
			Help: "Current number of unsnapshotted changes.",
		}),
		OverlapsDetected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "vrata_controller_overlaps_detected_total",
			Help: "Total route overlaps detected.",
		}),
		OverlapsRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "vrata_controller_overlaps_rejected_total",
			Help: "Total routes rejected due to overlaps.",
		}),
		RefGrantDenied: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "vrata_controller_refgrant_denied_total",
			Help: "Total cross-namespace refs denied by missing ReferenceGrant.",
		}),
	}

	reg.MustRegister(
		m.ReconcileDuration, m.ReconcileErrors, m.ReconcileTotal,
		m.SnapshotsCreated, m.PendingChanges,
		m.OverlapsDetected, m.OverlapsRejected, m.RefGrantDenied,
	)

	return m
}

// Handler returns the HTTP handler that serves the Prometheus scrape endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
