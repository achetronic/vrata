package raft

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/hashicorp/raft"

	boltstore "github.com/achetronic/vrata/internal/store/bolt"
	"github.com/achetronic/vrata/internal/model"
)

func newTestStore(t *testing.T) *boltstore.Store {
	t.Helper()
	s, err := boltstore.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestFSMApply verifies that the FSM correctly applies each command type
// to the underlying bolt store.
func TestFSMApply(t *testing.T) {
	st := newTestStore(t)
	fsm := NewFSM(st)

	applyCmd := func(t *testing.T, cmdType CommandType, id string, payload interface{}) {
		t.Helper()
		var rawPayload json.RawMessage
		if payload != nil {
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			rawPayload = data
		}
		cmd := Command{Type: cmdType, ID: id, Payload: rawPayload}
		data, _ := json.Marshal(cmd)
		entry := &raft.Log{Data: data}
		if resp := fsm.Apply(entry); resp != nil {
			if err, ok := resp.(error); ok {
				t.Fatalf("FSM.Apply: %v", err)
			}
		}
	}

	t.Run("SaveRoute", func(t *testing.T) {
		route := model.Route{ID: "r1", Name: "test", Match: model.MatchRule{PathPrefix: "/test"}, DirectResponse: &model.RouteDirectResponse{Status: 200}}
		applyCmd(t, CmdSaveRoute, route.ID, route)
		ctx := t.Context()
		got, err := st.GetRoute(ctx, "r1")
		if err != nil || got.Name != "test" {
			t.Errorf("SaveRoute: %v %v", got, err)
		}
	})

	t.Run("DeleteRoute", func(t *testing.T) {
		applyCmd(t, CmdDeleteRoute, "r1", nil)
		ctx := t.Context()
		if _, err := st.GetRoute(ctx, "r1"); err == nil {
			t.Error("expected not found after delete")
		}
	})

	t.Run("SaveSnapshot", func(t *testing.T) {
		vs := model.VersionedSnapshot{ID: "s1", Name: "v1", Snapshot: model.Snapshot{}}
		applyCmd(t, CmdSaveSnapshot, "s1", vs)
		ctx := t.Context()
		got, err := st.GetSnapshot(ctx, "s1")
		if err != nil || got.Name != "v1" {
			t.Errorf("SaveSnapshot: %v %v", got, err)
		}
	})

	t.Run("ActivateSnapshot", func(t *testing.T) {
		applyCmd(t, CmdActivateSnapshot, "s1", nil)
		ctx := t.Context()
		active, err := st.GetActiveSnapshot(ctx)
		if err != nil || active.ID != "s1" {
			t.Errorf("ActivateSnapshot: %v %v", active.ID, err)
		}
	})

	t.Run("DeleteSnapshot", func(t *testing.T) {
		applyCmd(t, CmdDeleteSnapshot, "s1", nil)
		ctx := t.Context()
		if _, err := st.GetSnapshot(ctx, "s1"); err == nil {
			t.Error("expected not found after delete")
		}
	})

	t.Run("UnknownCommand", func(t *testing.T) {
		cmd := Command{Type: "UnknownXYZ", ID: "x"}
		data, _ := json.Marshal(cmd)
		entry := &raft.Log{Data: data}
		resp := fsm.Apply(entry)
		if resp == nil {
			t.Error("expected error for unknown command")
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		entry := &raft.Log{Data: []byte("not json")}
		resp := fsm.Apply(entry)
		if resp == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

// TestFSMSnapshotRestore verifies that Snapshot + Restore round-trips
// the full database state correctly.
func TestFSMSnapshotRestore(t *testing.T) {
	st1 := newTestStore(t)
	fsm1 := NewFSM(st1)

	ctx := t.Context()
	st1.SaveRoute(ctx, model.Route{ID: "r1", Name: "route-one", Match: model.MatchRule{PathPrefix: "/"}, DirectResponse: &model.RouteDirectResponse{Status: 200}})
	st1.SaveDestination(ctx, model.Destination{ID: "d1", Name: "dest-one", Host: "10.0.0.1", Port: 80})

	snap, err := fsm1.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Capture the snapshot bytes via a fake sink.
	sink := &testSnapshotSink{}
	if err := snap.Persist(sink); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	// Restore into a fresh store.
	st2 := newTestStore(t)
	fsm2 := NewFSM(st2)
	if err := fsm2.Restore(io.NopCloser(&sink.buf)); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	route, err := st2.GetRoute(ctx, "r1")
	if err != nil || route.Name != "route-one" {
		t.Errorf("restored route: %v %v", route, err)
	}

	dest, err := st2.GetDestination(ctx, "d1")
	if err != nil || dest.Name != "dest-one" {
		t.Errorf("restored destination: %v %v", dest, err)
	}
}

// TestPeerResolutionStatic verifies static peer parsing.
func TestPeerResolutionStatic(t *testing.T) {
	node := &Node{}
	peers, err := node.resolveStatic([]string{
		"cp-0=10.0.0.1:7000",
		"cp-1=10.0.0.2:7000",
		"cp-2=10.0.0.3:7000",
	})
	if err != nil {
		t.Fatalf("resolveStatic: %v", err)
	}
	if len(peers) != 3 {
		t.Errorf("expected 3 peers, got %d", len(peers))
	}
	if string(peers[0].ID) != "cp-0" {
		t.Errorf("expected id cp-0, got %s", peers[0].ID)
	}
	if string(peers[2].Address) != "10.0.0.3:7000" {
		t.Errorf("expected addr 10.0.0.3:7000, got %s", peers[2].Address)
	}
}

func TestPeerResolutionStaticInvalid(t *testing.T) {
	node := &Node{}
	_, err := node.resolveStatic([]string{"no-equals-sign"})
	if err == nil {
		t.Error("expected error for invalid peer format")
	}
}

// testSnapshotSink captures snapshot bytes in memory.
type testSnapshotSink struct {
	buf bytes.Buffer
}

func (s *testSnapshotSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *testSnapshotSink) Close() error                { return nil }
func (s *testSnapshotSink) ID() string                  { return "test" }
func (s *testSnapshotSink) Cancel() error               { return nil }
