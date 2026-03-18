package proxy

import (
	"hash/crc32"
	"sort"

	"github.com/achetronic/rutoso/internal/model"
)

// destinationRing is a weighted consistent hash ring used for destination
// pinning. Each destination occupies ring space proportional to its weight.
// Given the same backends and the same key, every proxy produces the same
// result — no shared state required.
type destinationRing struct {
	entries []ringNode
}

type ringNode struct {
	hash   uint32
	destID string
}

const vnodeMultiplier = 100

// buildDestinationRing creates a weighted consistent hash ring from a list
// of backends. Destinations with higher weight get more virtual nodes.
func buildDestinationRing(backends []model.BackendRef) *destinationRing {
	if len(backends) == 0 {
		return &destinationRing{}
	}

	var entries []ringNode
	for _, b := range backends {
		vnodes := int(b.Weight) * vnodeMultiplier
		if vnodes < 1 {
			vnodes = 1
		}
		for i := 0; i < vnodes; i++ {
			key := []byte(b.DestinationID + ":" + itoa(i))
			entries = append(entries, ringNode{
				hash:   crc32.ChecksumIEEE(key),
				destID: b.DestinationID,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].hash < entries[j].hash
	})

	return &destinationRing{entries: entries}
}

// Pick returns the destination ID for the given hash key. Returns empty
// string if the ring is empty.
func (dr *destinationRing) Pick(hashKey uint32) string {
	if len(dr.entries) == 0 {
		return ""
	}

	idx := sort.Search(len(dr.entries), func(i int) bool {
		return dr.entries[i].hash >= hashKey
	})
	if idx >= len(dr.entries) {
		idx = 0
	}
	return dr.entries[idx].destID
}

// PickValid returns the destination ID for the given hash key, skipping
// destinations not present in the valid set. Returns empty string if no
// valid destination is found.
func (dr *destinationRing) PickValid(hashKey uint32, valid map[string]bool) string {
	if len(dr.entries) == 0 {
		return ""
	}

	idx := sort.Search(len(dr.entries), func(i int) bool {
		return dr.entries[i].hash >= hashKey
	})

	for range dr.entries {
		if idx >= len(dr.entries) {
			idx = 0
		}
		if valid[dr.entries[idx].destID] {
			return dr.entries[idx].destID
		}
		idx++
	}

	return ""
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}
