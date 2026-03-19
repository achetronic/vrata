// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetrics_Registration(t *testing.T) {
	m := New()
	if m.registry == nil {
		t.Fatal("registry should not be nil")
	}
}

func TestMetrics_RecordAndScrape(t *testing.T) {
	m := New()

	m.ReconcileTotal.WithLabelValues("httproute", "success").Inc()
	m.ReconcileTotal.WithLabelValues("httproute", "success").Inc()
	m.ReconcileErrors.WithLabelValues("httproute").Inc()
	m.ReconcileDuration.WithLabelValues("httproute").Observe(0.15)
	m.SnapshotsCreated.Inc()
	m.PendingChanges.Set(5)
	m.OverlapsDetected.Inc()
	m.OverlapsRejected.Inc()
	m.RefGrantDenied.Inc()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	checks := []string{
		"vrata_controller_reconcile_total",
		"vrata_controller_reconcile_errors_total",
		"vrata_controller_reconcile_duration_seconds",
		"vrata_controller_snapshots_created_total",
		"vrata_controller_pending_changes",
		"vrata_controller_overlaps_detected_total",
		"vrata_controller_overlaps_rejected_total",
		"vrata_controller_refgrant_denied_total",
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Errorf("metric %q not found in scrape output", c)
		}
	}
}
