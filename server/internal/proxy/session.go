package proxy

import "context"

// SessionStore is the interface for sticky session backends used by the
// STICKY destination balancing algorithm. It maps (sid, routeID) to a
// destination ID.
type SessionStore interface {
	Get(ctx context.Context, sid, routeID string) (string, error)
	Set(ctx context.Context, sid, routeID, destID string, ttlSeconds int) error
}
