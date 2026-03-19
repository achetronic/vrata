// Package model defines the core domain types used throughout Vrata.
package model

// Listener describes a network entry point where Vrata accepts HTTP traffic.
type Listener struct {
	// ID is the unique identifier of the listener.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the listener.
	Name string `json:"name" yaml:"name"`

	// Address is the IP address the listener binds to.
	// Defaults to "0.0.0.0" if empty.
	Address string `json:"address,omitempty" yaml:"address,omitempty"`

	// Port is the TCP port the listener binds to.
	Port uint32 `json:"port" yaml:"port"`

	// TLS holds optional TLS termination configuration.
	// When nil, the listener operates in plaintext mode.
	TLS *ListenerTLS `json:"tls,omitempty" yaml:"tls,omitempty"`

	// HTTP2 enables HTTP/2 on this listener. Required for gRPC clients.
	// With TLS, Go enables HTTP/2 automatically. Without TLS (h2c), Vrata
	// enables h2c upgrade support.
	HTTP2 bool `json:"http2,omitempty" yaml:"http2,omitempty"`

	// ServerName sets the "Server" response header.
	// When empty, no Server header is added.
	ServerName string `json:"serverName,omitempty" yaml:"serverName,omitempty"`

	// MaxRequestHeadersKB limits the total size of request headers in
	// kilobytes. Requests exceeding this limit receive a 431 response.
	// Default: 0 (no limit).
	MaxRequestHeadersKB uint32 `json:"maxRequestHeadersKB,omitempty" yaml:"maxRequestHeadersKB,omitempty"`

	// Metrics enables Prometheus metrics collection for traffic on this
	// listener. When nil, no metrics are collected.
	Metrics *ListenerMetrics `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// ListenerMetrics configures Prometheus metrics collection on a Listener.
// Presence of this struct activates metrics; there is no separate "enabled" field.
type ListenerMetrics struct {
	// Path is the HTTP path where the Prometheus scrape endpoint is served
	// on this listener. Default: "/metrics".
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// Collect controls which metric dimensions are collected. Each field
	// defaults to true when the entire Collect struct is nil, except
	// Endpoint which defaults to false (high cardinality, opt-in).
	Collect *MetricsCollectConfig `json:"collect,omitempty" yaml:"collect,omitempty"`

	// Histograms tunes histogram bucket boundaries. When nil, sensible
	// defaults are used.
	Histograms *MetricsHistogramConfig `json:"histograms,omitempty" yaml:"histograms,omitempty"`
}

// MetricsCollectConfig controls which metric dimensions are active.
type MetricsCollectConfig struct {
	// Route enables per-route metrics (requests, duration, retries, mirrors,
	// inflight, request/response size). Default: true.
	Route *bool `json:"route,omitempty" yaml:"route,omitempty"`

	// Destination enables per-destination metrics (requests, duration,
	// inflight). Default: true.
	Destination *bool `json:"destination,omitempty" yaml:"destination,omitempty"`

	// Endpoint enables per-endpoint metrics (requests, duration, health,
	// consecutive errors). Default: false (high cardinality).
	Endpoint *bool `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`

	// Middleware enables per-middleware metrics (duration, rejections,
	// passed count). Default: true.
	Middleware *bool `json:"middleware,omitempty" yaml:"middleware,omitempty"`

	// Listener enables listener-level metrics (connections, TLS errors).
	// Default: true.
	Listener *bool `json:"listener,omitempty" yaml:"listener,omitempty"`
}

// MetricsHistogramConfig tunes histogram bucket boundaries for metrics.
type MetricsHistogramConfig struct {
	// DurationBuckets are the upper bounds (in seconds) for duration
	// histograms. Default: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10].
	DurationBuckets []float64 `json:"durationBuckets,omitempty" yaml:"durationBuckets,omitempty"`

	// SizeBuckets are the upper bounds (in bytes) for request/response
	// size histograms. Default: [100, 1000, 10000, 100000, 1000000].
	SizeBuckets []float64 `json:"sizeBuckets,omitempty" yaml:"sizeBuckets,omitempty"`
}

// ResolvedPath returns the metrics scrape path, defaulting to "/metrics".
func (m *ListenerMetrics) ResolvedPath() string {
	if m.Path != "" {
		return m.Path
	}
	return "/metrics"
}

// CollectRoute returns whether route-level metrics are enabled.
func (m *ListenerMetrics) CollectRoute() bool {
	if m.Collect == nil || m.Collect.Route == nil {
		return true
	}
	return *m.Collect.Route
}

// CollectDestination returns whether destination-level metrics are enabled.
func (m *ListenerMetrics) CollectDestination() bool {
	if m.Collect == nil || m.Collect.Destination == nil {
		return true
	}
	return *m.Collect.Destination
}

// CollectEndpoint returns whether endpoint-level metrics are enabled.
func (m *ListenerMetrics) CollectEndpoint() bool {
	if m.Collect == nil || m.Collect.Endpoint == nil {
		return false
	}
	return *m.Collect.Endpoint
}

// CollectMiddleware returns whether middleware-level metrics are enabled.
func (m *ListenerMetrics) CollectMiddleware() bool {
	if m.Collect == nil || m.Collect.Middleware == nil {
		return true
	}
	return *m.Collect.Middleware
}

// CollectListener returns whether listener-level metrics are enabled.
func (m *ListenerMetrics) CollectListener() bool {
	if m.Collect == nil || m.Collect.Listener == nil {
		return true
	}
	return *m.Collect.Listener
}

// ResolvedDurationBuckets returns the duration histogram buckets.
func (m *ListenerMetrics) ResolvedDurationBuckets() []float64 {
	if m.Histograms != nil && len(m.Histograms.DurationBuckets) > 0 {
		return m.Histograms.DurationBuckets
	}
	return []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
}

// ResolvedSizeBuckets returns the size histogram buckets.
func (m *ListenerMetrics) ResolvedSizeBuckets() []float64 {
	if m.Histograms != nil && len(m.Histograms.SizeBuckets) > 0 {
		return m.Histograms.SizeBuckets
	}
	return []float64{100, 1000, 10000, 100000, 1000000}
}

// ListenerTLS holds TLS termination parameters for a Listener.
type ListenerTLS struct {
	// CertPath is the path to the PEM-encoded TLS certificate file.
	CertPath string `json:"certPath,omitempty" yaml:"certPath,omitempty"`

	// KeyPath is the path to the PEM-encoded private key file.
	KeyPath string `json:"keyPath,omitempty" yaml:"keyPath,omitempty"`

	// MinVersion is the minimum TLS protocol version to accept.
	// Accepted values: "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3".
	// Defaults to "TLSv1_2" if empty.
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`

	// MaxVersion is the maximum TLS protocol version to accept.
	// Accepted values: same as MinVersion. If empty, no upper bound is set.
	MaxVersion string `json:"maxVersion,omitempty" yaml:"maxVersion,omitempty"`
}
