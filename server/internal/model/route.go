package model

// Route is an independent first-class entity. It describes how a specific set
// of HTTP requests should be matched and where traffic should be forwarded.
// Routes are not owned by a single group; they can be referenced by any number
// of RouteGroups via RouteGroup.RouteIDs.
type Route struct {
	// ID is the unique identifier of the route.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the route.
	Name string `json:"name" yaml:"name"`

	// Match defines the conditions that a request must satisfy.
	Match MatchRule `json:"match" yaml:"match"`

	// Backends lists the upstream destinations for this route.
	// Weights across all backends should sum to 100 when more than one backend
	// is defined. If only one backend is provided its weight is ignored.
	Backends []Backend `json:"backends" yaml:"backends"`

	// FilterOverrides carries per-route overrides for filters registered on
	// the listener. The map key is the Filter ID. When both the route's group
	// and the route itself carry an override for the same filter, the route
	// override wins entirely (more specific takes precedence).
	FilterOverrides map[string]FilterOverride `json:"filterOverrides,omitempty" yaml:"filterOverrides,omitempty"`
}

// Backend represents a single upstream destination for a route.
type Backend struct {
	// Name is a unique identifier for this backend within the route.
	// It maps to an Envoy cluster name.
	Name string `json:"name" yaml:"name"`

	// Host is the upstream hostname or IP address.
	Host string `json:"host" yaml:"host"`

	// Port is the upstream TCP port.
	Port uint32 `json:"port" yaml:"port"`

	// Weight controls what percentage of traffic is sent to this backend
	// when multiple backends are defined. Values across all backends in a
	// route must sum to 100.
	Weight uint32 `json:"weight" yaml:"weight"`

	// TLS indicates whether the connection to the upstream should use TLS.
	TLS bool `json:"tls,omitempty" yaml:"tls,omitempty"`
}
