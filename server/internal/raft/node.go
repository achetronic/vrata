// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package raft provides Raft-based replication for the Vrata control plane.
// This file implements the cluster node: peer discovery (static or DNS),
// Raft node lifecycle, and the TCP transport used for peer communication.
package raft

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"

	"github.com/achetronic/vrata/internal/config"
	boltstore "github.com/achetronic/vrata/internal/store/bolt"
)

const (
	raftTimeout        = 10 * time.Second
	snapshotRetain     = 2
	dnsRefreshInterval = 30 * time.Second
)

// Node is a single Raft participant in the control plane cluster.
type Node struct {
	raft        *raft.Raft
	cfg         *config.RaftConfig
	httpAddr    string
	logger      *slog.Logger
	logStore    *raftboltdb.BoltStore
	stableStore *raftboltdb.BoltStore
	transport   *raft.NetworkTransport
}

// NewNode creates and starts a Raft node. dataDir is the directory for
// Raft logs and snapshots. httpAddr is the HTTP address of this node
// (e.g. ":8080"), used to derive the leader's HTTP address when
// forwarding writes.
func NewNode(ctx context.Context, cfg *config.RaftConfig, dataDir string, store *boltstore.Store, logger *slog.Logger, httpAddr string) (*Node, error) {
	fsm := NewFSM(store)

	raftCfg := raft.DefaultConfig()
	raftCfg.LocalID = raft.ServerID(cfg.NodeID)
	raftCfg.Logger = newRaftLogger(logger)

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating raft data dir %q: %w", dataDir, err)
	}

	logStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft-log.db"))
	if err != nil {
		return nil, fmt.Errorf("opening raft log store: %w", err)
	}

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft-stable.db"))
	if err != nil {
		// Best-effort cleanup — we are already returning an error.
		_ = logStore.Close()
		return nil, fmt.Errorf("opening raft stable store: %w", err)
	}

	snapshotStore, err := raft.NewFileSnapshotStore(dataDir, snapshotRetain, newSlogWriter(logger))
	if err != nil {
		_ = logStore.Close()    // Best-effort cleanup
		_ = stableStore.Close() // Best-effort cleanup
		return nil, fmt.Errorf("creating raft snapshot store: %w", err)
	}

	var advertise net.Addr
	if cfg.AdvertiseAddress != "" {
		tcpAddr, err := net.ResolveTCPAddr("tcp", cfg.AdvertiseAddress)
		if err != nil {
			_ = logStore.Close()    // Best-effort cleanup
			_ = stableStore.Close() // Best-effort cleanup
			return nil, fmt.Errorf("resolving advertise address %q: %w", cfg.AdvertiseAddress, err)
		}
		advertise = tcpAddr
	}

	transport, err := raft.NewTCPTransport(cfg.BindAddress, advertise, 3, raftTimeout, newSlogWriter(logger))
	if err != nil {
		_ = logStore.Close()    // Best-effort cleanup
		_ = stableStore.Close() // Best-effort cleanup
		return nil, fmt.Errorf("creating raft transport on %s: %w", cfg.BindAddress, err)
	}

	hasState, err := raft.HasExistingState(logStore, stableStore, snapshotStore)
	if err != nil {
		_ = logStore.Close()    // Best-effort cleanup
		_ = stableStore.Close() // Best-effort cleanup
		_ = transport.Close()   // Best-effort cleanup
		return nil, fmt.Errorf("checking existing raft state: %w", err)
	}

	r, err := raft.NewRaft(raftCfg, fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		logStore.Close()
		stableStore.Close()
		transport.Close()
		return nil, fmt.Errorf("creating raft node: %w", err)
	}

	node := &Node{
		raft:        r,
		cfg:         cfg,
		httpAddr:    httpAddr,
		logger:      logger,
		logStore:    logStore,
		stableStore: stableStore,
		transport:   transport,
	}

	// Resolve peers and bootstrap if this is a fresh cluster.
	if !hasState {
		var peers []raft.Server
		// Retry peer resolution — in Kubernetes, DNS and other nodes may not
		// be ready immediately when this node starts.
		for attempt := 0; attempt < 30; attempt++ {
			var err error
			peers, err = node.resolvePeers(ctx)
			if err == nil && len(peers) > 0 {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
			logger.Info("raft: waiting for peers to be available",
				slog.Int("attempt", attempt+1),
				slog.String("error", fmt.Sprintf("%v", err)),
			)
		}
		if len(peers) == 0 {
			return nil, fmt.Errorf("could not resolve any peers after retries")
		}
		raftCfg2 := raft.Configuration{Servers: peers}
		future := r.BootstrapCluster(raftCfg2)
		if err := future.Error(); err != nil && err != raft.ErrCantBootstrap {
			return nil, fmt.Errorf("bootstrapping cluster: %w", err)
		}
	}

	// If using DNS discovery, refresh peers in the background.
	if cfg.Discovery != nil && cfg.Discovery.DNS != "" {
		go node.refreshPeersLoop(ctx)
	}

	return node, nil
}

// IsLeader returns true if this node is the current Raft leader.
func (n *Node) IsLeader() bool {
	return n.raft.State() == raft.Leader
}

// LeaderAddr returns the Raft address of the current leader.
func (n *Node) LeaderAddr() string {
	addr, _ := n.raft.LeaderWithID()
	return string(addr)
}

// LeaderHTTPAddr derives the leader's HTTP address from its Raft address.
// It replaces the Raft port with the HTTP port configured on this node.
// This works in homogeneous clusters where all nodes use the same HTTP port.
func (n *Node) LeaderHTTPAddr() string {
	leaderRaftAddr := n.LeaderAddr()
	if leaderRaftAddr == "" {
		return ""
	}
	leaderHost, _, err := net.SplitHostPort(leaderRaftAddr)
	if err != nil {
		return ""
	}
	_, httpPort, err := net.SplitHostPort(n.httpAddr)
	if err != nil {
		return ""
	}
	return net.JoinHostPort(leaderHost, httpPort)
}

// ApplyRaw applies a raw JSON-encoded Raft command. Only valid on the leader.
func (n *Node) ApplyRaw(data []byte) error {
	future := n.raft.Apply(data, raftTimeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("raft apply: %w", err)
	}
	if resp := future.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}
	return nil
}

// WaitForLeader blocks until a leader is elected or the timeout expires.
func (n *Node) WaitForLeader(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if n.LeaderAddr() != "" {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for raft leader")
}

// Shutdown gracefully stops the Raft node and releases all resources.
func (n *Node) Shutdown() error {
	if err := n.raft.Shutdown().Error(); err != nil {
		return err
	}
	n.transport.Close()
	n.logStore.Close()
	n.stableStore.Close()
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Peer discovery
// ─────────────────────────────────────────────────────────────────────────────

// resolvePeers returns the current list of cluster peers. Uses DNS discovery
// if configured, otherwise parses the static peers list.
func (n *Node) resolvePeers(ctx context.Context) ([]raft.Server, error) {
	if n.cfg.Discovery != nil && n.cfg.Discovery.DNS != "" {
		return n.resolveByDNS(ctx, n.cfg.Discovery.DNS)
	}
	return n.resolveStatic(n.cfg.Peers)
}

// resolveStatic parses the "nodeId=host:port" peer strings.
func (n *Node) resolveStatic(peers []string) ([]raft.Server, error) {
	var servers []raft.Server
	for _, p := range peers {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid peer format %q: expected nodeId=host:port", p)
		}
		servers = append(servers, raft.Server{
			ID:      raft.ServerID(strings.TrimSpace(parts[0])),
			Address: raft.ServerAddress(strings.TrimSpace(parts[1])),
		})
	}
	return servers, nil
}

// resolveByDNS looks up the given hostname and builds peer entries. The local
// node uses cfg.NodeID as its Raft server ID (stable across pod restarts in
// Kubernetes). Remote peers use their IP:port as ID for initial bootstrap;
// once the cluster has existing state, bootstrap is skipped entirely and the
// persisted Raft configuration is used instead.
func (n *Node) resolveByDNS(ctx context.Context, hostname string) ([]raft.Server, error) {
	_, port, err := net.SplitHostPort(n.cfg.BindAddress)
	if err != nil {
		return nil, fmt.Errorf("parsing bindAddress %q: %w", n.cfg.BindAddress, err)
	}

	// Resolve this node's own IP to recognise it in the DNS response.
	// Use advertise address if set (pod IP in k8s), otherwise parse bind address.
	localAddr := n.cfg.AdvertiseAddress
	if localAddr == "" {
		localAddr = n.cfg.BindAddress
	}
	localHost, _, _ := net.SplitHostPort(localAddr) //nolint: host is always valid from raft config
	// DNS failure is non-fatal — localIPs will be empty and the filter skips no peers.
	localIPs, _ := net.LookupHost(localHost)

	resolver := net.DefaultResolver
	addrs, err := resolver.LookupHost(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup %q: %w", hostname, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("DNS lookup %q returned no addresses", hostname)
	}

	localIPSet := make(map[string]bool, len(localIPs))
	for _, ip := range localIPs {
		localIPSet[ip] = true
	}

	var servers []raft.Server
	for _, addr := range addrs {
		peerAddr := net.JoinHostPort(addr, port)
		var serverID raft.ServerID
		if localIPSet[addr] {
			// This is the local node — use the stable nodeId from config.
			serverID = raft.ServerID(n.cfg.NodeID)
		} else {
			// Remote peer — use IP:port as a stable-enough ID for bootstrap.
			// Once the cluster has state, Raft uses the persisted config and
			// this ID is no longer relevant.
			serverID = raft.ServerID(peerAddr)
		}
		servers = append(servers, raft.Server{
			ID:      serverID,
			Address: raft.ServerAddress(peerAddr),
		})
	}

	n.logger.Info("raft: DNS discovery resolved peers",
		slog.String("hostname", hostname),
		slog.Int("count", len(servers)),
	)

	return servers, nil
}

// refreshPeersLoop periodically re-resolves DNS peers and adds any new
// nodes to the Raft configuration. Only the leader applies AddVoter calls;
// followers skip silently.
func (n *Node) refreshPeersLoop(ctx context.Context) {
	ticker := time.NewTicker(dnsRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !n.IsLeader() {
				continue
			}
			peers, err := n.resolveByDNS(ctx, n.cfg.Discovery.DNS)
			if err != nil {
				n.logger.Warn("raft: DNS refresh failed", slog.String("error", err.Error()))
				continue
			}
			// Build a set of currently resolved addresses to detect stale voters.
			resolvedAddrs := make(map[raft.ServerAddress]bool, len(peers))
			for _, peer := range peers {
				resolvedAddrs[peer.Address] = true
				future := n.raft.AddVoter(peer.ID, peer.Address, 0, raftTimeout)
				if err := future.Error(); err != nil {
					n.logger.Warn("raft: AddVoter failed",
						slog.String("id", string(peer.ID)),
						slog.String("error", err.Error()),
					)
				}
			}

			// Remove voters whose addresses are no longer in DNS.
			configFuture := n.raft.GetConfiguration()
			if err := configFuture.Error(); err != nil {
				n.logger.Warn("raft: failed to get configuration for stale voter cleanup",
					slog.String("error", err.Error()),
				)
			} else {
				for _, srv := range configFuture.Configuration().Servers {
					if srv.ID == raft.ServerID(n.cfg.NodeID) {
						continue
					}
					if !resolvedAddrs[srv.Address] {
						n.logger.Info("raft: removing stale voter",
							slog.String("id", string(srv.ID)),
							slog.String("address", string(srv.Address)),
						)
						remFuture := n.raft.RemoveServer(srv.ID, 0, raftTimeout)
						if err := remFuture.Error(); err != nil {
							n.logger.Warn("raft: RemoveServer failed",
								slog.String("id", string(srv.ID)),
								slog.String("error", err.Error()),
							)
						}
					}
				}
			}
		}
	}
}

