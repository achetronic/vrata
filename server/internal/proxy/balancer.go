package proxy

import (
	"crypto/md5"
	"encoding/binary"
	"hash/crc32"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/achetronic/rutoso/internal/model"
)

// Balancer selects an upstream from a set of backends.
type Balancer interface {
	Pick(r *http.Request, backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream
}

// WeightedRandomBalancer picks by weighted random.
type WeightedRandomBalancer struct{}

func (WeightedRandomBalancer) Pick(_ *http.Request, backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream {
	return SelectBackend(backends, upstreams)
}

// RoundRobinBalancer cycles through backends in order.
type RoundRobinBalancer struct {
	counter atomic.Uint64
}

func (rr *RoundRobinBalancer) Pick(_ *http.Request, backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream {
	if len(backends) == 0 {
		return nil
	}
	idx := rr.counter.Add(1) % uint64(len(backends))
	return upstreams[backends[idx].DestinationID]
}

// LeastRequestBalancer picks the upstream with the fewest active requests.
type LeastRequestBalancer struct {
	mu       sync.Mutex
	inflight map[string]int64
}

func NewLeastRequestBalancer() *LeastRequestBalancer {
	return &LeastRequestBalancer{inflight: make(map[string]int64)}
}

func (lb *LeastRequestBalancer) Pick(_ *http.Request, backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	var best string
	bestCount := int64(1<<63 - 1)
	for _, b := range backends {
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
	return upstreams[best]
}

func (lb *LeastRequestBalancer) Done(destID string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.inflight[destID]--
	if lb.inflight[destID] < 0 {
		lb.inflight[destID] = 0
	}
}

// RandomBalancer picks a random backend.
type RandomBalancer struct{}

func (RandomBalancer) Pick(_ *http.Request, backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream {
	if len(backends) == 0 {
		return nil
	}
	idx := rand.Intn(len(backends))
	return upstreams[backends[idx].DestinationID]
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

// Build constructs the hash ring from the given backends.
func (rh *RingHashBalancer) Build(backends []model.BackendRef) {
	rh.mu.Lock()
	defer rh.mu.Unlock()

	rh.ring = nil
	vnodes := rh.ringSize / len(backends)
	if vnodes < 1 {
		vnodes = 1
	}

	for _, b := range backends {
		for i := 0; i < vnodes; i++ {
			key := []byte(b.DestinationID + ":" + string(rune(i)))
			h := crc32.ChecksumIEEE(key)
			rh.ring = append(rh.ring, ringEntry{hash: h, destID: b.DestinationID})
		}
	}

	sort.Slice(rh.ring, func(i, j int) bool {
		return rh.ring[i].hash < rh.ring[j].hash
	})
}

func (rh *RingHashBalancer) Pick(r *http.Request, backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream {
	rh.mu.RLock()
	defer rh.mu.RUnlock()

	if len(rh.ring) == 0 {
		return SelectBackend(backends, upstreams)
	}

	h := hashRequest(r)
	idx := sort.Search(len(rh.ring), func(i int) bool {
		return rh.ring[i].hash >= h
	})
	if idx >= len(rh.ring) {
		idx = 0
	}
	return upstreams[rh.ring[idx].destID]
}

// MaglevBalancer implements Maglev consistent hashing.
type MaglevBalancer struct {
	mu        sync.RWMutex
	table     []int
	backends  []string
	tableSize int
}

func NewMaglevBalancer(tableSize int) *MaglevBalancer {
	if tableSize <= 0 {
		tableSize = 65537
	}
	return &MaglevBalancer{tableSize: tableSize}
}

// Build constructs the Maglev lookup table.
func (m *MaglevBalancer) Build(backends []model.BackendRef) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := len(backends)
	if n == 0 {
		m.table = nil
		m.backends = nil
		return
	}

	m.backends = make([]string, n)
	for i, b := range backends {
		m.backends[i] = b.DestinationID
	}

	table := make([]int, m.tableSize)
	for i := range table {
		table[i] = -1
	}

	// Permutation table.
	offset := make([]uint64, n)
	skip := make([]uint64, n)
	for i, b := range backends {
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

func (m *MaglevBalancer) Pick(r *http.Request, backends []model.BackendRef, upstreams map[string]*Upstream) *Upstream {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.table) == 0 || len(m.backends) == 0 {
		return SelectBackend(backends, upstreams)
	}

	h := hashRequest(r)
	idx := m.table[h%uint32(len(m.table))]
	if idx < 0 || idx >= len(m.backends) {
		return SelectBackend(backends, upstreams)
	}
	return upstreams[m.backends[idx]]
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
func hashRequestWithPolicy(r *http.Request, policies []model.HashPolicy) uint32 {
	for _, hp := range policies {
		switch {
		case hp.Header != "":
			if val := r.Header.Get(hp.Header); val != "" {
				return crc32.ChecksumIEEE([]byte(val))
			}
		case hp.Cookie != "":
			if c, err := r.Cookie(hp.Cookie); err == nil {
				return crc32.ChecksumIEEE([]byte(c.Value))
			}
		case hp.SourceIP:
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			return crc32.ChecksumIEEE([]byte(host))
		}
	}
	// Fallback: client IP.
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return crc32.ChecksumIEEE([]byte(host))
}
