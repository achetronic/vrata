// Package xds implements the Envoy xDS control-plane server for Rutoso.
// It maintains a snapshot cache keyed by Envoy node ID and provides helpers
// to build and update snapshots from the domain model.
package xds

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Server wraps the go-control-plane gRPC xDS server and its snapshot cache.
type Server struct {
	cache          cachev3.SnapshotCache
	grpc           *grpc.Server
	logger         *slog.Logger
	version        atomic.Uint64
	lastSnapshot   cachev3.ResourceSnapshot
}

// nodeHash implements cachev3.NodeHash. It uses the node ID as the cache key
// so each Envoy instance receives its own snapshot.
type nodeHash struct{}

// ID returns the node ID from the core.Node, which is the canonical cache key.
func (nodeHash) ID(node *core.Node) string {
	if node == nil {
		return ""
	}
	return node.GetId()
}

// New creates a new xDS Server. The gRPC server is not started until Serve is called.
func New(logger *slog.Logger) *Server {
	s := &Server{logger: logger}

	s.cache = cachev3.NewSnapshotCache(false, nodeHash{}, nil)

	cb := &serverv3.CallbackFuncs{
		StreamRequestFunc: func(streamID int64, req *discoveryv3.DiscoveryRequest) error {
			if req.Node == nil || req.Node.Id == "" {
				return nil
			}
			nodeID := req.Node.Id
			if _, err := s.cache.GetSnapshot(nodeID); err != nil {
				// Node has no snapshot yet — push the last known one if available.
				if s.lastSnapshot != nil {
					if setErr := s.cache.SetSnapshot(context.Background(), nodeID, s.lastSnapshot); setErr != nil {
						logger.Warn("xds: failed to set snapshot for new node",
							slog.String("nodeId", nodeID),
							slog.String("error", setErr.Error()),
						)
					} else {
						logger.Info("xds: pushed snapshot to new node",
							slog.String("nodeId", nodeID),
						)
					}
				}
			}
			return nil
		},
	}

	grpcServer := grpc.NewServer()
	xdsSrv := serverv3.NewServer(context.Background(), s.cache, cb)

	discoveryv3.RegisterAggregatedDiscoveryServiceServer(grpcServer, xdsSrv)
	reflection.Register(grpcServer)

	s.grpc = grpcServer
	return s
}

// Cache returns the underlying snapshot cache so the gateway layer can push
// updated snapshots directly.
func (s *Server) Cache() cachev3.SnapshotCache {
	return s.cache
}

// Snapshot returns the last snapshot built by the gateway, regardless of
// whether any Envoy node has connected yet. Returns nil if no snapshot has
// been pushed yet.
func (s *Server) Snapshot() cachev3.ResourceSnapshot {
	return s.lastSnapshot
}

// SetLastSnapshot stores the most recently built snapshot for debug retrieval.
// Called by the gateway after every successful rebuild.
func (s *Server) SetLastSnapshot(snap cachev3.ResourceSnapshot) {
	s.lastSnapshot = snap
}

// NextVersion returns a monotonically incrementing version string for snapshots.
// go-control-plane rejects snapshots with the same version as the previous one,
// so this must be called for every snapshot update.
func (s *Server) NextVersion() string {
	return fmt.Sprintf("%d", s.version.Add(1))
}

// Serve starts the gRPC listener on addr and blocks until the context is cancelled
// or a fatal error occurs.
func (s *Server) Serve(ctx context.Context, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("xds: listening on %s: %w", addr, err)
	}

	s.logger.Info("xds server listening", slog.String("address", addr))

	errCh := make(chan error, 1)
	go func() {
		if err := s.grpc.Serve(lis); err != nil {
			errCh <- fmt.Errorf("xds: grpc serve: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		s.grpc.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}
