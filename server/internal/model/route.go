package model

// Route represents a single routing rule inside a RouteGroup.
// It defines how incoming requests are matched and which backends receive the traffic.
// A route can split traffic across multiple backends using weights (e.g. for canary deployments).
type Route struct {
	// ID is the unique identifier of the route within its group.
	ID string `json:"id" yaml:"id"`

	// GroupID is the identifier of the RouteGroup this route belongs to.
	GroupID string `json:"groupId" yaml:"groupId"`

	// Name is a human-readable label for the route.
	Name string `json:"name" yaml:"name"`

	// Description is an optional explanation of the route's purpose.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Match defines the conditions that a request must satisfy to be handled
	// by this route. Path, method, hostname, and header matchers are supported.
	Match MatchRule `json:"match" yaml:"match"`

	// Backends lists the upstream destinations for this route.
	// Weights across all backends should sum to 100. If only one backend is
	// provided its weight is ignored and all traffic goes to it.
	Backends []Backend `json:"backends" yaml:"backends"`
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
