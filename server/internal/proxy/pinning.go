// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"hash/crc32"
	"sort"
	"strconv"

	"github.com/achetronic/vrata/internal/model"
)

// destinationRing is a weighted consistent hash ring used for destination
// pinning. Each destination occupies ring space proportional to its weight.
// Given the same dests and the same key, every proxy produces the same
// result — no shared state required.
//
// Consistent hash property: when weights change, only the clients in the
// ring space that changes ownership are moved. Clients whose hash falls in
// unchanged ring space stay on the same destination. This minimises
// disruption compared to weighted random, but does NOT guarantee zero
// movement — reducing a destination's weight necessarily moves some clients.
type destinationRing struct {
	entries []ringNode
}

type ringNode struct {
	hash   uint32
	destID string
}

const vnodeMultiplier = 100

// buildDestinationRing creates a weighted consistent hash ring from a list
// of dests. Destinations with higher weight get more virtual nodes.
func buildDestinationRing(dests []model.DestinationRef) *destinationRing {
	if len(dests) == 0 {
		return &destinationRing{}
	}

	var entries []ringNode
	for _, b := range dests {
		vnodes := int(b.Weight) * vnodeMultiplier
		if vnodes < 1 {
			vnodes = 1
		}
		for i := 0; i < vnodes; i++ {
			key := []byte(b.DestinationID + ":" + strconv.Itoa(i))
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

