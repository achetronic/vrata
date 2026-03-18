// Package raft provides Raft-based replication for the Rutoso control plane.
// It enables multiple control plane instances to share the same configuration
// with strong consistency — any node can accept reads and writes, and all
// nodes converge to the same state.
package raft

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/hashicorp/raft"

	boltstore "github.com/achetronic/vrata/internal/store/bolt"
)

// CommandType identifies the store operation carried by a Raft log entry.
type CommandType = string

const (
	// CmdSaveRoute creates or replaces a route.
	CmdSaveRoute CommandType = "SaveRoute"
	// CmdDeleteRoute removes a route by ID.
	CmdDeleteRoute CommandType = "DeleteRoute"
	// CmdSaveGroup creates or replaces a route group.
	CmdSaveGroup CommandType = "SaveGroup"
	// CmdDeleteGroup removes a route group by ID.
	CmdDeleteGroup CommandType = "DeleteGroup"
	// CmdSaveMiddleware creates or replaces a middleware.
	CmdSaveMiddleware CommandType = "SaveMiddleware"
	// CmdDeleteMiddleware removes a middleware by ID.
	CmdDeleteMiddleware CommandType = "DeleteMiddleware"
	// CmdSaveListener creates or replaces a listener.
	CmdSaveListener CommandType = "SaveListener"
	// CmdDeleteListener removes a listener by ID.
	CmdDeleteListener CommandType = "DeleteListener"
	// CmdSaveDestination creates or replaces a destination.
	CmdSaveDestination CommandType = "SaveDestination"
	// CmdDeleteDestination removes a destination by ID.
	CmdDeleteDestination CommandType = "DeleteDestination"
	// CmdSaveSnapshot creates or replaces a versioned snapshot.
	CmdSaveSnapshot CommandType = "SaveSnapshot"
	// CmdDeleteSnapshot removes a versioned snapshot by ID.
	CmdDeleteSnapshot CommandType = "DeleteSnapshot"
	// CmdActivateSnapshot sets the active snapshot by ID.
	CmdActivateSnapshot CommandType = "ActivateSnapshot"
)

// Command is a single store operation serialised into a Raft log entry.
type Command struct {
	// Type identifies which store operation to apply.
	Type CommandType `json:"type"`

	// ID is used for delete and activate operations.
	ID string `json:"id,omitempty"`

	// Payload carries the JSON-encoded entity for save operations.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// FSM implements raft.FSM backed by a bolt store. Every committed log
// entry is a command applied directly to the local bbolt database.
type FSM struct {
	store *boltstore.Store
}

// NewFSM creates an FSM backed by the given bolt store.
func NewFSM(store *boltstore.Store) *FSM {
	return &FSM{store: store}
}

// Apply is called by Raft when a log entry is committed. It decodes the
// command and applies it to the local bolt store.
func (f *FSM) Apply(entry *raft.Log) interface{} {
	var cmd Command
	if err := json.Unmarshal(entry.Data, &cmd); err != nil {
		return fmt.Errorf("decoding command: %w", err)
	}

	if err := f.store.ApplyCommand(cmd.Type, cmd.ID, cmd.Payload); err != nil {
		return fmt.Errorf("applying command %s: %w", cmd.Type, err)
	}

	return nil
}

// Snapshot returns an FSMSnapshot that serialises the full bolt database
// state for Raft log compaction.
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	data, err := f.store.Dump()
	if err != nil {
		return nil, fmt.Errorf("dumping store for snapshot: %w", err)
	}
	return &fsmSnapshot{data: data}, nil
}

// Restore replaces the bolt store contents with the data from a Raft
// snapshot. Called when a node falls behind and needs to catch up.
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("reading snapshot data: %w", err)
	}

	return f.store.Restore(data)
}

// fsmSnapshot implements raft.FSMSnapshot using a raw bytes dump.
type fsmSnapshot struct {
	data []byte
}

// Persist writes the snapshot data to the given sink.
func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	if _, err := sink.Write(s.data); err != nil {
		sink.Cancel()
		return fmt.Errorf("writing snapshot: %w", err)
	}
	return sink.Close()
}

// Release is a no-op — the snapshot data is held in memory only long
// enough to be persisted by Raft.
func (s *fsmSnapshot) Release() {}
