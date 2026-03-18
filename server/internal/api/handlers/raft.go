package handlers

import (
	"io"
	"net"
	"net/http"

	"github.com/achetronic/vrata/internal/api/respond"
)

// RaftApply is an internal endpoint used for write-forwarding in cluster mode.
// A follower that receives a write forwards the serialised Raft command here
// on the leader, which applies it through the Raft log.
//
// Security: this endpoint only accepts requests from private/loopback IPs.
// It must not be exposed outside the cluster network.
//
// @Summary     Internal Raft apply
// @Description Applies a raw Raft command. For internal cluster use only.
// @Tags        internal
// @Accept      json
// @Produce     json
// @Success     200
// @Failure     400 {object} respond.ErrorBody
// @Failure     403 {object} respond.ErrorBody
// @Failure     500 {object} respond.ErrorBody
// @Router      /internal/raft/apply [post]
func (d *Dependencies) RaftApply(w http.ResponseWriter, r *http.Request) {
	if d.Raft == nil {
		respond.Error(w, http.StatusServiceUnavailable, "cluster mode not active", d.Logger)
		return
	}

	if !isPrivateAddr(r.RemoteAddr) {
		respond.Error(w, http.StatusForbidden, "internal endpoint: access denied", d.Logger)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "reading body: "+err.Error(), d.Logger)
		return
	}

	if err := d.Raft.ApplyRaw(data); err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error(), d.Logger)
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
