// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package session provides a session store interface for the STICKY destination
// balancing algorithm. The store maps (sessionID, routeID) → destinationID.
package session

import "context"

// Store is the interface for sticky session backends.
type Store interface {
	// Get returns the destination ID for the given session+route pair.
	// Returns empty string if no mapping exists.
	Get(ctx context.Context, sid, routeID string) (string, error)

	// Set stores the mapping from session+route to destination.
	Set(ctx context.Context, sid, routeID, destID string, ttlSeconds int) error

	// Close releases any resources held by the store.
	Close() error
}
