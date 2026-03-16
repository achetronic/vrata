// Package model defines the core domain types used throughout Rutoso.
package model

// Listener is a first-class entity that describes an Envoy listener:
// the address/port it binds to, the ordered set of HTTP filters active on it,
// and optional TLS termination config.
//
// HTTP filters are referenced by ID. The order of FilterIDs determines the
// order in which Envoy evaluates them in the filter chain. The router filter
// is always appended last automatically — do not include it here.
//
// Per-route and per-group filter behaviour is controlled via FilterOverrides
// on Route and RouteGroup respectively. When both levels carry an override for
// the same filter, the Route override wins (more specific takes precedence).
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

	// FilterIDs lists the IDs of Filter entities to activate on this listener,
	// in evaluation order. The router filter is always added last automatically.
	FilterIDs []string `json:"filterIds,omitempty" yaml:"filterIds,omitempty"`

	// TLS holds optional TLS termination configuration.
	// When nil, the listener operates in plaintext mode.
	// NOTE: TLS support is modelled here but not yet implemented in the xDS
	// builder. The field is accepted and stored; it has no effect until the
	// builder is updated to emit a DownstreamTlsContext.
	TLS *ListenerTLS `json:"tls,omitempty" yaml:"tls,omitempty"`

	// AccessLog configures access logging for this listener.
	// When nil, no access logs are emitted.
	// Maps to HttpConnectionManager.access_log.
	AccessLog *AccessLogConfig `json:"accessLog,omitempty" yaml:"accessLog,omitempty"`

	// HTTP2 enables HTTP/2 on this listener. Required for gRPC clients.
	// When true, the HCM codec type is set to AUTO (accepts both HTTP/1.1
	// and HTTP/2). When false, only HTTP/1.1 is accepted.
	// Maps to HttpConnectionManager.codec_type.
	HTTP2 bool `json:"http2,omitempty" yaml:"http2,omitempty"`

	// ListenerFilters are TCP-level filters evaluated before the HTTP
	// connection manager. Common examples: tls_inspector (required for
	// TLS/SNI), proxy_protocol (PROXY protocol v1/v2 from load balancers).
	// Maps to Listener.listener_filters.
	ListenerFilters []ListenerFilter `json:"listenerFilters,omitempty" yaml:"listenerFilters,omitempty"`

	// ServerName sets the "server" header Envoy adds to responses.
	// When empty, Envoy uses its default ("envoy").
	// Maps to HttpConnectionManager.server_name.
	ServerName string `json:"serverName,omitempty" yaml:"serverName,omitempty"`

	// MaxRequestHeadersKB limits the total size of request headers in
	// kilobytes. Requests exceeding this limit receive a 431 response.
	// Default: 60 (Envoy's default). Maps to HCM.max_request_headers_kb.
	MaxRequestHeadersKB uint32 `json:"maxRequestHeadersKB,omitempty" yaml:"maxRequestHeadersKB,omitempty"`
}

// ListenerTLS holds TLS termination parameters for a Listener.
// All fields are optional; Envoy applies sensible defaults for omitted values.
//
// NOTE: not yet implemented in the xDS builder — stored only.
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

// AccessLogConfig configures access logging for a Listener.
type AccessLogConfig struct {
	// Path is the file path where access logs are written.
	// Use "/dev/stdout" for container-friendly stdout logging.
	Path string `json:"path" yaml:"path"`

	// Format is the log line format using Envoy's command operator syntax.
	// When empty, Envoy uses its default format.
	// Example: "%REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% %PROTOCOL% %RESPONSE_CODE%"
	Format string `json:"format,omitempty" yaml:"format,omitempty"`

	// JSON switches the log output to JSON format. When true, Format is
	// interpreted as a JSON template (map of key to format string).
	// When false, Format is used as a plain text template.
	JSON bool `json:"json,omitempty" yaml:"json,omitempty"`
}

// ListenerFilterType identifies a TCP-level listener filter.
type ListenerFilterType string

const (
	// ListenerFilterTLSInspector detects TLS and extracts the SNI. Required
	// for TLS termination and SNI-based routing.
	ListenerFilterTLSInspector ListenerFilterType = "tls_inspector"

	// ListenerFilterProxyProtocol parses the PROXY protocol header (v1/v2)
	// to extract the real client IP from upstream load balancers.
	ListenerFilterProxyProtocol ListenerFilterType = "proxy_protocol"

	// ListenerFilterOriginalDst recovers the original destination address
	// for connections redirected by iptables REDIRECT / TPROXY.
	ListenerFilterOriginalDst ListenerFilterType = "original_dst"
)

// ListenerFilter represents a TCP-level filter applied before the HTTP
// connection manager. Maps to Listener.listener_filters entries.
type ListenerFilter struct {
	// Type selects the listener filter.
	Type ListenerFilterType `json:"type" yaml:"type"`
}
