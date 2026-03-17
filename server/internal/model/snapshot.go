package model

import "time"

// Snapshot is a complete point-in-time view of all configuration entities.
// The control plane sends this to proxy-mode instances via SSE. The proxy
// applies it atomically — in-flight requests are never affected.
type Snapshot struct {
	// Listeners holds all proxy listener configurations.
	Listeners []Listener `json:"listeners" yaml:"listeners"`

	// Routes holds all routing rules.
	Routes []Route `json:"routes" yaml:"routes"`

	// Groups holds all route groups.
	Groups []RouteGroup `json:"groups" yaml:"groups"`

	// Destinations holds all upstream destination configurations.
	Destinations []Destination `json:"destinations" yaml:"destinations"`

	// Middlewares holds all middleware configurations.
	Middlewares []Middleware `json:"middlewares" yaml:"middlewares"`
}

// VersionedSnapshot wraps a Snapshot with versioning metadata. Stored in
// bbolt as an immutable, named configuration release. One versioned
// snapshot can be marked as "active" — the SSE stream serves that snapshot
// to all connected proxies.
type VersionedSnapshot struct {
	// ID is the unique identifier of this snapshot version.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label (e.g. "v1.2.0", "pre-deploy", "rollback-safe").
	Name string `json:"name" yaml:"name"`

	// CreatedAt is the UTC timestamp when this snapshot was captured.
	CreatedAt time.Time `json:"createdAt" yaml:"createdAt"`

	// Snapshot holds the full configuration at the time of capture.
	Snapshot Snapshot `json:"snapshot" yaml:"snapshot"`
}

// SnapshotSummary is a lightweight representation of a VersionedSnapshot
// returned by list endpoints. It omits the full snapshot payload to keep
// responses small.
type SnapshotSummary struct {
	// ID is the unique identifier of this snapshot version.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label.
	Name string `json:"name" yaml:"name"`

	// CreatedAt is the UTC timestamp when this snapshot was captured.
	CreatedAt time.Time `json:"createdAt" yaml:"createdAt"`

	// Active indicates whether this snapshot is the one currently being
	// served to proxies via the SSE stream.
	Active bool `json:"active" yaml:"active"`
}
