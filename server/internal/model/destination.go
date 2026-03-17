package model

// DiscoveryType controls how Rutoso resolves endpoints for a Destination.
type DiscoveryType string

const (
	// DiscoveryTypeKubernetes makes Rutoso watch the Kubernetes EndpointSlices
	// of the Service encoded in the Destination host FQDN, enabling real EDS
	// and sticky-session support at the pod level.
	DiscoveryTypeKubernetes DiscoveryType = "kubernetes"
)

// LBPolicy is the load-balancing algorithm Envoy uses between endpoints.
type LBPolicy string

const (
	LBPolicyRoundRobin   LBPolicy = "ROUND_ROBIN"
	LBPolicyLeastRequest LBPolicy = "LEAST_REQUEST"
	LBPolicyRingHash     LBPolicy = "RING_HASH"
	LBPolicyMaglev       LBPolicy = "MAGLEV"
	LBPolicyRandom       LBPolicy = "RANDOM"
)

// TLSMode controls how Envoy connects to the upstream.
type TLSMode string

const (
	TLSModeNone TLSMode = "none" // plaintext (default)
	TLSModeTLS  TLSMode = "tls"  // TLS — verify server certificate
	TLSModeMTLS TLSMode = "mtls" // mutual TLS — present client certificate
)

// Destination is a named upstream target that routes reference by ID.
// It maps 1:1 to an Envoy Cluster. The cluster discovery type (STRICT_DNS,
// STATIC, EDS) is derived automatically from the host value and Options.Discovery.
type Destination struct {
	// ID is the unique identifier of this destination.
	ID string `json:"id"`

	// Name is a human-readable label.
	Name string `json:"name"`

	// Host is the upstream FQDN or IP address.
	// For Kubernetes Services use the full FQDN:
	//   pepe.default.svc.cluster.local
	// When Options.Discovery.Type is "kubernetes" the service name and
	// namespace are parsed from this field automatically.
	Host string `json:"host"`

	// Port is the upstream TCP port.
	Port uint32 `json:"port"`

	// Options contains advanced Envoy cluster configuration.
	// All fields are optional — sensible defaults are applied when omitted.
	Options *DestinationOptions `json:"options,omitempty"`
}

// DestinationOptions holds advanced configuration for a Destination.
// Each sub-struct maps directly to the corresponding Envoy Cluster proto field.
// All sub-structs are optional.
type DestinationOptions struct {
	// ConnectTimeout is the timeout for establishing a new TCP connection
	// to the upstream. Accepts Go duration strings (e.g. "3s", "500ms").
	// Maps to Cluster.connect_timeout. Default: 5s.
	ConnectTimeout string `json:"connectTimeout,omitempty"`

	// TLS controls upstream TLS / mTLS configuration.
	// Configures upstream TLS transport.
	TLS *TLSOptions `json:"tls,omitempty"`

	// Balancing controls the load-balancing algorithm and its parameters.
	// Maps to Cluster.lb_policy + ring_hash_lb_config / maglev_lb_config.
	Balancing *BalancingOptions `json:"balancing,omitempty"`

	// CircuitBreaker limits in-flight traffic to protect the upstream.
	// Maps to Cluster.circuit_breakers.thresholds.
	CircuitBreaker *CircuitBreakerOptions `json:"circuitBreaker,omitempty"`

	// HealthCheck configures active HTTP health checking against the upstream.
	// Maps to Cluster.health_checks.
	HealthCheck *HealthCheckOptions `json:"healthCheck,omitempty"`

	// OutlierDetection automatically ejects endpoints that return consecutive
	// errors, without requiring active health checks.
	// Maps to Cluster.outlier_detection.
	OutlierDetection *OutlierDetectionOptions `json:"outlierDetection,omitempty"`

	// Discovery enables dynamic endpoint resolution.
	// When nil, Rutoso derives the Envoy cluster type from the host value:
	//   IP address  → STATIC
	//   FQDN        → STRICT_DNS
	Discovery *DestinationDiscovery `json:"discovery,omitempty"`

	// HTTP2 enables HTTP/2 to the upstream. Required when the backend speaks
	// gRPC or HTTP/2. Maps to Cluster.typed_extension_protocol_options with
	// upstream HTTP protocol options.
	HTTP2 bool `json:"http2,omitempty"`

	// DNSRefreshRate controls how often STRICT_DNS clusters re-resolve the
	// host. Ignored for STATIC and EDS clusters. Accepts Go duration strings.
	// Default: "5s". Maps to Cluster.dns_refresh_rate.
	DNSRefreshRate string `json:"dnsRefreshRate,omitempty"`

	// DNSLookupFamily selects the IP version for DNS resolution.
	// Accepted values: "AUTO", "V4_ONLY", "V6_ONLY". Default: "AUTO".
	// Maps to Cluster.dns_lookup_family.
	DNSLookupFamily string `json:"dnsLookupFamily,omitempty"`

	// MaxRequestsPerConnection drains a connection to the upstream after this
	// many requests. 0 means unlimited. Useful for load balancing across new
	// pods. Maps to Cluster.max_requests_per_connection.
	MaxRequestsPerConnection uint32 `json:"maxRequestsPerConnection,omitempty"`

	// SlowStart configures gradual traffic ramp-up to new endpoints.
	// Helps avoid overwhelming a freshly-started pod with full traffic.
	// Maps to Cluster.slow_start_config.
	SlowStart *SlowStartOptions `json:"slowStart,omitempty"`
}

// TLSOptions configures the upstream TLS transport socket.
// Configures upstream TLS.
type TLSOptions struct {
	// Mode selects the connection security model.
	// Default: none (plaintext).
	Mode TLSMode `json:"mode"`

	// CertFile is the path to the client certificate PEM file.
	// Required when Mode is mtls.
	// Maps to common_tls_context.tls_certificates[0].certificate_chain.filename.
	CertFile string `json:"certFile,omitempty"`

	// KeyFile is the path to the client private key PEM file.
	// Required when Mode is mtls.
	// Maps to common_tls_context.tls_certificates[0].private_key.filename.
	KeyFile string `json:"keyFile,omitempty"`

	// CAFile is the path to the CA certificate PEM file used to verify
	// the server certificate. Applies to both tls and mtls modes.
	// Maps to common_tls_context.validation_context.trusted_ca.filename.
	CAFile string `json:"caFile,omitempty"`

	// SNI overrides the Server Name Indication sent during the TLS handshake.
	// When empty, Envoy uses the upstream host value.
	// Maps to UpstreamTlsContext.sni.
	SNI string `json:"sni,omitempty"`

	// MinVersion is the minimum TLS protocol version Envoy will negotiate.
	// Accepted values: "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3".
	// Maps to common_tls_context.tls_params.tls_minimum_protocol_version.
	MinVersion string `json:"minVersion,omitempty"`

	// MaxVersion is the maximum TLS protocol version Envoy will negotiate.
	// Accepted values: same as MinVersion.
	// Maps to common_tls_context.tls_params.tls_maximum_protocol_version.
	MaxVersion string `json:"maxVersion,omitempty"`
}

// BalancingOptions controls the Envoy load-balancing algorithm and its parameters.
type BalancingOptions struct {
	// Algorithm selects the load-balancing policy.
	// Default: ROUND_ROBIN.
	// Maps to Cluster.lb_policy.
	Algorithm LBPolicy `json:"algorithm,omitempty"`

	// RingSize tunes the consistent hash ring used by RING_HASH.
	// Ignored unless Algorithm is RING_HASH.
	// Maps to Cluster.ring_hash_lb_config.
	RingSize *RingSizeOptions `json:"ringSize,omitempty"`

	// MaglevTableSize sets the Maglev hash table size.
	// Must be a prime number. Default: 65537.
	// Ignored unless Algorithm is MAGLEV.
	// Maps to Cluster.maglev_lb_config.table_size.
	MaglevTableSize uint64 `json:"maglevTableSize,omitempty"`
}

// RingSizeOptions tunes the consistent-hashing ring for the RING_HASH policy.
// Maps to Cluster.ring_hash_lb_config.
type RingSizeOptions struct {
	// Min is the minimum number of virtual nodes in the ring.
	// Default: 1024. Maps to minimum_ring_size.
	Min uint64 `json:"min,omitempty"`

	// Max is the maximum number of virtual nodes in the ring.
	// Default: 8388608. Maps to maximum_ring_size.
	Max uint64 `json:"max,omitempty"`
}

// CircuitBreakerOptions limits in-flight traffic to the upstream.
// Maps to Cluster.circuit_breakers.thresholds[0] (DEFAULT priority).
type CircuitBreakerOptions struct {
	// MaxConnections is the maximum number of concurrent TCP connections.
	// Maps to max_connections.
	MaxConnections uint32 `json:"maxConnections,omitempty"`

	// MaxPendingRequests is the maximum number of requests queued while
	// waiting for a connection. Maps to max_pending_requests.
	MaxPendingRequests uint32 `json:"maxPendingRequests,omitempty"`

	// MaxRequests is the maximum number of concurrent requests.
	// Maps to max_requests.
	MaxRequests uint32 `json:"maxRequests,omitempty"`

	// MaxRetries is the maximum number of concurrent retries.
	// Maps to max_retries.
	MaxRetries uint32 `json:"maxRetries,omitempty"`
}

// HealthCheckOptions configures active HTTP health checking.
// Maps to Cluster.health_checks[0].
type HealthCheckOptions struct {
	// Path is the HTTP path Envoy sends health-check requests to.
	// Example: "/healthz". Required.
	// Maps to http_health_check.path.
	Path string `json:"path"`

	// Interval is how often Envoy sends a health-check request.
	// Accepts Go duration strings. Default: "10s".
	// Maps to interval.
	Interval string `json:"interval,omitempty"`

	// Timeout is how long Envoy waits for a health-check response.
	// Accepts Go duration strings. Default: "5s".
	// Maps to timeout.
	Timeout string `json:"timeout,omitempty"`

	// UnhealthyThreshold is the number of consecutive failures before an
	// endpoint is marked unhealthy. Default: 3.
	// Maps to unhealthy_threshold.
	UnhealthyThreshold uint32 `json:"unhealthyThreshold,omitempty"`

	// HealthyThreshold is the number of consecutive successes before an
	// unhealthy endpoint is returned to the pool. Default: 2.
	// Maps to healthy_threshold.
	HealthyThreshold uint32 `json:"healthyThreshold,omitempty"`
}

// OutlierDetectionOptions automatically ejects endpoints that return errors,
// without requiring active health checks.
// Maps to Cluster.outlier_detection.
type OutlierDetectionOptions struct {
	// Consecutive5xx is the number of consecutive 5xx responses that trigger
	// ejection. Default: 5. Maps to consecutive_5xx.
	Consecutive5xx uint32 `json:"consecutive5xx,omitempty"`

	// ConsecutiveGatewayErrors is the number of consecutive gateway errors
	// (502, 503, 504) that trigger ejection.
	// Maps to consecutive_gateway_failure.
	ConsecutiveGatewayErrors uint32 `json:"consecutiveGatewayErrors,omitempty"`

	// Interval is how often Envoy evaluates ejection conditions.
	// Accepts Go duration strings. Default: "10s".
	// Maps to interval.
	Interval string `json:"interval,omitempty"`

	// BaseEjectionTime is how long an endpoint stays ejected the first time.
	// Each subsequent ejection multiplies this value by the ejection count.
	// Accepts Go duration strings. Default: "30s".
	// Maps to base_ejection_time.
	BaseEjectionTime string `json:"baseEjectionTime,omitempty"`

	// MaxEjectionPercent is the maximum percentage of endpoints that can be
	// ejected simultaneously. Default: 10.
	// Maps to max_ejection_percent.
	MaxEjectionPercent uint32 `json:"maxEjectionPercent,omitempty"`
}

// SlowStartOptions configures gradual traffic ramp-up to new endpoints.
// During the slow-start window, Envoy applies a reduced weight to the endpoint
// that increases linearly until the window elapses.
// Maps to Cluster.slow_start_config.
type SlowStartOptions struct {
	// Window is how long the slow-start period lasts after an endpoint
	// becomes healthy. Accepts Go duration strings (e.g. "30s", "2m").
	// Maps to slow_start_window.
	Window string `json:"window" yaml:"window"`

	// Aggression controls how aggressively weight ramps up. Values > 1.0
	// produce a steeper ramp at the end; values < 1.0 ramp steeply at the
	// start. Default: 1.0 (linear). Maps to aggression.
	Aggression float64 `json:"aggression,omitempty" yaml:"aggression,omitempty"`
}

// DestinationDiscovery enables dynamic endpoint resolution beyond plain DNS.
type DestinationDiscovery struct {
	// Type selects the discovery mechanism.
	// Currently only "kubernetes" is supported.
	Type DiscoveryType `json:"type"`
}

// BackendRef references a Destination by ID and assigns a traffic weight.
type BackendRef struct {
	// DestinationID is the ID of the Destination this backend points to.
	DestinationID string `json:"destinationId"`

	// Weight controls the proportion of traffic sent to this Destination
	// when multiple backends are defined. Values across all BackendRefs in
	// a Route must sum to 100.
	Weight uint32 `json:"weight"`
}

// HashPolicy defines the key Envoy uses to compute the consistent hash for
// sticky sessions. Exactly one field should be set.
//
// This type lives alongside ForwardAction (not on Destination) because Envoy
// evaluates hash_policy at routing time, using request attributes (headers,
// cookies, client IP) that are only available in the RouteAction context.
// The Destination (Cluster) defines the algorithm (RING_HASH / MAGLEV) and
// ring parameters; the route defines what request data feeds the hash.
// Both sides must be configured for sticky sessions to work.
type HashPolicy struct {
	// Header uses the value of the named request header as the hash key.
	Header string `json:"header,omitempty" yaml:"header,omitempty"`

	// Cookie uses the named cookie as the hash key. If the cookie is absent
	// Envoy creates it with the given TTL (e.g. "3600s").
	Cookie string `json:"cookie,omitempty" yaml:"cookie,omitempty"`

	// CookieTTL is the TTL Envoy sets when generating a new sticky cookie.
	// Ignored unless Cookie is set. Accepts Go duration strings.
	CookieTTL string `json:"cookieTtl,omitempty" yaml:"cookieTtl,omitempty"`

	// SourceIP uses the downstream client IP as the hash key.
	SourceIP bool `json:"sourceIP,omitempty" yaml:"sourceIP,omitempty"`
}
