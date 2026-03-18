package model

// DiscoveryType controls how Rutoso resolves endpoints for a Destination.
type DiscoveryType string

const (
	// DiscoveryTypeKubernetes makes Rutoso watch Kubernetes EndpointSlices
	// for the Service encoded in the Destination host FQDN, resolving
	// individual pod IPs for direct load balancing.
	DiscoveryTypeKubernetes DiscoveryType = "kubernetes"
)

// LBPolicy is the load-balancing algorithm used between endpoints.
type LBPolicy string

const (
	LBPolicyRoundRobin   LBPolicy = "ROUND_ROBIN"
	LBPolicyLeastRequest LBPolicy = "LEAST_REQUEST"
	LBPolicyRingHash     LBPolicy = "RING_HASH"
	LBPolicyMaglev       LBPolicy = "MAGLEV"
	LBPolicyRandom       LBPolicy = "RANDOM"
)

// TLSMode controls how Rutoso connects to the upstream.
type TLSMode string

const (
	TLSModeNone TLSMode = "none" // plaintext (default)
	TLSModeTLS  TLSMode = "tls"  // TLS — verify server certificate
	TLSModeMTLS TLSMode = "mtls" // mutual TLS — present client certificate
)

// Destination is a named upstream target that routes reference by ID.
type Destination struct {
	// ID is the unique identifier of this destination.
	ID string `json:"id"`

	// Name is a human-readable label.
	Name string `json:"name"`

	// Host is the upstream FQDN or IP address.
	// For Kubernetes Services use the full FQDN:
	//   my-svc.my-namespace.svc.cluster.local
	Host string `json:"host"`

	// Port is the upstream TCP port.
	Port uint32 `json:"port"`

	// Options contains advanced configuration. All fields are optional.
	Options *DestinationOptions `json:"options,omitempty"`
}

// DestinationOptions holds advanced configuration for a Destination.
type DestinationOptions struct {
	// ConnectTimeout is the timeout for establishing a new TCP connection.
	// Accepts Go duration strings (e.g. "3s", "500ms"). Default: 5s.
	ConnectTimeout string `json:"connectTimeout,omitempty"`

	// TLS controls upstream TLS / mTLS configuration.
	TLS *TLSOptions `json:"tls,omitempty"`

	// Balancing controls the load-balancing algorithm and its parameters.
	Balancing *BalancingOptions `json:"balancing,omitempty"`

	// CircuitBreaker limits in-flight traffic to protect the upstream.
	CircuitBreaker *CircuitBreakerOptions `json:"circuitBreaker,omitempty"`

	// HealthCheck configures active HTTP health checking.
	HealthCheck *HealthCheckOptions `json:"healthCheck,omitempty"`

	// OutlierDetection automatically ejects endpoints that return
	// consecutive errors, without requiring active health checks.
	OutlierDetection *OutlierDetectionOptions `json:"outlierDetection,omitempty"`

	// Discovery enables dynamic endpoint resolution.
	// When nil, Rutoso connects directly to host:port.
	Discovery *DestinationDiscovery `json:"discovery,omitempty"`

	// HTTP2 enables HTTP/2 to the upstream. Required for gRPC destinations.
	HTTP2 bool `json:"http2,omitempty"`

	// MaxRequestsPerConnection drains a connection after this many requests.
	// 0 means unlimited.
	MaxRequestsPerConnection uint32 `json:"maxRequestsPerConnection,omitempty"`
}

// TLSOptions configures upstream TLS.
type TLSOptions struct {
	// Mode selects the connection security model. Default: none (plaintext).
	Mode TLSMode `json:"mode"`

	// CertFile is the path to the client certificate PEM file.
	// Required when Mode is mtls.
	CertFile string `json:"certFile,omitempty"`

	// KeyFile is the path to the client private key PEM file.
	// Required when Mode is mtls.
	KeyFile string `json:"keyFile,omitempty"`

	// CAFile is the path to the CA certificate PEM file. When empty,
	// the system CA bundle (/etc/ssl/certs/ca-certificates.crt) is used.
	CAFile string `json:"caFile,omitempty"`

	// SNI overrides the Server Name Indication sent during TLS handshake.
	// When empty, the destination host is used.
	SNI string `json:"sni,omitempty"`

	// MinVersion is the minimum TLS protocol version.
	// Accepted values: "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3".
	MinVersion string `json:"minVersion,omitempty"`

	// MaxVersion is the maximum TLS protocol version.
	MaxVersion string `json:"maxVersion,omitempty"`
}

// BalancingOptions controls the load-balancing algorithm.
type BalancingOptions struct {
	// Algorithm selects the load-balancing policy. Default: ROUND_ROBIN.
	Algorithm LBPolicy `json:"algorithm,omitempty"`

	// RingSize tunes the consistent hash ring. Only used with RING_HASH.
	RingSize *RingSizeOptions `json:"ringSize,omitempty"`

	// MaglevTableSize sets the Maglev hash table size.
	// Must be a prime number. Default: 65537. Only used with MAGLEV.
	MaglevTableSize uint64 `json:"maglevTableSize,omitempty"`
}

// RingSizeOptions tunes the consistent-hashing ring for RING_HASH.
type RingSizeOptions struct {
	// Min is the minimum number of virtual nodes. Default: 1024.
	Min uint64 `json:"min,omitempty"`

	// Max is the maximum number of virtual nodes. Default: 8388608.
	Max uint64 `json:"max,omitempty"`
}

// CircuitBreakerOptions limits in-flight traffic to the upstream.
type CircuitBreakerOptions struct {
	// MaxConnections is the maximum number of concurrent TCP connections.
	MaxConnections uint32 `json:"maxConnections,omitempty"`

	// MaxPendingRequests is the maximum number of requests queued.
	MaxPendingRequests uint32 `json:"maxPendingRequests,omitempty"`

	// MaxRequests is the maximum number of concurrent requests.
	MaxRequests uint32 `json:"maxRequests,omitempty"`

	// MaxRetries is the maximum number of concurrent retries.
	MaxRetries uint32 `json:"maxRetries,omitempty"`
}

// HealthCheckOptions configures active HTTP health checking.
type HealthCheckOptions struct {
	// Path is the HTTP path for health-check requests. Required.
	Path string `json:"path"`

	// Interval is how often health checks run. Default: "10s".
	Interval string `json:"interval,omitempty"`

	// Timeout is how long to wait for a response. Default: "5s".
	Timeout string `json:"timeout,omitempty"`

	// UnhealthyThreshold is consecutive failures before marking unhealthy. Default: 3.
	UnhealthyThreshold uint32 `json:"unhealthyThreshold,omitempty"`

	// HealthyThreshold is consecutive successes before marking healthy. Default: 2.
	HealthyThreshold uint32 `json:"healthyThreshold,omitempty"`
}

// OutlierDetectionOptions ejects endpoints based on error patterns.
type OutlierDetectionOptions struct {
	// Consecutive5xx is consecutive 5xx responses that trigger ejection. Default: 5.
	Consecutive5xx uint32 `json:"consecutive5xx,omitempty"`

	// ConsecutiveGatewayErrors is consecutive 502/503/504 that trigger ejection.
	ConsecutiveGatewayErrors uint32 `json:"consecutiveGatewayErrors,omitempty"`

	// Interval is how often ejection conditions are evaluated. Default: "10s".
	Interval string `json:"interval,omitempty"`

	// BaseEjectionTime is how long an endpoint stays ejected. Default: "30s".
	BaseEjectionTime string `json:"baseEjectionTime,omitempty"`

	// MaxEjectionPercent is the maximum percentage of endpoints ejected. Default: 10.
	MaxEjectionPercent uint32 `json:"maxEjectionPercent,omitempty"`
}

// DestinationDiscovery enables dynamic endpoint resolution.
type DestinationDiscovery struct {
	// Type selects the discovery mechanism. Currently only "kubernetes".
	Type DiscoveryType `json:"type"`
}

// DestinationRef references a Destination by ID and assigns a traffic weight.
type DestinationRef struct {
	// DestinationID is the ID of the Destination.
	DestinationID string `json:"destinationId"`

	// Weight controls the proportion of traffic. Must sum to 100 across
	// destinations when more than one is defined.
	Weight uint32 `json:"weight"`
}

// HashPolicy defines how to compute the consistent hash key for sticky sessions.
// Lives on ForwardAction because it uses request attributes (headers, cookies, IP).
// The Destination defines the algorithm (RING_HASH/MAGLEV); the route defines
// what data feeds the hash.
type HashPolicy struct {
	// Header uses the named request header as the hash key.
	Header string `json:"header,omitempty" yaml:"header,omitempty"`

	// Cookie uses the named cookie as the hash key.
	Cookie string `json:"cookie,omitempty" yaml:"cookie,omitempty"`

	// CookieTTL is the TTL for auto-generated sticky cookies. Accepts Go durations.
	CookieTTL string `json:"cookieTtl,omitempty" yaml:"cookieTtl,omitempty"`

	// SourceIP uses the client IP as the hash key.
	SourceIP bool `json:"sourceIP,omitempty" yaml:"sourceIP,omitempty"`
}
