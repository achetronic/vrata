package model

// Snapshot is a complete point-in-time view of all configuration entities.
// The control plane sends this to proxy-mode instances via SSE. The proxy
// applies it atomically — in-flight requests are never affected.
type Snapshot struct {
	Listeners    []Listener    `json:"listeners"`
	Routes       []Route       `json:"routes"`
	Groups       []RouteGroup  `json:"groups"`
	Destinations []Destination `json:"destinations"`
	Middlewares  []Middleware   `json:"middlewares"`
}
