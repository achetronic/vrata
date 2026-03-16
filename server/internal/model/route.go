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
	// Each entry references a Destination by ID and carries a traffic weight.
	// Weights across all backends should sum to 100 when more than one backend
	// is defined. If only one backend is provided its weight is ignored.
	Backends []BackendRef `json:"backends" yaml:"backends"`

	// FilterOverrides carries per-route overrides for filters registered on
	// the listener. The map key is the Filter ID. When both the route's group
	// and the route itself carry an override for the same filter, the route
	// override wins entirely (more specific takes precedence).
	FilterOverrides map[string]FilterOverride `json:"filterOverrides,omitempty" yaml:"filterOverrides,omitempty"`
}
