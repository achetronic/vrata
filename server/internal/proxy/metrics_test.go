package proxy

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/achetronic/vrata/internal/model"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

func boolPtr(b bool) *bool { return &b }

func TestMetricsCollectorNilConfig(t *testing.T) {
	mc := NewMetricsCollector(nil)
	if mc != nil {
		t.Fatal("expected nil collector for nil config")
	}
}

func TestMetricsCollectorRecordRoute(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	mc.RecordRoute("my-route", "my-group", "GET", 200, 15*time.Millisecond, 512, 1024)
	mc.RecordRoute("my-route", "my-group", "GET", 500, 100*time.Millisecond, 256, 0)

	val := counterValue(t, mc.routeRequests, "my-route", "my-group", "GET", "200", "2xx")
	if val != 1 {
		t.Errorf("expected 1 request with 200, got %v", val)
	}
	val = counterValue(t, mc.routeRequests, "my-route", "my-group", "GET", "500", "5xx")
	if val != 1 {
		t.Errorf("expected 1 request with 500, got %v", val)
	}
}

func TestMetricsCollectorRecordDestination(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	mc.RecordDestination("dest-1", 200, 10*time.Millisecond)
	mc.RecordDestination("dest-1", 503, 50*time.Millisecond)

	val := counterValue(t, mc.destRequests, "dest-1", "200", "2xx")
	if val != 1 {
		t.Errorf("expected 1, got %v", val)
	}
	val = counterValue(t, mc.destRequests, "dest-1", "503", "5xx")
	if val != 1 {
		t.Errorf("expected 1, got %v", val)
	}
}

func TestMetricsCollectorEndpointDisabledByDefault(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	if mc.epRequests != nil {
		t.Fatal("endpoint metrics should be nil by default (opt-in)")
	}

	mc.RecordEndpoint("dest-1", "10.0.0.1:8080", 200, 5*time.Millisecond)
}

func TestMetricsCollectorEndpointEnabled(t *testing.T) {
	cfg := &model.ListenerMetrics{
		Path: "/metrics",
		Collect: &model.MetricsCollectConfig{
			Endpoint: boolPtr(true),
		},
	}
	mc := NewMetricsCollector(cfg)

	mc.RecordEndpoint("dest-1", "10.0.0.1:8080", 200, 5*time.Millisecond)

	val := counterValue(t, mc.epRequests, "dest-1", "10.0.0.1:8080", "200", "2xx")
	if val != 1 {
		t.Errorf("expected 1, got %v", val)
	}
}

func TestMetricsCollectorMiddleware(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	mc.RecordMiddleware("jwt-auth", "jwt", 100*time.Microsecond, 200, true)
	mc.RecordMiddleware("jwt-auth", "jwt", 50*time.Microsecond, 401, false)

	passed := counterValue(t, mc.mwPassed, "jwt-auth", "jwt")
	if passed != 1 {
		t.Errorf("expected 1 passed, got %v", passed)
	}
	rejected := counterValue(t, mc.mwRejections, "jwt-auth", "jwt", "401")
	if rejected != 1 {
		t.Errorf("expected 1 rejected, got %v", rejected)
	}
}

func TestMetricsCollectorDisableSection(t *testing.T) {
	cfg := &model.ListenerMetrics{
		Path: "/metrics",
		Collect: &model.MetricsCollectConfig{
			Route:       boolPtr(false),
			Destination: boolPtr(false),
			Middleware:  boolPtr(false),
			Listener:    boolPtr(false),
		},
	}
	mc := NewMetricsCollector(cfg)

	if mc.routeRequests != nil {
		t.Error("route metrics should be nil when disabled")
	}
	if mc.destRequests != nil {
		t.Error("destination metrics should be nil when disabled")
	}
	if mc.mwDuration != nil {
		t.Error("middleware metrics should be nil when disabled")
	}
	if mc.listenerConnections != nil {
		t.Error("listener metrics should be nil when disabled")
	}

	mc.RecordRoute("r", "g", "GET", 200, time.Millisecond, 0, 0)
	mc.RecordDestination("d", 200, time.Millisecond)
	mc.RecordMiddleware("m", "t", time.Millisecond, 200, true)
	mc.RecordListenerConnection("l", ":8080")
}

func TestMetricsCollectorInflight(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	mc.RouteInflightInc("r", "g")
	mc.RouteInflightInc("r", "g")
	mc.RouteInflightDec("r", "g")

	val := gaugeValue(t, mc.routeInflight, "r", "g")
	if val != 1 {
		t.Errorf("expected inflight=1, got %v", val)
	}
}

func TestMetricsCollectorHandler(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	mc.RecordRoute("test-route", "test-group", "GET", 200, 5*time.Millisecond, 100, 200)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	mc.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if len(body) == 0 {
		t.Fatal("expected non-empty metrics output")
	}
}

func TestMetricsCollectorRetry(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	mc.RecordRetry("my-route", "my-group", 1)
	mc.RecordRetry("my-route", "my-group", 2)
	mc.RecordRetry("my-route", "my-group", 1)

	val := counterValue(t, mc.routeRetries, "my-route", "my-group", "1")
	if val != 2 {
		t.Errorf("expected 2 retries at attempt 1, got %v", val)
	}
	val = counterValue(t, mc.routeRetries, "my-route", "my-group", "2")
	if val != 1 {
		t.Errorf("expected 1 retry at attempt 2, got %v", val)
	}
}

func TestMetricsCollectorMirror(t *testing.T) {
	cfg := &model.ListenerMetrics{Path: "/metrics"}
	mc := NewMetricsCollector(cfg)

	mc.RecordMirror("my-route", "shadow-dest")
	mc.RecordMirror("my-route", "shadow-dest")

	val := counterValue(t, mc.routeMirrors, "my-route", "shadow-dest")
	if val != 2 {
		t.Errorf("expected 2 mirrors, got %v", val)
	}
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{100, "1xx"}, {200, "2xx"}, {301, "3xx"}, {404, "4xx"}, {500, "5xx"}, {503, "5xx"},
	}
	for _, tt := range tests {
		got := statusClass(tt.code)
		if got != tt.want {
			t.Errorf("statusClass(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func counterValue(t *testing.T, cv *prometheus.CounterVec, labels ...string) float64 {
	t.Helper()
	m := &io_prometheus_client.Metric{}
	if err := cv.WithLabelValues(labels...).Write(m); err != nil {
		t.Fatalf("writing metric: %v", err)
	}
	return m.GetCounter().GetValue()
}

func gaugeValue(t *testing.T, gv *prometheus.GaugeVec, labels ...string) float64 {
	t.Helper()
	m := &io_prometheus_client.Metric{}
	if err := gv.WithLabelValues(labels...).Write(m); err != nil {
		t.Fatalf("writing metric: %v", err)
	}
	return m.GetGauge().GetValue()
}
