// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

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
	// EndpointLBRoundRobin cycles through endpoints in order.
	EndpointLBRoundRobin EndpointLBPolicy = "ROUND_ROBIN"

	// EndpointLBLeastRequest picks the endpoint with the fewest active requests.
	EndpointLBLeastRequest EndpointLBPolicy = "LEAST_REQUEST"

	// EndpointLBRingHash uses consistent hashing for endpoint stickiness.
	EndpointLBRingHash EndpointLBPolicy = "RING_HASH"

	// EndpointLBMaglev uses Maglev consistent hashing for endpoint stickiness.
	EndpointLBMaglev EndpointLBPolicy = "MAGLEV"

	// EndpointLBRandom picks a random endpoint.
	EndpointLBRandom EndpointLBPolicy = "RANDOM"

	// EndpointLBSticky uses a session cookie and an external session store
	// (e.g. Redis) to guarantee zero disruption when endpoints change.
	// New clients are assigned via weighted random; existing clients always
	// return to the same endpoint until the cookie expires or the endpoint
	// is removed.
	EndpointLBSticky EndpointLBPolicy = "STICKY"
)

// TLSMode controls how Vrata connects to the upstream.
type TLSMode string

const (
	// TLSModeNone connects to the upstream in plaintext (default).
	TLSModeNone TLSMode = "none"

	// TLSModeTLS connects with TLS and verifies the server certificate.
	TLSModeTLS TLSMode = "tls"

	// TLSModeMTLS connects with mutual TLS, presenting a client certificate.
	TLSModeMTLS TLSMode = "mtls"
)

// Endpoint is a concrete network address (IP or hostname + port) that a
// Destination resolves to. A Destination has one or more endpoints:
//   - No Endpoints field and no Discovery → single implicit endpoint from Host:Port
//   - Endpoints field set → static list configured via the API
//   - Discovery configured → dynamic list resolved by the k8s watcher at runtime
type Endpoint struct {
	// Host is the endpoint IP address or hostname.
	Host string `json:"host" yaml:"host"`

	// Port is the endpoint TCP port.
	Port uint32 `json:"port" yaml:"port"`
}

// Destination is a named upstream target that routes reference by ID.
type Destination struct {
	// ID is the unique identifier of this destination.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label.
	Name string `json:"name" yaml:"name"`

	// Host is the default upstream FQDN or IP address. Used as the sole
	// endpoint when Endpoints is empty and no Discovery is configured.
	// For Kubernetes Services use the full FQDN:
	//   my-svc.my-namespace.svc.cluster.local
	Host string `json:"host" yaml:"host"`

	// Port is the default upstream TCP port. Used together with Host as
	// the sole endpoint when Endpoints is empty.
	Port uint32 `json:"port" yaml:"port"`

	// Endpoints is an explicit list of backend addresses for this Destination.
	// When set, Host:Port is ignored for traffic routing (Host is still used
	// for TLS SNI and k8s discovery FQDN parsing). The endpointBalancing
	// algorithm selects which endpoint receives each request.
	Endpoints []Endpoint `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`

	// Options contains advanced configuration. All fields are optional.
	Options *DestinationOptions `json:"options,omitempty" yaml:"options,omitempty"`
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
	// Timeouts controls how long each stage of the connection to this
	// upstream is allowed to take. When nil, sensible defaults are used.
	Timeouts *DestinationTimeouts `json:"timeouts,omitempty" yaml:"timeouts,omitempty"`

	// TLS controls upstream TLS / mTLS configuration.
	TLS *TLSOptions `json:"tls,omitempty" yaml:"tls,omitempty"`

	// EndpointBalancing controls how Vrata selects an endpoint within this
	// Destination when multiple endpoints are available (via Endpoints list
	// or Discovery). When nil, ROUND_ROBIN is used. When the Destination has
	// only one endpoint, the algorithm is irrelevant.
	EndpointBalancing *EndpointBalancing `json:"endpointBalancing,omitempty" yaml:"endpointBalancing,omitempty"`

	// CircuitBreaker limits in-flight traffic to protect the upstream.
	CircuitBreaker *CircuitBreakerOptions `json:"circuitBreaker,omitempty" yaml:"circuitBreaker,omitempty"`

	// HealthCheck configures active HTTP health checking.
	HealthCheck *HealthCheckOptions `json:"healthCheck,omitempty" yaml:"healthCheck,omitempty"`

	// OutlierDetection automatically ejects endpoints that return
	// consecutive errors, without requiring active health checks.
	OutlierDetection *OutlierDetectionOptions `json:"outlierDetection,omitempty" yaml:"outlierDetection,omitempty"`

	// Discovery enables dynamic endpoint resolution.
	// When nil, Vrata connects directly to host:port.
	Discovery *DestinationDiscovery `json:"discovery,omitempty" yaml:"discovery,omitempty"`

	// HTTP2 enables HTTP/2 to the upstream. Required for gRPC destinations.
	HTTP2 bool `json:"http2,omitempty" yaml:"http2,omitempty"`

	// MaxConnsPerHost limits the maximum number of simultaneous TCP connections
	// Vrata maintains to this destination. 0 means unlimited.
	MaxConnsPerHost uint32 `json:"maxConnsPerHost,omitempty" yaml:"maxConnsPerHost,omitempty"`
}

// DestinationTimeouts configures timeout durations for connections to an
// upstream destination. All fields accept Go duration strings (e.g. "5s", "200ms").
type DestinationTimeouts struct {
	// Request is the total budget for the entire HTTP call to this
	// destination — connect, TLS, send request, wait, receive response.
	// The absolute ceiling. Default: "30s".
	Request string `json:"request,omitempty" yaml:"request,omitempty"`

	// Connect is the maximum time to establish a TCP connection with
	// the endpoint. Default: "5s".
	Connect string `json:"connect,omitempty" yaml:"connect,omitempty"`

	// DualStackFallback is how long to wait before trying the other IP
	// family in parallel (IPv4↔IPv6, RFC 6555 Happy Eyeballs).
	// Default: "300ms".
	DualStackFallback string `json:"dualStackFallback,omitempty" yaml:"dualStackFallback,omitempty"`

	// TLSHandshake is the maximum time to complete the TLS handshake
	// after the TCP connection is established. Default: "5s".
	TLSHandshake string `json:"tlsHandshake,omitempty" yaml:"tlsHandshake,omitempty"`

	// ResponseHeader is the maximum time to wait for the upstream to
	// send the first byte of the response headers after the request
	// has been fully sent. Default: "10s".
	ResponseHeader string `json:"responseHeader,omitempty" yaml:"responseHeader,omitempty"`

	// ExpectContinue is the maximum time to wait for the upstream's
	// 100-Continue response before sending the request body. Only
	// applies to requests with Expect: 100-continue. Default: "1s".
	ExpectContinue string `json:"expectContinue,omitempty" yaml:"expectContinue,omitempty"`

	// IdleConnection is how long a reusable connection to this
	// destination stays idle in the pool before being closed.
	// Default: "90s".
	IdleConnection string `json:"idleConnection,omitempty" yaml:"idleConnection,omitempty"`
}

// EndpointBalancing controls how Vrata selects an endpoint within a Destination.
// The algorithm field selects the strategy; algorithm-specific parameters live
// in the corresponding nested struct (e.g. ringHash, maglev, leastRequest).
type EndpointBalancing struct {
	// Algorithm selects the endpoint load-balancing policy. Default: ROUND_ROBIN.
	Algorithm EndpointLBPolicy `json:"algorithm,omitempty" yaml:"algorithm,omitempty"`

	// RingHash holds parameters for the RING_HASH algorithm.
	// Only used when Algorithm is RING_HASH.
	RingHash *RingHashOptions `json:"ringHash,omitempty" yaml:"ringHash,omitempty"`

	// Maglev holds parameters for the MAGLEV algorithm.
	// Only used when Algorithm is MAGLEV.
	Maglev *MaglevOptions `json:"maglev,omitempty" yaml:"maglev,omitempty"`

	// LeastRequest holds parameters for the LEAST_REQUEST algorithm.
	// Only used when Algorithm is LEAST_REQUEST.
	LeastRequest *LeastRequestOptions `json:"leastRequest,omitempty" yaml:"leastRequest,omitempty"`

	// Sticky holds parameters for the STICKY algorithm.
	// Only used when Algorithm is STICKY.
	Sticky *EndpointStickyOptions `json:"sticky,omitempty" yaml:"sticky,omitempty"`
}

// EndpointStickyOptions configures zero-disruption endpoint pinning backed
// by an external session store (e.g. Redis). New clients are assigned via
// random; existing clients always return to the same endpoint.
type EndpointStickyOptions struct {
	// Cookie configures the session cookie used for client identification.
	Cookie *EndpointPinCookie `json:"cookie,omitempty" yaml:"cookie,omitempty"`
}

// EndpointPinCookie configures the session cookie for endpoint pinning.
type EndpointPinCookie struct {
	// Name is the cookie name. Default: "_vrata_endpoint_pin".
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// TTL is the lifetime of the session cookie. Accepts Go duration strings
	// (e.g. "1h", "24h"). Default: "1h".
	TTL string `json:"ttl,omitempty" yaml:"ttl,omitempty"`
}

// RingHashOptions configures the RING_HASH consistent hashing algorithm.
type RingHashOptions struct {
	// RingSize tunes the consistent hash ring.
	RingSize *RingSizeOptions `json:"ringSize,omitempty" yaml:"ringSize,omitempty"`

	// HashPolicy defines what request data feeds the hash function.
	// Entries are evaluated in order; the first one that produces a value wins.
	HashPolicy []HashPolicy `json:"hashPolicy,omitempty" yaml:"hashPolicy,omitempty"`
}

// MaglevOptions configures the MAGLEV consistent hashing algorithm.
type MaglevOptions struct {
	// TableSize sets the Maglev hash table size.
	// Must be a prime number. Default: 65537.
	TableSize uint64 `json:"tableSize,omitempty" yaml:"tableSize,omitempty"`

	// HashPolicy defines what request data feeds the hash function.
	// Entries are evaluated in order; the first one that produces a value wins.
	HashPolicy []HashPolicy `json:"hashPolicy,omitempty" yaml:"hashPolicy,omitempty"`
}

// LeastRequestOptions configures the LEAST_REQUEST algorithm.
type LeastRequestOptions struct {
	// ChoiceCount is the number of random choices to consider.
	// The endpoint with the fewest active requests among those chosen wins.
	// Default: 2 (power of two choices).
	ChoiceCount uint32 `json:"choiceCount,omitempty" yaml:"choiceCount,omitempty"`
}

// RingSizeOptions tunes the consistent-hashing ring for RING_HASH.
type RingSizeOptions struct {
	// Min is the minimum number of virtual nodes. Default: 1024.
	Min uint64 `json:"min,omitempty" yaml:"min,omitempty"`

	// Max is the maximum number of virtual nodes. Default: 8388608.
	Max uint64 `json:"max,omitempty" yaml:"max,omitempty"`
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
	Mode TLSMode `json:"mode" yaml:"mode"`

	// Cert is the PEM-encoded client certificate, or a {{secret:...}} reference.
	// Required when Mode is mtls.
	Cert string `json:"cert,omitempty" yaml:"cert,omitempty"`

	// Key is the PEM-encoded client private key, or a {{secret:...}} reference.
	// Required when Mode is mtls.
	Key string `json:"key,omitempty" yaml:"key,omitempty"`

	// CA is the PEM-encoded CA certificate, or a {{secret:...}} reference.
	// When empty, the system CA bundle is used.
	CA string `json:"ca,omitempty" yaml:"ca,omitempty"`

	// SNI overrides the Server Name Indication sent during TLS handshake.
	// When empty, the destination host is used.
	SNI string `json:"sni,omitempty" yaml:"sni,omitempty"`

	// MinVersion is the minimum TLS protocol version.
	// Accepted values: "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3".
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`

	// MaxVersion is the maximum TLS protocol version.
	MaxVersion string `json:"maxVersion,omitempty" yaml:"maxVersion,omitempty"`
}

// CircuitBreakerOptions limits in-flight traffic to the upstream and
// controls when the circuit opens after consecutive failures.
type CircuitBreakerOptions struct {
	// MaxConnections is the maximum number of concurrent TCP connections.
	MaxConnections uint32 `json:"maxConnections,omitempty" yaml:"maxConnections,omitempty"`

	// MaxPendingRequests is the maximum number of requests queued.
	MaxPendingRequests uint32 `json:"maxPendingRequests,omitempty" yaml:"maxPendingRequests,omitempty"`

	// MaxRequests is the maximum number of concurrent requests.
	MaxRequests uint32 `json:"maxRequests,omitempty" yaml:"maxRequests,omitempty"`

	// MaxRetries is the maximum number of concurrent retries.
	MaxRetries uint32 `json:"maxRetries,omitempty" yaml:"maxRetries,omitempty"`

	// FailureThreshold is the number of consecutive failures required to
	// open the circuit. Default: 5.
	FailureThreshold uint32 `json:"failureThreshold,omitempty" yaml:"failureThreshold,omitempty"`

	// OpenDuration is how long the circuit stays open before transitioning
	// to half-open and allowing a single probe request. Default: "30s".
	OpenDuration string `json:"openDuration,omitempty" yaml:"openDuration,omitempty"`
}

// HealthCheckOptions configures active HTTP health checking.
type HealthCheckOptions struct {
	// Path is the HTTP path for health-check requests. Required.
	Path string `json:"path" yaml:"path"`

	// Interval is how often health checks run. Default: "10s".
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`

	// Timeout is how long to wait for a response. Default: "5s".
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// UnhealthyThreshold is consecutive failures before marking unhealthy. Default: 3.
	UnhealthyThreshold uint32 `json:"unhealthyThreshold,omitempty" yaml:"unhealthyThreshold,omitempty"`

	// HealthyThreshold is consecutive successes before marking healthy. Default: 2.
	HealthyThreshold uint32 `json:"healthyThreshold,omitempty" yaml:"healthyThreshold,omitempty"`
}

// OutlierDetectionOptions ejects endpoints based on error patterns.
type OutlierDetectionOptions struct {
	// Consecutive5xx is consecutive 5xx responses that trigger ejection. Default: 5.
	Consecutive5xx uint32 `json:"consecutive5xx,omitempty" yaml:"consecutive5xx,omitempty"`

	// ConsecutiveGatewayErrors is consecutive 502/503/504 that trigger ejection.
	ConsecutiveGatewayErrors uint32 `json:"consecutiveGatewayErrors,omitempty" yaml:"consecutiveGatewayErrors,omitempty"`

	// Interval is how often ejection conditions are evaluated. Default: "10s".
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`

	// BaseEjectionTime is how long an endpoint stays ejected. Default: "30s".
	BaseEjectionTime string `json:"baseEjectionTime,omitempty" yaml:"baseEjectionTime,omitempty"`

	// MaxEjectionPercent is the maximum percentage of endpoints ejected. Default: 10.
	MaxEjectionPercent uint32 `json:"maxEjectionPercent,omitempty" yaml:"maxEjectionPercent,omitempty"`
}

// DestinationDiscovery enables dynamic endpoint resolution.
type DestinationDiscovery struct {
	// Type selects the discovery mechanism. Currently only "kubernetes".
	Type DiscoveryType `json:"type" yaml:"type"`
}

// DestinationRef references a Destination by ID and assigns a traffic weight.
type DestinationRef struct {
	// DestinationID is the ID of the Destination.
	DestinationID string `json:"destinationId" yaml:"destinationId"`

	// Weight controls the proportion of traffic. Must sum to 100 across
	// destinations when more than one is defined.
	Weight uint32 `json:"weight" yaml:"weight"`
}
