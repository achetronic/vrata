// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package memory provides an in-memory session store for sticky sessions.
package memory

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	destID    string
	expiresAt time.Time
}

// Store implements session.Store using an in-memory map.
type Store struct {
	mu   sync.RWMutex
	data map[string]entry
	stop chan struct{}
}

// New creates a new in-memory session store.
func New() *Store {
	s := &Store{
		data: make(map[string]entry),
		stop: make(chan struct{}),
	}
	go s.cleanup()
	return s
}

// Get returns the destination ID for the given session+route pair.
func (s *Store) Get(ctx context.Context, sid, routeID string) (string, error) {
	key := routeID + ":" + sid
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.data[key]
	if !ok {
		return "", nil
	}
	if time.Now().After(e.expiresAt) {
		return "", nil
	}
	return e.destID, nil
}

// Set stores the mapping from session+route to destination.
func (s *Store) Set(ctx context.Context, sid, routeID, destID string, ttlSeconds int) error {
	key := routeID + ":" + sid
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = entry{
		destID:    destID,
		expiresAt: time.Now().Add(time.Duration(ttlSeconds) * time.Second),
	}
	return nil
}

// Close releases resources.
func (s *Store) Close() error {
	close(s.stop)
	return nil
}

func (s *Store) cleanup() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for k, v := range s.data {
				if now.After(v.expiresAt) {
					delete(s.data, k)
				}
			}
			s.mu.Unlock()
		}
	}
}
