// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package raftstore provides a store.Store implementation backed by Raft
// consensus. Reads are served directly from the local bolt store. Writes
// are applied through the Raft log so that all cluster nodes receive them
// in the same order and converge to the same state.
//
// If a write arrives at a follower, it is forwarded transparently to the
// current Raft leader via HTTP. From the caller's perspective, every node
// accepts writes.
package raftstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/achetronic/vrata/internal/model"
	rft "github.com/achetronic/vrata/internal/raft"
	"github.com/achetronic/vrata/internal/store"
	boltstore "github.com/achetronic/vrata/internal/store/bolt"
)

// ErrNoLeader is returned when a write is attempted but no Raft leader is
// available (e.g. during an election or when the cluster has lost quorum).
var ErrNoLeader = errors.New("no raft leader available")

// Store wraps a bolt store with Raft consensus. All writes go through the
// Raft log; reads are served from the local bolt database.
type Store struct {
	local      *boltstore.Store
	node       *rft.Node
	httpClient *http.Client
}

// New creates a Raft-backed Store. The httpClient is used for write-forwarding
// from followers to the leader. Pass a TLS-configured client when the control
// plane uses HTTPS. If httpClient is nil, a default plaintext client is used.
func New(local *boltstore.Store, node *rft.Node, httpClient *http.Client) *Store {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Store{
		local:      local,
		node:       node,
		httpClient: httpClient,
	}
}

// ─── Reads — served from local bolt ─────────────────────────────────────────

// ListRoutes returns all routes from the local store.
func (s *Store) ListRoutes(ctx context.Context) ([]model.Route, error) {
	return s.local.ListRoutes(ctx)
}
// GetRoute returns a route by ID from the local store.
func (s *Store) GetRoute(ctx context.Context, id string) (model.Route, error) {
	return s.local.GetRoute(ctx, id)
}
// ListGroups returns all groups from the local store.
func (s *Store) ListGroups(ctx context.Context) ([]model.RouteGroup, error) {
	return s.local.ListGroups(ctx)
}
// GetGroup returns a group by ID from the local store.
func (s *Store) GetGroup(ctx context.Context, id string) (model.RouteGroup, error) {
	return s.local.GetGroup(ctx, id)
}
// ListMiddlewares returns all middlewares from the local store.
func (s *Store) ListMiddlewares(ctx context.Context) ([]model.Middleware, error) {
	return s.local.ListMiddlewares(ctx)
}
// GetMiddleware returns a middleware by ID from the local store.
func (s *Store) GetMiddleware(ctx context.Context, id string) (model.Middleware, error) {
	return s.local.GetMiddleware(ctx, id)
}
// ListListeners returns all listeners from the local store.
func (s *Store) ListListeners(ctx context.Context) ([]model.Listener, error) {
	return s.local.ListListeners(ctx)
}
// GetListener returns a listener by ID from the local store.
func (s *Store) GetListener(ctx context.Context, id string) (model.Listener, error) {
	return s.local.GetListener(ctx, id)
}
// ListDestinations returns all destinations from the local store.
func (s *Store) ListDestinations(ctx context.Context) ([]model.Destination, error) {
	return s.local.ListDestinations(ctx)
}
// GetDestination returns a destination by ID from the local store.
func (s *Store) GetDestination(ctx context.Context, id string) (model.Destination, error) {
	return s.local.GetDestination(ctx, id)
}
// ListSecrets returns summary metadata for all secrets from the local store.
func (s *Store) ListSecrets(ctx context.Context) ([]model.SecretSummary, error) {
	return s.local.ListSecrets(ctx)
}
// GetSecret returns a secret by ID from the local store.
func (s *Store) GetSecret(ctx context.Context, id string) (model.Secret, error) {
	return s.local.GetSecret(ctx, id)
}
// ListSnapshots returns summary metadata for all snapshots from the local store.
func (s *Store) ListSnapshots(ctx context.Context) ([]model.SnapshotSummary, error) {
	return s.local.ListSnapshots(ctx)
}
// GetSnapshot returns a snapshot by ID from the local store.
func (s *Store) GetSnapshot(ctx context.Context, id string) (model.VersionedSnapshot, error) {
	return s.local.GetSnapshot(ctx, id)
}
// GetActiveSnapshot returns the active snapshot from the local store.
func (s *Store) GetActiveSnapshot(ctx context.Context) (model.VersionedSnapshot, error) {
	return s.local.GetActiveSnapshot(ctx)
}
// Subscribe returns a channel of store events from the local store.
func (s *Store) Subscribe(ctx context.Context) (<-chan store.StoreEvent, error) {
	return s.local.Subscribe(ctx)
}

// ─── Writes — go through Raft ────────────────────────────────────────────────

// SaveRoute creates or replaces a route via the Raft log.
func (s *Store) SaveRoute(ctx context.Context, v model.Route) error {
	return s.apply(ctx, rft.CmdSaveRoute, v.ID, v)
}
// DeleteRoute removes a route via the Raft log.
func (s *Store) DeleteRoute(ctx context.Context, id string) error {
	return s.apply(ctx, rft.CmdDeleteRoute, id, nil)
}
// SaveGroup creates or replaces a group via the Raft log.
func (s *Store) SaveGroup(ctx context.Context, v model.RouteGroup) error {
	return s.apply(ctx, rft.CmdSaveGroup, v.ID, v)
}
// DeleteGroup removes a group via the Raft log.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	return s.apply(ctx, rft.CmdDeleteGroup, id, nil)
}
// SaveMiddleware creates or replaces a middleware via the Raft log.
func (s *Store) SaveMiddleware(ctx context.Context, v model.Middleware) error {
	return s.apply(ctx, rft.CmdSaveMiddleware, v.ID, v)
}
// DeleteMiddleware removes a middleware via the Raft log.
func (s *Store) DeleteMiddleware(ctx context.Context, id string) error {
	return s.apply(ctx, rft.CmdDeleteMiddleware, id, nil)
}
// SaveListener creates or replaces a listener via the Raft log.
func (s *Store) SaveListener(ctx context.Context, v model.Listener) error {
	return s.apply(ctx, rft.CmdSaveListener, v.ID, v)
}
// DeleteListener removes a listener via the Raft log.
func (s *Store) DeleteListener(ctx context.Context, id string) error {
	return s.apply(ctx, rft.CmdDeleteListener, id, nil)
}
// SaveDestination creates or replaces a destination via the Raft log.
func (s *Store) SaveDestination(ctx context.Context, v model.Destination) error {
	return s.apply(ctx, rft.CmdSaveDestination, v.ID, v)
}
// DeleteDestination removes a destination via the Raft log.
func (s *Store) DeleteDestination(ctx context.Context, id string) error {
	return s.apply(ctx, rft.CmdDeleteDestination, id, nil)
}
// SaveSecret creates or replaces a secret via the Raft log.
func (s *Store) SaveSecret(ctx context.Context, v model.Secret) error {
	return s.apply(ctx, rft.CmdSaveSecret, v.ID, v)
}
// DeleteSecret removes a secret via the Raft log.
func (s *Store) DeleteSecret(ctx context.Context, id string) error {
	return s.apply(ctx, rft.CmdDeleteSecret, id, nil)
}
// SaveSnapshot creates or replaces a versioned snapshot via the Raft log.
func (s *Store) SaveSnapshot(ctx context.Context, v model.VersionedSnapshot) error {
	return s.apply(ctx, rft.CmdSaveSnapshot, v.ID, v)
}
// DeleteSnapshot removes a versioned snapshot via the Raft log.
func (s *Store) DeleteSnapshot(ctx context.Context, id string) error {
	return s.apply(ctx, rft.CmdDeleteSnapshot, id, nil)
}
// ActivateSnapshot sets the active snapshot via the Raft log.
func (s *Store) ActivateSnapshot(ctx context.Context, id string) error {
	return s.apply(ctx, rft.CmdActivateSnapshot, id, nil)
}

// ─── apply: forward to leader or commit locally ──────────────────────────────

// apply encodes a command and applies it through the Raft log. If this node
// is not the leader, it forwards the command to the leader transparently.
func (s *Store) apply(ctx context.Context, cmdType rft.CommandType, id string, payload interface{}) error {
	var rawPayload json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshalling payload: %w", err)
		}
		rawPayload = data
	}

	cmd := rft.Command{Type: cmdType, ID: id, Payload: rawPayload}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshalling command: %w", err)
	}

	if s.node.IsLeader() {
		return s.node.ApplyRaw(data)
	}
	return s.forwardToLeader(ctx, data)
}

// forwardToLeader forwards a command to the current Raft leader over HTTP.
func (s *Store) forwardToLeader(ctx context.Context, data []byte) error {
	leaderHTTP := s.node.LeaderHTTPAddr()
	if leaderHTTP == "" {
		return ErrNoLeader
	}

	scheme := "http"
	if s.httpClient.Transport != nil {
		if t, ok := s.httpClient.Transport.(*http.Transport); ok && t.TLSClientConfig != nil {
			scheme = "https"
		}
	}
	url := fmt.Sprintf("%s://%s/api/v1/sync/raft", scheme, leaderHTTP)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("building forward request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("forwarding to leader %s: %w", leaderHTTP, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Best-effort body read for error context — not critical if it fails.
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("leader returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// Verify Store implements store.Store at compile time.
var _ store.Store = (*Store)(nil)
