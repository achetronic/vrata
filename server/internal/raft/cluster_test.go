// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package raft_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	raftnode "github.com/achetronic/vrata/internal/raft"
	"github.com/achetronic/vrata/internal/config"
	boltstore "github.com/achetronic/vrata/internal/store/bolt"
	"github.com/achetronic/vrata/internal/store/raftstore"
	"github.com/achetronic/vrata/internal/model"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func waitLeader(t *testing.T, node *raftnode.Node) {
	t.Helper()
	if err := node.WaitForLeader(10 * time.Second); err != nil {
		t.Fatal(err)
	}
}

// TestClusterSingleNode verifies that a single-node cluster can apply
// writes and read them back.
func TestClusterSingleNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := t.TempDir()
	st, err := boltstore.New(dir + "/store.db", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	port := freePort(t)
	bindAddr := fmt.Sprintf("127.0.0.1:%d", port)

	cfg := &config.RaftConfig{
		NodeID:      "solo",
		BindAddress: bindAddr,
		Peers:       []string{fmt.Sprintf("solo=%s", bindAddr)},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	n, err := raftnode.NewNode(ctx, cfg, dir, st, logger, ":8080")
	if err != nil {
		t.Fatal(err)
	}
	defer n.Shutdown()

	waitLeader(t, n)
	rs := raftstore.New(st, n)

	route := model.Route{
		ID: "r1", Name: "cluster-route",
		Match:          model.MatchRule{PathPrefix: "/"},
		DirectResponse: &model.RouteDirectResponse{Status: 200},
	}
	if err := rs.SaveRoute(ctx, route); err != nil {
		t.Fatalf("SaveRoute: %v", err)
	}

	got, err := rs.GetRoute(ctx, "r1")
	if err != nil || got.Name != "cluster-route" {
		t.Errorf("GetRoute: %v %v", got.Name, err)
	}
}

// TestClusterThreeNodesReplication verifies that a write on one node
// is replicated to the others.
func TestClusterThreeNodesReplication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Allocate ports first.
	p0, p1, p2 := freePort(t), freePort(t), freePort(t)
	a0 := fmt.Sprintf("127.0.0.1:%d", p0)
	a1 := fmt.Sprintf("127.0.0.1:%d", p1)
	a2 := fmt.Sprintf("127.0.0.1:%d", p2)
	peers := []string{
		fmt.Sprintf("n0=%s", a0),
		fmt.Sprintf("n1=%s", a1),
		fmt.Sprintf("n2=%s", a2),
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	newNode := func(id, addr string) (*raftnode.Node, *boltstore.Store) {
		dir := t.TempDir()
		st, _ := boltstore.New(dir + "/store.db", nil)
		t.Cleanup(func() { st.Close() })
		cfg := &config.RaftConfig{NodeID: id, BindAddress: addr, Peers: peers}
		n, err := raftnode.NewNode(ctx, cfg, dir, st, logger, ":8080")
		if err != nil {
			t.Fatalf("NewNode %s: %v", id, err)
		}
		t.Cleanup(func() { n.Shutdown() })
		return n, st
	}

	n0, st0 := newNode("n0", a0)
	n1, st1 := newNode("n1", a1)
	n2, st2 := newNode("n2", a2)

	// Wait for any node to become leader.
	deadline := time.After(15 * time.Second)
	var leader *raftnode.Node
	var leaderStore *boltstore.Store
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for leader")
		default:
		}
		switch {
		case n0.IsLeader():
			leader, leaderStore = n0, st0
		case n1.IsLeader():
			leader, leaderStore = n1, st1
		case n2.IsLeader():
			leader, leaderStore = n2, st2
		}
		if leader != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	_ = leaderStore

	// Write via the leader.
	rs := raftstore.New(leaderStore, leader)
	route := model.Route{
		ID: "rep-route", Name: "replicated",
		Match:       model.MatchRule{PathPrefix: "/rep"},
		DirectResponse: &model.RouteDirectResponse{Status: 201},
	}
	if err := rs.SaveRoute(ctx, route); err != nil {
		t.Fatalf("SaveRoute on leader: %v", err)
	}

	// Wait for replication.
	time.Sleep(500 * time.Millisecond)

	// Read from all three local stores.
	for i, st := range []*boltstore.Store{st0, st1, st2} {
		got, err := st.GetRoute(ctx, "rep-route")
		if err != nil || got.Name != "replicated" {
			t.Errorf("node %d: expected replicated route, got %v %v", i, got.Name, err)
		}
	}
}

// TestClusterDumpRestore verifies that Dump + Restore preserves all data.
func TestClusterDumpRestore(t *testing.T) {
	ctx := context.Background()
	dir1 := t.TempDir()
	st1, _ := boltstore.New(dir1 + "/store.db", nil)
	defer st1.Close()

	st1.SaveRoute(ctx, model.Route{
		ID: "r1", Name: "original",
		Match:       model.MatchRule{PathPrefix: "/"},
		DirectResponse: &model.RouteDirectResponse{Status: 200},
	})
	st1.SaveDestination(ctx, model.Destination{ID: "d1", Name: "dest", Host: "10.0.0.1", Port: 80})

	data, err := st1.Dump()
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}

	// Validate it's valid JSON.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Dump is not valid JSON: %v", err)
	}

	dir2 := t.TempDir()
	st2, _ := boltstore.New(dir2 + "/store.db", nil)
	defer st2.Close()

	if err := st2.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	route, err := st2.GetRoute(ctx, "r1")
	if err != nil || route.Name != "original" {
		t.Errorf("after restore, GetRoute: %v %v", route.Name, err)
	}
	dest, err := st2.GetDestination(ctx, "d1")
	if err != nil || dest.Name != "dest" {
		t.Errorf("after restore, GetDestination: %v %v", dest.Name, err)
	}
}
