package model

// DiscoveryType controls how Vrata resolves endpoints for a Destination.
type DiscoveryType string

const (
	// DiscoveryTypeKubernetes makes Vrata watch Kubernetes EndpointSlices
	// for the Service encoded in the Destination host FQDN, resolving
	// individual pod IPs for direct load balancing.
	DiscoveryTypeKubernetes DiscoveryType = "kubernetes"
)

// EndpointLBPolicy is the load-balancing algorithm used between endpoints
// within a single Destination.
type EndpointLBPolicy string

const (
	EndpointLBRoundRobin   EndpointLBPolicy = "ROUND_ROBIN"
	EndpointLBLeastRequest EndpointLBPolicy = "LEAST_REQUEST"
	EndpointLBRingHash     EndpointLBPolicy = "RING_HASH"
	EndpointLBMaglev       EndpointLBPolicy = "MAGLEV"
	EndpointLBRandom       EndpointLBPolicy = "RANDOM"
)

// TLSMode controls how Vrata connects to the upstream.
type TLSMode string

const (
	TLSModeNone TLSMode = "none" // plaintext (default)
	TLSModeTLS  TLSMode = "tls"  // TLS — verify server certificate
	TLSModeMTLS TLSMode = "mtls" // mutual TLS — present client certificate
)

// Endpoint is a concrete network address (IP or hostname + port) that a
// Destination resolves to. A Destination has one or more endpoints:
//   - No Endpoints field and no Discovery → single implicit endpoint from Host:Port
//   - Endpoints field set → static list configured via the API
//   - Discovery configured → dynamic list resolved by the k8s watcher at runtime
type Endpoint struct {
	// Host is the endpoint IP address or hostname.
	Host string `json:"host"`

	// Port is the endpoint TCP port.
	Port uint32 `json:"port"`
}

// Destination is a named upstream target that routes reference by ID.
type Destination struct {
	// ID is the unique identifier of this destination.
	ID string `json:"id"`

	// Name is a human-readable label.
	Name string `json:"name"`

	// Host is the default upstream FQDN or IP address. Used as the sole
	// endpoint when Endpoints is empty and no Discovery is configured.
	// For Kubernetes Services use the full FQDN:
	//   my-svc.my-namespace.svc.cluster.local
	Host string `json:"host"`

	// Port is the default upstream TCP port. Used together with Host as
	// the sole endpoint when Endpoints is empty.
	Port uint32 `json:"port"`

	// Endpoints is an explicit list of backend addresses for this Destination.
	// When set, Host:Port is ignored for traffic routing (Host is still used
	// for TLS SNI and k8s discovery FQDN parsing). The endpointBalancing
	// algorithm selects which endpoint receives each request.
	Endpoints []Endpoint `json:"endpoints,omitempty"`

	// Options contains advanced configuration. All fields are optional.
	Options *DestinationOptions `json:"options,omitempty"`
}

// ResolvedEndpoints returns the effective endpoint list for this Destination.
// If Endpoints is set, returns it as-is. Otherwise returns a single-element
// list derived from Host:Port.
func (d Destination) ResolvedEndpoints() []Endpoint {
	if len(d.Endpoints) > 0 {
		return d.Endpoints
	}
	return []Endpoint{{Host: d.Host, Port: d.Port}}
}

// DestinationOptions holds advanced configuration for a Destination.
type DestinationOptions struct {
	// ConnectTimeout is the timeout for establishing a new TCP connection.
	// Accepts Go duration strings (e.g. "3s", "500ms"). Default: 5s.
	ConnectTimeout string `json:"connectTimeout,omitempty"`

	// TLS controls upstream TLS / mTLS configuration.
	TLS *TLSOptions `json:"tls,omitempty"`

	// EndpointBalancing controls how Vrata selects an endpoint within this
	// Destination when multiple endpoints are available (via Endpoints list
	// or Discovery). When nil, ROUND_ROBIN is used. When the Destination has
	// only one endpoint, the algorithm is irrelevant.
	EndpointBalancing *EndpointBalancing `json:"endpointBalancing,omitempty"`

	// CircuitBreaker limits in-flight traffic to protect the upstream.
	CircuitBreaker *CircuitBreakerOptions `json:"circuitBreaker,omitempty"`

	// HealthCheck configures active HTTP health checking.
	HealthCheck *HealthCheckOptions `json:"healthCheck,omitempty"`

	// OutlierDetection automatically ejects endpoints that return
	// consecutive errors, without requiring active health checks.
	OutlierDetection *OutlierDetectionOptions `json:"outlierDetection,omitempty"`

	// Discovery enables dynamic endpoint resolution.
	// When nil, Vrata connects directly to host:port.
	Discovery *DestinationDiscovery `json:"discovery,omitempty"`

	// HTTP2 enables HTTP/2 to the upstream. Required for gRPC destinations.
	HTTP2 bool `json:"http2,omitempty"`

	// MaxRequestsPerConnection drains a connection after this many requests.
	// 0 means unlimited.
	MaxRequestsPerConnection uint32 `json:"maxRequestsPerConnection,omitempty"`
}

// EndpointBalancing controls how Vrata selects an endpoint within a Destination.
// The algorithm field selects the strategy; algorithm-specific parameters live
// in the corresponding nested struct (e.g. ringHash, maglev, leastRequest).
type EndpointBalancing struct {
	// Algorithm selects the endpoint load-balancing policy. Default: ROUND_ROBIN.
	Algorithm EndpointLBPolicy `json:"algorithm,omitempty"`

	// RingHash holds parameters for the RING_HASH algorithm.
	// Only used when Algorithm is RING_HASH.
	RingHash *RingHashOptions `json:"ringHash,omitempty"`

	// Maglev holds parameters for the MAGLEV algorithm.
	// Only used when Algorithm is MAGLEV.
	Maglev *MaglevOptions `json:"maglev,omitempty"`

	// LeastRequest holds parameters for the LEAST_REQUEST algorithm.
	// Only used when Algorithm is LEAST_REQUEST.
	LeastRequest *LeastRequestOptions `json:"leastRequest,omitempty"`
}

// RingHashOptions configures the RING_HASH consistent hashing algorithm.
type RingHashOptions struct {
	// RingSize tunes the consistent hash ring.
	RingSize *RingSizeOptions `json:"ringSize,omitempty"`

	// HashPolicy defines what request data feeds the hash function.
	// Entries are evaluated in order; the first one that produces a value wins.
	HashPolicy []HashPolicy `json:"hashPolicy,omitempty"`
}

// MaglevOptions configures the MAGLEV consistent hashing algorithm.
type MaglevOptions struct {
	// TableSize sets the Maglev hash table size.
	// Must be a prime number. Default: 65537.
	TableSize uint64 `json:"tableSize,omitempty"`

	// HashPolicy defines what request data feeds the hash function.
	// Entries are evaluated in order; the first one that produces a value wins.
	HashPolicy []HashPolicy `json:"hashPolicy,omitempty"`
}

// LeastRequestOptions configures the LEAST_REQUEST algorithm.
type LeastRequestOptions struct {
	// ChoiceCount is the number of random choices to consider.
	// The endpoint with the fewest active requests among those chosen wins.
	// Default: 2 (power of two choices).
	ChoiceCount uint32 `json:"choiceCount,omitempty"`
}

// RingSizeOptions tunes the consistent-hashing ring for RING_HASH.
type RingSizeOptions struct {
	// Min is the minimum number of virtual nodes. Default: 1024.
	Min uint64 `json:"min,omitempty"`

	// Max is the maximum number of virtual nodes. Default: 8388608.
	Max uint64 `json:"max,omitempty"`
}

// HashPolicy defines how to compute the consistent hash key for endpoint
// stickiness. Each entry uses exactly one of Header, Cookie, or SourceIP.
// All fields are objects for consistency and future extensibility.
type HashPolicy struct {
	// Header uses the named request header as the hash key.
	Header *HashPolicyHeader `json:"header,omitempty" yaml:"header,omitempty"`

	// Cookie uses the named cookie as the hash key.
	// If the cookie does not exist, Vrata generates it with the given TTL.
	Cookie *HashPolicyCookie `json:"cookie,omitempty" yaml:"cookie,omitempty"`

	// SourceIP uses the client IP as the hash key.
	SourceIP *HashPolicySourceIP `json:"sourceIP,omitempty" yaml:"sourceIP,omitempty"`
}

// HashPolicyHeader hashes on a request header value.
type HashPolicyHeader struct {
	// Name is the header name to hash on.
	Name string `json:"name" yaml:"name"`
}

// HashPolicyCookie hashes on a cookie value. If the cookie is not present
// in the request, Vrata generates it and sets it on the response.
type HashPolicyCookie struct {
	// Name is the cookie name. Default: "_vrata_endpoint_pin".
	Name string `json:"name" yaml:"name"`

	// TTL is the lifetime of the auto-generated cookie.
	// Accepts Go duration strings (e.g. "1h"). Default: "1h".
	TTL string `json:"ttl,omitempty" yaml:"ttl,omitempty"`
}

// HashPolicySourceIP hashes on the client IP address.
type HashPolicySourceIP struct {
	// Enabled must be true to activate source IP hashing.
	Enabled bool `json:"enabled" yaml:"enabled"`
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
