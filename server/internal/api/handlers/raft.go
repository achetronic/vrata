// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"io"
	"net"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
)

// HandleRaftApply receives a serialised Raft command from a follower node and
// applies it through the local Raft log. This is the write-forwarding
// mechanism that allows any control plane node to accept writes: followers
// forward the command to the leader via this endpoint, the leader replicates
// it to quorum, and the follower relays the response to the original caller.
//
// Security: only accepts requests from private or loopback IPs.
// Must not be exposed outside the cluster network.
//
// @Summary     Raft write-forward
// @Description Receives a serialised Raft command from a follower and applies it on the leader. Used internally by control plane nodes for write-forwarding in cluster mode. Rejects requests from public IPs.
// @Tags        sync
// @Accept      json
// @Produce     json
// @Success     200
// @Failure     400 {object} respond.ErrorBody
// @Failure     403 {object} respond.ErrorBody
// @Failure     500 {object} respond.ErrorBody
// @Router      /sync/raft [post]
func (d *Dependencies) HandleRaftApply(w http.ResponseWriter, r *http.Request) {
	if d.Raft == nil {
		respond.Error(w, http.StatusServiceUnavailable, "cluster mode not active", d.Logger)
		return
	}

	if !isPrivateAddr(r.RemoteAddr) {
		respond.Error(w, http.StatusForbidden, "internal endpoint: access denied", d.Logger)
		return
	}

	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "reading body", d.Logger)
		return
	}

	if err := d.Raft.ApplyRaw(data); err != nil {
		respond.Error(w, http.StatusInternalServerError, "applying raft command", d.Logger)
		d.Logger.Error("raft apply failed", "error", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// isPrivateAddr returns true if the remote address is a loopback or private IP.
func isPrivateAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
