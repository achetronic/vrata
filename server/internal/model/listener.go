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
