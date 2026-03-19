// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package dedup detects overlapping match combinations across HTTPRoutes and
// logs warnings when configured. It detects both exact duplicates and semantic
// overlaps (e.g. PathPrefix /api covers Exact /api/users). Header matchers and
// HTTP methods are taken into account: two routes with the same hostname and
// overlapping paths but different headers or different methods are NOT
// considered overlapping because they target different traffic.
package dedup

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/achetronic/vrata/clients/controller/internal/mapper"
)

// Entry represents a single registered match combination (path + hostname +
// headers + method) along with its originating HTTPRoute source for
// deduplication tracking.
type Entry struct {
	Path     string
	PathType string // "PathPrefix", "Exact", "RegularExpression"
	Hostname string
	Headers  []mapper.HeaderMatchInput
	Method   string // empty means all methods
	Source   string // "namespace/name"
}

// Overlap describes a conflict between two route entries that target the same traffic.
type Overlap struct {
	Incoming Entry // the new entry that caused the overlap
	Existing Entry // the already-registered entry it conflicts with
}

// Detector tracks path+hostname+header+method combinations across HTTPRoutes
// and reports overlaps. It is not safe for concurrent use — callers must
// synchronize externally if needed.
type Detector struct {
	byHost map[string][]Entry // entries keyed by hostname
	logger *slog.Logger
}

// NewDetector creates a Detector with the given structured logger.
func NewDetector(logger *slog.Logger) *Detector {
	return &Detector{
		byHost: make(map[string][]Entry),
		logger: logger,
	}
}

// Check registers all match combinations from an HTTPRoute and returns any
// overlaps found against previously registered entries. It detects:
//   - Exact duplicate (same pathType + path + hostname + headers + method)
//   - PathPrefix covers another PathPrefix (e.g. /api covers /api/users)
//   - PathPrefix covers an Exact path (e.g. /api covers Exact /api/users)
//   - Exact path covered by a PathPrefix
//
// Two entries with overlapping paths but different header matchers or different
// HTTP methods are NOT considered overlapping — they route different traffic.
//
// RegularExpression paths are skipped (overlap detection is not feasible).
func (d *Detector) Check(input mapper.HTTPRouteInput) []Overlap {
	source := fmt.Sprintf("%s/%s", input.Namespace, input.Name)
	var overlaps []Overlap

	hostnames := input.Hostnames
	if len(hostnames) == 0 {
		hostnames = []string{"*"}
	}

	for _, rule := range input.Rules {
		matches := rule.Matches
		if len(matches) == 0 {
			matches = []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/"}}
		}
		for _, m := range matches {
			if m.PathType == "RegularExpression" {
				continue
			}
			for _, hostname := range hostnames {
				entry := Entry{
					Path:     m.PathValue,
					PathType: m.PathType,
					Hostname: hostname,
					Headers:  m.Headers,
					Method:   m.Method,
					Source:   source,
				}
				ols := d.findOverlaps(entry)
				for _, ol := range ols {
					d.logger.Warn("overlapping route detected",
						slog.String("incoming", formatEntry(ol.Incoming)),
						slog.String("existing", formatEntry(ol.Existing)),
					)
				}
				overlaps = append(overlaps, ols...)
				d.byHost[hostname] = append(d.byHost[hostname], entry)
			}
		}
	}
	return overlaps
}

// Remove unregisters all entries originating from the given HTTPRoute.
func (d *Detector) Remove(namespace, name string) {
	source := fmt.Sprintf("%s/%s", namespace, name)
	for hostname, entries := range d.byHost {
		var kept []Entry
		for _, e := range entries {
			if e.Source != source {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(d.byHost, hostname)
		} else {
			d.byHost[hostname] = kept
		}
	}
}

// Reset clears all registered entries. Useful when the detector needs to be
// rebuilt from scratch (e.g. after a full resync).
func (d *Detector) Reset() {
	d.byHost = make(map[string][]Entry)
}

// findOverlaps tests a new entry against all existing entries for the same
// hostname and returns any conflicts. Entries from the same source are skipped.
func (d *Detector) findOverlaps(newEntry Entry) []Overlap {
	existing := d.byHost[newEntry.Hostname]
	var result []Overlap

	for _, e := range existing {
		if e.Source == newEntry.Source {
			continue
		}
		if e.PathType == "RegularExpression" || newEntry.PathType == "RegularExpression" {
			continue
		}
		if entriesOverlap(e, newEntry) {
			result = append(result, Overlap{Incoming: newEntry, Existing: e})
		}
	}
	return result
}

// entriesOverlap returns true if two entries on the same hostname have
// overlapping paths, identical header matchers, AND overlapping HTTP methods.
// A difference in any of these three dimensions means the routes target
// different traffic and don't actually conflict.
func entriesOverlap(a, b Entry) bool {
	if !methodsOverlap(a.Method, b.Method) {
		return false
	}
	if !sameHeaders(a.Headers, b.Headers) {
		return false
	}
	return pathsOverlap(a.PathType, a.Path, b.PathType, b.Path)
}

// methodsOverlap returns true if two method matchers can select the same
// requests. An empty method means "all methods", which overlaps with everything.
// Two non-empty methods overlap only if they are the same (case-insensitive).
func methodsOverlap(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	return strings.EqualFold(a, b)
}

// pathsOverlap returns true if two path matchers on the same hostname select
// overlapping request sets. It handles all combinations of Exact and PathPrefix.
func pathsOverlap(aType, aPath, bType, bPath string) bool {
	if aType == "Exact" && bType == "Exact" {
		return aPath == bPath
	}
	if aType == "PathPrefix" && bType == "PathPrefix" {
		return prefixCovers(aPath, bPath) || prefixCovers(bPath, aPath)
	}
	if aType == "PathPrefix" && bType == "Exact" {
		return prefixCovers(aPath, bPath)
	}
	if aType == "Exact" && bType == "PathPrefix" {
		return prefixCovers(bPath, aPath)
	}
	return false
}

// sameHeaders returns true if both header slices contain the exact same set of
// matchers, regardless of order. Header names are compared case-insensitively
// per HTTP semantics. An empty Type defaults to "Exact" (Gateway API default).
// Two entries with no headers both match all traffic, so they are equal.
func sameHeaders(a, b []mapper.HeaderMatchInput) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	an := normalizeHeaders(a)
	bn := normalizeHeaders(b)
	for i := range an {
		if an[i] != bn[i] {
			return false
		}
	}
	return true
}

// normalizeHeaders converts a slice of header matchers into a sorted slice of
// canonical strings for order-independent comparison. Each header is encoded as
// "Type:lowercased-name=value".
func normalizeHeaders(headers []mapper.HeaderMatchInput) []string {
	out := make([]string, len(headers))
	for i, h := range headers {
		t := h.Type
		if t == "" {
			t = "Exact"
		}
		out[i] = t + ":" + strings.ToLower(h.Name) + "=" + h.Value
	}
	sort.Strings(out)
	return out
}

// prefixCovers returns true if the given prefix path covers the target path.
// A prefix "/api" covers "/api", "/api/", and "/api/users" but NOT "/apikeys".
// Coverage requires a segment boundary: the character after the prefix must be
// '/' or end-of-string.
func prefixCovers(prefix, path string) bool {
	if prefix == path {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if strings.HasSuffix(prefix, "/") {
		return true
	}
	return len(path) > len(prefix) && path[len(prefix)] == '/'
}

// formatEntry produces a human-readable string for an Entry, including header
// matchers and method when present, for use in log messages.
func formatEntry(e Entry) string {
	base := fmt.Sprintf("%s %s %s (from %s)", e.Hostname, e.PathType, e.Path, e.Source)
	var extras []string
	if e.Method != "" {
		extras = append(extras, fmt.Sprintf("method: %s", e.Method))
	}
	if len(e.Headers) > 0 {
		parts := make([]string, len(e.Headers))
		for i, h := range e.Headers {
			parts[i] = fmt.Sprintf("%s=%s", h.Name, h.Value)
		}
		extras = append(extras, fmt.Sprintf("headers: %s", strings.Join(parts, ", ")))
	}
	if len(extras) == 0 {
		return base
	}
	return fmt.Sprintf("%s [%s]", base, strings.Join(extras, "; "))
}
