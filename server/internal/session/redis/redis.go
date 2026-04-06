// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package redis implements the session store interface using Redis as the
// backend for STICKY destination and endpoint balancing.
package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Store implements session.Store using Redis as the backend.
type Store struct {
	client *goredis.Client
}

// New creates a Store connected to the given Redis address.
func New(addr, password string, db int) (*Store, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("pinging redis at %s: %w", addr, err)
	}
	return &Store{client: client}, nil
}

func key(sid, routeID string) string {
	return "vrata:sticky:" + sid + ":" + routeID
}

// Get returns the value for the given session+route pair.
func (s *Store) Get(ctx context.Context, sid, routeID string) (string, error) {
	val, err := s.client.Get(ctx, key(sid, routeID)).Result()
	if err == goredis.Nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("redis get %s: %w", key(sid, routeID), err)
	}
	return val, nil
}

// Set stores a value for the given session+route pair with a TTL.
func (s *Store) Set(ctx context.Context, sid, routeID, value string, ttlSeconds int) error {
	ttl := time.Duration(ttlSeconds) * time.Second
	if err := s.client.Set(ctx, key(sid, routeID), value, ttl).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key(sid, routeID), err)
	}
	return nil
}

// Close releases the Redis connection.
func (s *Store) Close() error {
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("closing redis connection: %w", err)
	}
	return nil
}
