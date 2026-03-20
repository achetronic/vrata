// Package dedup detects overlapping path+hostname combinations across
// HTTPRoutes and logs warnings when configured. It detects both exact
// duplicates and semantic overlaps (e.g. PathPrefix /api covers Exact /api/users).
package dedup

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/achetronic/vrata/clients/controller/internal/mapper"
)

// Entry is a registered path+hostname combination with its source.
type Entry struct {
	Path     string
	PathType string // "PathPrefix", "Exact", "RegularExpression"
	Hostname string
	Source   string // "namespace/name"
}

// Overlap describes a single overlap between two routes.
type Overlap struct {
	// Incoming is the new entry that overlaps.
	Incoming Entry
	// Existing is the already-registered entry that it overlaps with.
	Existing Entry
}

// Detector tracks path+hostname combinations and warns on overlaps.
type Detector struct {
	// entries keyed by hostname → list of entries for that host.
	byHost map[string][]Entry
	logger *slog.Logger
}

// NewDetector creates a Detector.
func NewDetector(logger *slog.Logger) *Detector {
	return &Detector{
		byHost: make(map[string][]Entry),
		logger: logger,
	}
}

// Check registers all paths from an HTTPRoute and returns any overlaps
// found. It detects:
//   - Exact duplicate (same pathType + path + hostname)
//   - PathPrefix covers another PathPrefix (e.g. /api covers /api/users)
//   - PathPrefix covers an Exact path (e.g. /api covers Exact /api/users)
//   - Exact path covered by a PathPrefix
//
// RegularExpression overlaps are not detected (tracked in TODO).
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
					Source:   source,
				}
				ols := d.checkOverlap(entry)
				for _, ol := range ols {
					d.logger.Warn("overlapping route detected",
						slog.String("incoming", fmt.Sprintf("%s %s %s (from %s)", ol.Incoming.Hostname, ol.Incoming.PathType, ol.Incoming.Path, ol.Incoming.Source)),
						slog.String("existing", fmt.Sprintf("%s %s %s (from %s)", ol.Existing.Hostname, ol.Existing.PathType, ol.Existing.Path, ol.Existing.Source)),
					)
				}
				overlaps = append(overlaps, ols...)
				d.byHost[hostname] = append(d.byHost[hostname], entry)
			}
		}
	}
	return overlaps
}

// checkOverlap tests a new entry against all existing entries for the same hostname.
func (d *Detector) checkOverlap(newEntry Entry) []Overlap {
	existing := d.byHost[newEntry.Hostname]
	var result []Overlap

	for _, e := range existing {
		if e.Source == newEntry.Source {
			continue
		}
		if e.PathType == "RegularExpression" || newEntry.PathType == "RegularExpression" {
			continue
		}
		if overlaps(e, newEntry) {
			result = append(result, Overlap{Incoming: newEntry, Existing: e})
		}
	}
	return result
}

// overlaps returns true if two entries on the same hostname have overlapping paths.
func overlaps(a, b Entry) bool {
	// Exact vs Exact: only if identical path.
	if a.PathType == "Exact" && b.PathType == "Exact" {
		return a.Path == b.Path
	}

	// PathPrefix vs PathPrefix: one contains the other.
	if a.PathType == "PathPrefix" && b.PathType == "PathPrefix" {
		return prefixCovers(a.Path, b.Path) || prefixCovers(b.Path, a.Path)
	}

	// PathPrefix vs Exact: prefix covers the exact path.
	if a.PathType == "PathPrefix" && b.PathType == "Exact" {
		return prefixCovers(a.Path, b.Path)
	}
	if a.PathType == "Exact" && b.PathType == "PathPrefix" {
		return prefixCovers(b.Path, a.Path)
	}

	return false
}

// prefixCovers returns true if prefix covers path. A prefix "/api" covers
// "/api", "/api/", and "/api/users" but NOT "/apikeys".
func prefixCovers(prefix, path string) bool {
	if prefix == path {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// /api covers /api/users but not /apikeys — the next char must be / or end of string.
	if strings.HasSuffix(prefix, "/") {
		return true
	}
	return len(path) > len(prefix) && path[len(prefix)] == '/'
}

// Remove unregisters all entries from the given source.
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

// Reset clears all entries.
func (d *Detector) Reset() {
	d.byHost = make(map[string][]Entry)
}
