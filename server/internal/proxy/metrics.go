package proxy

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/achetronic/vrata/internal/model"
)

// MetricsCollector accumulates Prometheus metrics for all proxy traffic.
// Each listener that enables metrics creates its own collector with an
// isolated prometheus.Registry. The collector is injected into the proxy
// pipeline via Dependencies and receives callbacks from every instrumented
// component.
type MetricsCollector struct {
	cfg      *model.ListenerMetrics
	registry *prometheus.Registry

	// Route-level metrics.
	routeRequests      *prometheus.CounterVec
	routeDuration      *prometheus.HistogramVec
	routeRequestBytes  *prometheus.CounterVec
	routeResponseBytes *prometheus.CounterVec
	routeInflight      *prometheus.GaugeVec
	routeRetries       *prometheus.CounterVec
	routeMirrors       *prometheus.CounterVec

	// Destination-level metrics.
	destRequests *prometheus.CounterVec
	destDuration *prometheus.HistogramVec
	destInflight *prometheus.GaugeVec

	// Endpoint-level metrics.
	epRequests       *prometheus.CounterVec
	epDuration       *prometheus.HistogramVec
	epHealthy        *prometheus.GaugeVec
	epConsecutive5xx *prometheus.GaugeVec

	// Destination infrastructure gauges.
	destCircuitState *prometheus.GaugeVec

	// Middleware-level metrics.
	mwDuration   *prometheus.HistogramVec
	mwRejections *prometheus.CounterVec
	mwPassed     *prometheus.CounterVec

	// Listener-level metrics.
	listenerConnections *prometheus.CounterVec
	listenerActive      *prometheus.GaugeVec
	listenerTLSErrors   *prometheus.CounterVec

	// Gauge scraper state.
	mu    sync.Mutex
	pools map[string]*DestinationPool
	stop  chan struct{}
}

// NewMetricsCollector creates a collector with metrics registered according
// to the listener's metrics configuration. Returns nil if cfg is nil.
func NewMetricsCollector(cfg *model.ListenerMetrics) *MetricsCollector {
	if cfg == nil {
		return nil
	}

	reg := prometheus.NewRegistry()
	mc := &MetricsCollector{
		cfg:      cfg,
		registry: reg,
		pools:    make(map[string]*DestinationPool),
		stop:     make(chan struct{}),
	}

	durationBuckets := cfg.ResolvedDurationBuckets()

	if cfg.CollectRoute() {
		mc.routeRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_route_requests_total",
			Help: "Total requests per route.",
		}, []string{"route", "group", "method", "status_code", "status_class"})
		mc.routeDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "vrata_route_duration_seconds",
			Help:    "Request duration per route (client perspective).",
			Buckets: durationBuckets,
		}, []string{"route", "group", "method"})
		mc.routeRequestBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_route_request_bytes_total",
			Help: "Total request bytes per route.",
		}, []string{"route", "group"})
		mc.routeResponseBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_route_response_bytes_total",
			Help: "Total response bytes per route.",
		}, []string{"route", "group"})
		mc.routeInflight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vrata_route_inflight_requests",
			Help: "Currently in-flight requests per route.",
		}, []string{"route", "group"})
		mc.routeRetries = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_route_retries_total",
			Help: "Total retries per route.",
		}, []string{"route", "group", "attempt"})
		mc.routeMirrors = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_mirror_requests_total",
			Help: "Total mirrored requests per route.",
		}, []string{"route", "destination"})

		reg.MustRegister(mc.routeRequests, mc.routeDuration, mc.routeRequestBytes,
			mc.routeResponseBytes, mc.routeInflight, mc.routeRetries, mc.routeMirrors)
	}

	if cfg.CollectDestination() {
		mc.destRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_destination_requests_total",
			Help: "Total requests per destination.",
		}, []string{"destination", "status_code", "status_class"})
		mc.destDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "vrata_destination_duration_seconds",
			Help:    "Upstream duration per destination.",
			Buckets: durationBuckets,
		}, []string{"destination"})
		mc.destInflight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vrata_destination_inflight_requests",
			Help: "Currently in-flight requests per destination.",
		}, []string{"destination"})
		mc.destCircuitState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vrata_destination_circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=open, 2=half_open).",
		}, []string{"destination"})

		reg.MustRegister(mc.destRequests, mc.destDuration, mc.destInflight, mc.destCircuitState)
	}

	if cfg.CollectEndpoint() {
		mc.epRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_endpoint_requests_total",
			Help: "Total requests per endpoint.",
		}, []string{"destination", "endpoint", "status_code", "status_class"})
		mc.epDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "vrata_endpoint_duration_seconds",
			Help:    "Upstream duration per endpoint.",
			Buckets: durationBuckets,
		}, []string{"destination", "endpoint"})
		mc.epHealthy = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vrata_endpoint_healthy",
			Help: "Endpoint health state (1=healthy, 0=ejected).",
		}, []string{"destination", "endpoint"})
		mc.epConsecutive5xx = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vrata_endpoint_consecutive_5xx",
			Help: "Consecutive 5xx responses from endpoint.",
		}, []string{"destination", "endpoint"})

		reg.MustRegister(mc.epRequests, mc.epDuration, mc.epHealthy, mc.epConsecutive5xx)
	}

	if cfg.CollectMiddleware() {
		mc.mwDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "vrata_middleware_duration_seconds",
			Help:    "Middleware processing duration.",
			Buckets: durationBuckets,
		}, []string{"middleware", "type"})
		mc.mwRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_middleware_rejections_total",
			Help: "Requests rejected by middleware.",
		}, []string{"middleware", "type", "status_code"})
		mc.mwPassed = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_middleware_passed_total",
			Help: "Requests that passed through middleware.",
		}, []string{"middleware", "type"})

		reg.MustRegister(mc.mwDuration, mc.mwRejections, mc.mwPassed)
	}

	if cfg.CollectListener() {
		mc.listenerConnections = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_listener_connections_total",
			Help: "Total connections per listener.",
		}, []string{"listener", "address"})
		mc.listenerActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "vrata_listener_active_connections",
			Help: "Active connections per listener.",
		}, []string{"listener", "address"})
		mc.listenerTLSErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vrata_listener_tls_handshake_errors_total",
			Help: "TLS handshake errors per listener.",
		}, []string{"listener", "address"})

		reg.MustRegister(mc.listenerConnections, mc.listenerActive, mc.listenerTLSErrors)
	}

	return mc
}

// Handler returns the HTTP handler that serves the Prometheus scrape endpoint.
func (mc *MetricsCollector) Handler() http.Handler {
	return promhttp.HandlerFor(mc.registry, promhttp.HandlerOpts{})
}

// UpdatePools replaces the pool snapshot used by the gauge scraper.
func (mc *MetricsCollector) UpdatePools(pools map[string]*DestinationPool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.pools = pools
}

// Start launches the background goroutine that scrapes gauge values
// (health, circuit breaker state) from the current pools.
func (mc *MetricsCollector) Start() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-mc.stop:
				return
			case <-ticker.C:
				mc.scrapeGauges()
			}
		}
	}()
}

// Stop terminates the background gauge scraper.
func (mc *MetricsCollector) Stop() {
	close(mc.stop)
}

func (mc *MetricsCollector) scrapeGauges() {
	mc.mu.Lock()
	pools := mc.pools
	mc.mu.Unlock()

	for _, pool := range pools {
		destID := pool.Destination.ID

		if mc.destCircuitState != nil && pool.CircuitBreaker != nil {
			state := float64(pool.CircuitBreaker.state.Load())
			mc.destCircuitState.WithLabelValues(destID).Set(state)
		}

		if mc.epHealthy != nil || mc.epConsecutive5xx != nil {
			for _, ep := range pool.Endpoints {
				epID := ep.ID
				if mc.epHealthy != nil {
					ep.mu.RLock()
					h := ep.Healthy
					ep.mu.RUnlock()
					val := float64(0)
					if h {
						val = 1
					}
					mc.epHealthy.WithLabelValues(destID, epID).Set(val)
				}
			}
		}
	}
}

// ─── Recording methods called by instrumented components ────────────────────

func statusClass(code int) string {
	switch {
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

// RecordRoute records a completed request at the route level.
func (mc *MetricsCollector) RecordRoute(route, group, method string, statusCode int, duration time.Duration, reqBytes, respBytes int64) {
	if mc.routeRequests == nil {
		return
	}
	sc := strconv.Itoa(statusCode)
	cls := statusClass(statusCode)
	mc.routeRequests.WithLabelValues(route, group, method, sc, cls).Inc()
	mc.routeDuration.WithLabelValues(route, group, method).Observe(duration.Seconds())
	mc.routeRequestBytes.WithLabelValues(route, group).Add(float64(reqBytes))
	mc.routeResponseBytes.WithLabelValues(route, group).Add(float64(respBytes))
}

// RouteInflightInc increments the in-flight gauge for a route.
func (mc *MetricsCollector) RouteInflightInc(route, group string) {
	if mc.routeInflight != nil {
		mc.routeInflight.WithLabelValues(route, group).Inc()
	}
}

// RouteInflightDec decrements the in-flight gauge for a route.
func (mc *MetricsCollector) RouteInflightDec(route, group string) {
	if mc.routeInflight != nil {
		mc.routeInflight.WithLabelValues(route, group).Dec()
	}
}

// RecordRetry records a retry attempt on a route.
func (mc *MetricsCollector) RecordRetry(route, group string, attempt int) {
	if mc.routeRetries != nil {
		mc.routeRetries.WithLabelValues(route, group, strconv.Itoa(attempt)).Inc()
	}
}

// RecordMirror records a mirrored request.
func (mc *MetricsCollector) RecordMirror(route, destination string) {
	if mc.routeMirrors != nil {
		mc.routeMirrors.WithLabelValues(route, destination).Inc()
	}
}

// RecordDestination records a completed request at the destination level.
func (mc *MetricsCollector) RecordDestination(destination string, statusCode int, duration time.Duration) {
	if mc.destRequests == nil {
		return
	}
	sc := strconv.Itoa(statusCode)
	cls := statusClass(statusCode)
	mc.destRequests.WithLabelValues(destination, sc, cls).Inc()
	mc.destDuration.WithLabelValues(destination).Observe(duration.Seconds())
}

// DestInflightInc increments the in-flight gauge for a destination.
func (mc *MetricsCollector) DestInflightInc(destination string) {
	if mc.destInflight != nil {
		mc.destInflight.WithLabelValues(destination).Inc()
	}
}

// DestInflightDec decrements the in-flight gauge for a destination.
func (mc *MetricsCollector) DestInflightDec(destination string) {
	if mc.destInflight != nil {
		mc.destInflight.WithLabelValues(destination).Dec()
	}
}

// RecordEndpoint records a completed request at the endpoint level.
func (mc *MetricsCollector) RecordEndpoint(destination, endpoint string, statusCode int, duration time.Duration) {
	if mc.epRequests == nil {
		return
	}
	sc := strconv.Itoa(statusCode)
	cls := statusClass(statusCode)
	mc.epRequests.WithLabelValues(destination, endpoint, sc, cls).Inc()
	mc.epDuration.WithLabelValues(destination, endpoint).Observe(duration.Seconds())
}

// RecordMiddleware records a middleware invocation.
func (mc *MetricsCollector) RecordMiddleware(name, mwType string, duration time.Duration, statusCode int, passed bool) {
	if mc.mwDuration == nil {
		return
	}
	mc.mwDuration.WithLabelValues(name, mwType).Observe(duration.Seconds())
	if passed {
		mc.mwPassed.WithLabelValues(name, mwType).Inc()
	} else {
		mc.mwRejections.WithLabelValues(name, mwType, strconv.Itoa(statusCode)).Inc()
	}
}

// RecordListenerConnection records a new connection on a listener.
func (mc *MetricsCollector) RecordListenerConnection(listener, address string) {
	if mc.listenerConnections != nil {
		mc.listenerConnections.WithLabelValues(listener, address).Inc()
	}
}

// ListenerActiveInc increments the active connections gauge.
func (mc *MetricsCollector) ListenerActiveInc(listener, address string) {
	if mc.listenerActive != nil {
		mc.listenerActive.WithLabelValues(listener, address).Inc()
	}
}

// ListenerActiveDec decrements the active connections gauge.
func (mc *MetricsCollector) ListenerActiveDec(listener, address string) {
	if mc.listenerActive != nil {
		mc.listenerActive.WithLabelValues(listener, address).Dec()
	}
}

// RecordTLSError records a TLS handshake error.
func (mc *MetricsCollector) RecordTLSError(listener, address string) {
	if mc.listenerTLSErrors != nil {
		mc.listenerTLSErrors.WithLabelValues(listener, address).Inc()
	}
}

// Path returns the configured metrics endpoint path.
func (mc *MetricsCollector) Path() string {
	return mc.cfg.ResolvedPath()
}

// FormatDestEndpoint formats an endpoint ID for metric labels.
func FormatDestEndpoint(host string, port uint32) string {
	return fmt.Sprintf("%s:%d", host, port)
}
