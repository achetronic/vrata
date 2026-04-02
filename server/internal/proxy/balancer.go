// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	cryptorand "crypto/rand"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// Balancer selects an upstream from a set of dests.
type Balancer interface {
	Pick(r *http.Request, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint
}

// HashBalancer is implemented by consistent hash balancers that can accept
// a pre-computed hash key for deterministic endpoint selection.
type HashBalancer interface {
	Balancer
	PickByHash(h uint32, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint
}

// WeightedRandomBalancer picks by weighted random.
type WeightedRandomBalancer struct{}

// Pick selects a random endpoint.
func (WeightedRandomBalancer) Pick(_ *http.Request, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint {
	return pickRandomEndpoint(endpoints)
}

// RoundRobinBalancer cycles through dests in order.
type RoundRobinBalancer struct {
	counter atomic.Uint64
}

// Pick selects the next endpoint in round-robin order.
func (rr *RoundRobinBalancer) Pick(_ *http.Request, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint {
	if len(dests) == 0 {
		return nil
	}
	idx := rr.counter.Add(1) % uint64(len(dests))
	return endpoints[dests[idx].DestinationID]
}

// LeastRequestBalancer picks the upstream with the fewest active requests.
// When choiceCount > 0, it uses the power-of-two-choices algorithm: pick
// choiceCount random candidates and select the one with the lowest inflight.
type LeastRequestBalancer struct {
	mu          sync.Mutex
	inflight    map[string]int64
	choiceCount int
}

// NewLeastRequestBalancer creates a LeastRequestBalancer.
// choiceCount controls the power-of-two-choices sample size. When 0 or >= len(dests),
// all destinations are considered (exact least-request).
func NewLeastRequestBalancer(choiceCount int) *LeastRequestBalancer {
	return &LeastRequestBalancer{inflight: make(map[string]int64), choiceCount: choiceCount}
}

// Pick selects the endpoint with the fewest active requests.
func (lb *LeastRequestBalancer) Pick(_ *http.Request, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	candidates := dests
	if lb.choiceCount > 0 && lb.choiceCount < len(dests) {
		candidates = sampleDests(dests, lb.choiceCount)
	}

	var best string
	bestCount := int64(1<<63 - 1)
	for _, b := range candidates {
		count := lb.inflight[b.DestinationID]
		if count < bestCount {
			bestCount = count
			best = b.DestinationID
		}
	}
	if best == "" {
		return nil
	}
	lb.inflight[best]++
	return endpoints[best]
}

// Done decrements the in-flight counter for the given endpoint.
func (lb *LeastRequestBalancer) Done(destID string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.inflight[destID]--
	if lb.inflight[destID] < 0 {
		lb.inflight[destID] = 0
	}
}

// sampleDests returns a random sample of n destinations from the slice.
func sampleDests(dests []model.DestinationRef, n int) []model.DestinationRef {
	if n >= len(dests) {
		return dests
	}
	perm := rand.Perm(len(dests))
	out := make([]model.DestinationRef, n)
	for i := 0; i < n; i++ {
		out[i] = dests[perm[i]]
	}
	return out
}

// RandomBalancer picks a random destination.
type RandomBalancer struct{}

// Pick selects a random endpoint.
func (RandomBalancer) Pick(_ *http.Request, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint {
	if len(dests) == 0 {
		return nil
	}
	idx := rand.Intn(len(dests))
	return endpoints[dests[idx].DestinationID]
}

// ─── Consistent hashing ─────────────────────────────────────────────────────

// RingHashBalancer implements consistent hashing with a virtual node ring.
type RingHashBalancer struct {
	mu       sync.RWMutex
	ring     []ringEntry
	ringSize int
}

type ringEntry struct {
	hash   uint32
	destID string
}

// NewRingHashBalancer creates a RingHashBalancer with the given vnode range.
func NewRingHashBalancer(minSize, maxSize int) *RingHashBalancer {
	if minSize <= 0 {
		minSize = 1024
	}
	if maxSize <= 0 {
		maxSize = 8388608
	}
	size := minSize
	if size > maxSize {
		size = maxSize
	}
	return &RingHashBalancer{ringSize: size}
}

// Build constructs the hash ring from the given dests.
func (rh *RingHashBalancer) Build(dests []model.DestinationRef) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	rh.ring = nil
	if len(dests) == 0 {
		return
	}
	vnodes := rh.ringSize / len(dests)
	if vnodes < 1 {
		vnodes = 1
	}

	for _, b := range dests {
		for i := 0; i < vnodes; i++ {
			key := []byte(fmt.Sprintf("%s:%d", b.DestinationID, i))
			h := crc32.ChecksumIEEE(key)
			rh.ring = append(rh.ring, ringEntry{hash: h, destID: b.DestinationID})
		}
	}

	sort.Slice(rh.ring, func(i, j int) bool {
		return rh.ring[i].hash < rh.ring[j].hash
	})
}

// Pick selects an endpoint using the request's hash.
func (rh *RingHashBalancer) Pick(r *http.Request, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint {
	return rh.PickByHash(hashRequest(r), dests, endpoints)
}

// PickByHash selects an endpoint using a pre-computed hash key.
func (rh *RingHashBalancer) PickByHash(h uint32, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	if len(rh.ring) == 0 {
		return pickRandomEndpoint(endpoints)
	}

	idx := sort.Search(len(rh.ring), func(i int) bool {
		return rh.ring[i].hash >= h
	})
	if idx >= len(rh.ring) {
		idx = 0
	}
	return endpoints[rh.ring[idx].destID]
}

// MaglevBalancer implements Maglev consistent hashing.
type MaglevBalancer struct {
	mu        sync.RWMutex
	table     []int
	dests     []string
	tableSize int
}

// NewMaglevBalancer creates a MaglevBalancer with the given table size.
func NewMaglevBalancer(tableSize int) *MaglevBalancer {
	if tableSize <= 0 {
		tableSize = 65537
	}
	return &MaglevBalancer{tableSize: tableSize}
}

// Build constructs the Maglev lookup table.
func (m *MaglevBalancer) Build(dests []model.DestinationRef) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := len(dests)
	if n == 0 {
		m.table = nil
		m.dests = nil
		return
	}

	m.dests = make([]string, n)
	for i, b := range dests {
		m.dests[i] = b.DestinationID
	}

	table := make([]int, m.tableSize)
	for i := range table {
		table[i] = -1
	}

	// Permutation table.
	offset := make([]uint64, n)
	skip := make([]uint64, n)
	for i, b := range dests {
		h := md5.Sum([]byte(b.DestinationID))
		offset[i] = binary.LittleEndian.Uint64(h[:8]) % uint64(m.tableSize)
		skip[i] = (binary.LittleEndian.Uint64(h[8:]) % uint64(m.tableSize-1)) + 1
	}

	next := make([]uint64, n)
	for i := range next {
		next[i] = offset[i]
	}

	filled := 0
	for filled < m.tableSize {
		for i := 0; i < n; i++ {
			c := next[i]
			for table[c] != -1 {
				next[i] = (next[i] + skip[i]) % uint64(m.tableSize)
				c = next[i]
			}
			table[c] = i
			next[i] = (next[i] + skip[i]) % uint64(m.tableSize)
			filled++
			if filled >= m.tableSize {
				break
			}
		}
	}

	m.table = table
}

// Pick selects an endpoint using the request's hash.
func (m *MaglevBalancer) Pick(r *http.Request, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint {
	return m.PickByHash(hashRequest(r), dests, endpoints)
}

// PickByHash selects an endpoint using a pre-computed hash key.
func (m *MaglevBalancer) PickByHash(h uint32, dests []model.DestinationRef, endpoints map[string]*Endpoint) *Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.table) == 0 || len(m.dests) == 0 {
		return pickRandomEndpoint(endpoints)
	}

	idx := m.table[h%uint32(len(m.table))]
	if idx < 0 || idx >= len(m.dests) {
		return pickRandomEndpoint(endpoints)
	}
	return endpoints[m.dests[idx]]
}

// hashRequest computes a hash from the request using the provided hash policies.
// Evaluates policies in order; the first one that produces a value wins.
// Falls back to client IP if no policy matches.
func hashRequest(r *http.Request) uint32 {
	// Default: hash by client IP.
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return crc32.ChecksumIEEE([]byte(host))
}

// hashRequestWithPolicy computes a hash using explicit hash policies.
// The destID is appended to the hash input to isolate endpoint selection
// per destination — the same cookie value produces different hashes for
// different destinations, preventing cross-destination correlation.
func hashRequestWithPolicy(r *http.Request, w http.ResponseWriter, policies []model.HashPolicy, destID string) uint32 {
	for _, hp := range policies {
		switch {
		case hp.Header != nil && hp.Header.Name != "":
			if val := r.Header.Get(hp.Header.Name); val != "" {
				return crc32.ChecksumIEEE([]byte(val + ":" + destID))
			}
		case hp.Cookie != nil && hp.Cookie.Name != "":
			cookieName := hp.Cookie.Name
			if c, err := r.Cookie(cookieName); err == nil {
				return crc32.ChecksumIEEE([]byte(c.Value + ":" + destID))
			}
			sid := generateEndpointSessionID()
			ttl := parseTTL(hp.Cookie.TTL, time.Hour)
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    sid,
				Path:     "/",
				MaxAge:   int(ttl.Seconds()),
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			return crc32.ChecksumIEEE([]byte(sid + ":" + destID))
		case hp.SourceIP != nil && hp.SourceIP.Enabled:
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			return crc32.ChecksumIEEE([]byte(host + ":" + destID))
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return crc32.ChecksumIEEE([]byte(host + ":" + destID))
}

// generateEndpointSessionID creates a random session identifier for endpoint pinning.
func generateEndpointSessionID() string {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(rand.Intn(256))
		}
	}
	return fmt.Sprintf("%x", b)
}

// pickRandomEndpoint picks a random endpoint from the map. Used as fallback
// when a consistent hash ring is empty.
func pickRandomEndpoint(endpoints map[string]*Endpoint) *Endpoint {
	if len(endpoints) == 0 {
		return nil
	}
	keys := make([]string, 0, len(endpoints))
	for k := range endpoints {
		keys = append(keys, k)
	}
	return endpoints[keys[rand.Intn(len(keys))]]
}
