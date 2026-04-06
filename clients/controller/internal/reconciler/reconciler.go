// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package reconciler calculates the diff between desired state (from Kubernetes)
// and actual state (from Vrata), and applies changes in dependency order.
// It also maintains Destination reference counts to safely delete shared destinations.
package reconciler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/achetronic/vrata/clients/controller/internal/mapper"
	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

// RefCount tracks how many owned Routes reference each Destination name.
// It is rebuilt on startup from the current state in Vrata and updated
// incrementally on each reconcile.
type RefCount struct {
	counts map[string]int
}

// NewRefCount creates an empty RefCount.
func NewRefCount() *RefCount {
	return &RefCount{counts: make(map[string]int)}
}

// RebuildFromRoutes scans all owned routes in Vrata and counts how many
// reference each Destination (by name in the destinationId field).
func (rc *RefCount) RebuildFromRoutes(routes []vrata.Route) {
	rc.counts = make(map[string]int)
	for _, r := range routes {
		if !mapper.IsOwned(r.Name) {
			continue
		}
		for _, destName := range extractDestinationNames(r) {
			rc.counts[destName]++
		}
	}
}

// Increment adds a reference for a destination.
func (rc *RefCount) Increment(destName string) {
	rc.counts[destName]++
}

// Decrement removes a reference. Returns true if the count reached zero.
func (rc *RefCount) Decrement(destName string) bool {
	rc.counts[destName]--
	if rc.counts[destName] <= 0 {
		delete(rc.counts, destName)
		return true
	}
	return false
}

// Count returns the current reference count for a destination.
func (rc *RefCount) Count(destName string) int {
	return rc.counts[destName]
}

// extractDestinationNames pulls destination names from a route's forward action.
func extractDestinationNames(r vrata.Route) []string {
	if r.Forward == nil {
		return nil
	}
	dests, ok := r.Forward["destinations"]
	if !ok {
		return nil
	}
	destSlice, ok := dests.([]any)
	if !ok {
		return nil
	}
	var names []string
	for _, d := range destSlice {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := dm["destinationId"].(string); ok {
			names = append(names, id)
		}
	}
	return names
}

// Reconciler applies changes from mapped entities to Vrata.
type Reconciler struct {
	client       *vrata.Client
	refCount     *RefCount
	routeDestMap map[string][]string // routeName → []destinationName
	logger       *slog.Logger
}

// NewReconciler creates a Reconciler.
func NewReconciler(client *vrata.Client, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		client:       client,
		refCount:     NewRefCount(),
		routeDestMap: make(map[string][]string),
		logger:       logger,
	}
}

// Client returns the underlying Vrata API client.
func (r *Reconciler) Client() *vrata.Client {
	return r.client
}

// Init rebuilds the refcount and routeDestMap from the current state in Vrata.
// Must be called once on startup before any reconcile.
func (r *Reconciler) Init(ctx context.Context) error {
	routes, err := r.client.ListRoutes(ctx)
	if err != nil {
		return fmt.Errorf("init: listing routes: %w", err)
	}
	dests, err := r.client.ListDestinations(ctx)
	if err != nil {
		return fmt.Errorf("init: listing destinations: %w", err)
	}

	// Build UUID → name lookup for destinations.
	idToName := make(map[string]string, len(dests))
	for _, d := range dests {
		idToName[d.ID] = d.Name
	}

	// Rebuild refcount and routeDestMap.
	r.refCount = NewRefCount()
	r.routeDestMap = make(map[string][]string)
	for _, route := range routes {
		if !mapper.IsOwned(route.Name) {
			continue
		}
		uuids := extractDestinationNames(route)
		var names []string
		for _, uuid := range uuids {
			if name, ok := idToName[uuid]; ok {
				names = append(names, name)
			}
		}
		r.routeDestMap[route.Name] = names
		for _, name := range names {
			r.refCount.Increment(name)
		}
	}

	r.logger.Info("reconciler: init complete",
		slog.Int("destinations", len(r.refCount.counts)),
		slog.Int("routes", len(r.routeDestMap)),
	)
	return nil
}

// ApplyHTTPRoute reconciles a single HTTPRoute's mapped entities against Vrata.
// It creates/updates destinations, routes, middlewares, and the group.
// Returns the number of changes applied.
func (r *Reconciler) ApplyHTTPRoute(ctx context.Context, mapped mapper.MappedEntities) (int, error) {
	changes := 0

	// 1. Ensure destinations exist.
	allDests, err := r.client.ListDestinations(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing destinations: %w", err)
	}
	destByName := make(map[string]vrata.Destination)
	for _, d := range allDests {
		destByName[d.Name] = d
	}

	for _, dk := range mapped.Destinations {
		name := dk.DestinationName()
		dest := vrata.Destination{
			Name: name,
			Host: dk.FQDN(),
			Port: dk.Port,
		}
		
		if existing, ok := destByName[name]; !ok {
			created, err := r.client.CreateDestination(ctx, dest)
			if err != nil {
				return changes, fmt.Errorf("creating destination %q: %w", name, err)
			}
			destByName[name] = *created
			r.logger.Info("reconciler: created destination", slog.String("name", name), slog.String("id", created.ID))
			changes++
		} else {
			dest.ID = existing.ID
			if existing.Host != dest.Host || existing.Port != dest.Port {
				if err := r.client.UpdateDestination(ctx, dest.ID, dest); err != nil {
					return changes, fmt.Errorf("updating destination %q: %w", name, err)
				}
				r.logger.Info("reconciler: updated destination", slog.String("name", name), slog.String("id", dest.ID))
				changes++
			}
		}
	}

	// 2. Create/update middlewares — pre-fetch once.
	allMws, err := r.client.ListMiddlewares(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing middlewares: %w", err)
	}
	mwByName := make(map[string]vrata.Middleware)
	for _, m := range allMws {
		mwByName[m.Name] = m
	}

	for _, mw := range mapped.Middlewares {
		if existing, ok := mwByName[mw.Name]; !ok {
			created, err := r.client.CreateMiddleware(ctx, mw)
			if err != nil {
				return changes, fmt.Errorf("creating middleware %q: %w", mw.Name, err)
			}
			mwByName[mw.Name] = *created
			changes++
		} else {
			mw.ID = existing.ID
			if err := r.client.UpdateMiddleware(ctx, existing.ID, mw); err != nil {
				return changes, fmt.Errorf("updating middleware %q: %w", mw.Name, err)
			}
			changes++
		}
	}

	// 3. Create/update routes (resolve destinationId from name → Vrata ID).
	destIDs := make(map[string]string, len(destByName))
	for name, d := range destByName {
		destIDs[name] = d.ID
	}
	mwIDs := make(map[string]string, len(mwByName))
	for name, m := range mwByName {
		mwIDs[name] = m.ID
	}

	// Pre-fetch routes for name→ID lookup.
	allRoutes, err := r.client.ListRoutes(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing routes: %w", err)
	}
	routeByName := make(map[string]vrata.Route)
	for _, rt := range allRoutes {
		routeByName[rt.Name] = rt
	}

	// Pre-fetch groups for name→ID lookup.
	allGroups, err := r.client.ListGroups(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing groups: %w", err)
	}
	groupByName := make(map[string]vrata.RouteGroup)
	for _, g := range allGroups {
		groupByName[g.Name] = g
	}

	var routeVrataIDs []string
	for _, route := range mapped.Routes {
		resolved := resolveRouteRefs(route, destIDs, mwIDs)
		if existing, ok := routeByName[route.Name]; !ok {
			created, err := r.client.CreateRoute(ctx, resolved)
			if err != nil {
				return changes, fmt.Errorf("creating route %q: %w", route.Name, err)
			}
			routeVrataIDs = append(routeVrataIDs, created.ID)
			destNames := destinationNamesForRoute(mapped.Destinations)
			r.routeDestMap[route.Name] = destNames
			for _, dn := range destNames {
				r.refCount.Increment(dn)
			}
			changes++
		} else {
			resolved.ID = existing.ID
			if err := r.client.UpdateRoute(ctx, existing.ID, resolved); err != nil {
				return changes, fmt.Errorf("updating route %q: %w", route.Name, err)
			}
			routeVrataIDs = append(routeVrataIDs, existing.ID)
			changes++
		}
	}

	// 4. Create/update group.
	group := mapped.Group
	group.RouteIDs = routeVrataIDs
	if existingGroup, ok := groupByName[group.Name]; !ok {
		_, err := r.client.CreateGroup(ctx, group)
		if err != nil {
			return changes, fmt.Errorf("creating group %q: %w", group.Name, err)
		}
		changes++
	} else {
		group.ID = existingGroup.ID
		if err := r.client.UpdateGroup(ctx, existingGroup.ID, group); err != nil {
			return changes, fmt.Errorf("updating group %q: %w", group.Name, err)
		}
		changes++
	}

	// 5. Delete stale routes that belong to this HTTPRoute but are no longer
	//    generated by the mapper (e.g. a match or rule was removed).
	prefix := group.Name + "/"
	desiredRouteNames := make(map[string]bool, len(mapped.Routes))
	for _, route := range mapped.Routes {
		desiredRouteNames[route.Name] = true
	}
	allRoutes, err = r.client.ListRoutes(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing routes for intra-group GC: %w", err)
	}
	var destsToCheck []string
	for _, route := range allRoutes {
		if !mapper.IsOwned(route.Name) || !hasPrefix(route.Name, prefix) {
			continue
		}
		if desiredRouteNames[route.Name] {
			continue
		}
		destNames := r.routeDestMap[route.Name]
		destsToCheck = append(destsToCheck, destNames...)
		if err := r.client.DeleteRoute(ctx, route.ID); err != nil {
			return changes, fmt.Errorf("deleting stale route %q: %w", route.Name, err)
		}
		r.logger.Info("reconciler: deleted stale route", slog.String("name", route.Name))
		changes++
		for _, dn := range destNames {
			r.refCount.Decrement(dn)
		}
		delete(r.routeDestMap, route.Name)
	}

	// 6. Delete stale middlewares that belong to this HTTPRoute but are no
	//    longer generated by the mapper.
	desiredMWNames := make(map[string]bool, len(mapped.Middlewares))
	for _, mw := range mapped.Middlewares {
		desiredMWNames[mw.Name] = true
	}
	allMWs, err := r.client.ListMiddlewares(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing middlewares for intra-group GC: %w", err)
	}
	for _, mw := range allMWs {
		if !mapper.IsOwned(mw.Name) || !hasPrefix(mw.Name, prefix) {
			continue
		}
		if desiredMWNames[mw.Name] {
			continue
		}
		if err := r.client.DeleteMiddleware(ctx, mw.ID); err != nil {
			return changes, fmt.Errorf("deleting stale middleware %q: %w", mw.Name, err)
		}
		r.logger.Info("reconciler: deleted stale middleware", slog.String("name", mw.Name))
		changes++
	}

	// 7. Delete destinations whose refcount reached zero from stale route removal.
	if len(destsToCheck) > 0 {
		allDests, err := r.client.ListDestinations(ctx)
		if err != nil {
			return changes, fmt.Errorf("listing destinations for intra-group GC: %w", err)
		}
		for _, dn := range unique(destsToCheck) {
			if r.refCount.Count(dn) > 0 {
				continue
			}
			for _, d := range allDests {
				if d.Name == dn {
					if err := r.client.DeleteDestination(ctx, d.ID); err != nil {
						return changes, fmt.Errorf("deleting unreferenced destination %q: %w", dn, err)
					}
					r.logger.Info("reconciler: deleted unreferenced destination", slog.String("name", dn))
					changes++
					break
				}
			}
		}
	}

	return changes, nil
}

// DeleteRouteGroup removes all entities created from an HTTPRoute.
// Destinations are only deleted if their refcount reaches zero.
// Returns the number of changes applied.
func (r *Reconciler) DeleteRouteGroup(ctx context.Context, namespace, name string) (int, error) {
	prefix := fmt.Sprintf("k8s:%s/%s", namespace, name)
	changes := 0

	// 1. Delete group.
	groups, err := r.client.ListGroups(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing groups: %w", err)
	}
	for _, g := range groups {
		if g.Name == prefix {
			if err := r.client.DeleteGroup(ctx, g.ID); err != nil {
				return changes, fmt.Errorf("deleting group %q: %w", g.Name, err)
			}
			changes++
		}
	}

	// 2. Delete routes and decrement refcounts.
	routes, err := r.client.ListRoutes(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing routes: %w", err)
	}
	var destsToCheck []string
	for _, route := range routes {
		if !mapper.IsOwned(route.Name) || !hasPrefix(route.Name, prefix+"/") {
			continue
		}
		// Use the routeDestMap for reliable name-based refcount.
		destNames := r.routeDestMap[route.Name]
		destsToCheck = append(destsToCheck, destNames...)
		if err := r.client.DeleteRoute(ctx, route.ID); err != nil {
			return changes, fmt.Errorf("deleting route %q: %w", route.Name, err)
		}
		changes++
		for _, dn := range destNames {
			r.refCount.Decrement(dn)
		}
		delete(r.routeDestMap, route.Name)
	}

	// 3. Delete middlewares.
	middlewares, err := r.client.ListMiddlewares(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing middlewares: %w", err)
	}
	for _, mw := range middlewares {
		if mapper.IsOwned(mw.Name) && hasPrefix(mw.Name, prefix+"/") {
			if err := r.client.DeleteMiddleware(ctx, mw.ID); err != nil {
				return changes, fmt.Errorf("deleting middleware %q: %w", mw.Name, err)
			}
			changes++
		}
	}

	// 4. Delete destinations with zero refcount.
	dests, err := r.client.ListDestinations(ctx)
	if err != nil {
		return changes, fmt.Errorf("listing destinations: %w", err)
	}
	for _, dn := range unique(destsToCheck) {
		if r.refCount.Count(dn) > 0 {
			continue
		}
		for _, d := range dests {
			if d.Name == dn {
				if err := r.client.DeleteDestination(ctx, d.ID); err != nil {
					return changes, fmt.Errorf("deleting destination %q: %w", dn, err)
				}
				changes++
				break
			}
		}
	}

	return changes, nil
}

// DeleteListenerByName removes a single owned listener from Vrata by name.
// Returns the number of changes applied (0 or 1).
func (r *Reconciler) DeleteListenerByName(ctx context.Context, name string) (int, error) {
	listeners, err := r.client.ListListeners(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing listeners: %w", err)
	}
	for _, l := range listeners {
		if l.Name == name {
			if err := r.client.DeleteListener(ctx, l.ID); err != nil {
				return 0, fmt.Errorf("deleting listener %q: %w", name, err)
			}
			r.logger.Info("reconciler: deleted orphaned listener", slog.String("name", name))
			return 1, nil
		}
	}
	return 0, nil
}

// OwnedGroupNames returns the k8s: prefixed group names currently in Vrata.
// Each group corresponds to one HTTPRoute or SuperHTTPRoute.
func (r *Reconciler) OwnedGroupNames(ctx context.Context) ([]string, error) {
	groups, err := r.client.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}
	var names []string
	for _, g := range groups {
		if mapper.IsOwned(g.Name) {
			names = append(names, g.Name)
		}
	}
	return names, nil
}

// OwnedListenerNames returns the k8s: prefixed listener names currently in Vrata.
func (r *Reconciler) OwnedListenerNames(ctx context.Context) ([]string, error) {
	listeners, err := r.client.ListListeners(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing listeners: %w", err)
	}
	var names []string
	for _, l := range listeners {
		if mapper.IsOwned(l.Name) {
			names = append(names, l.Name)
		}
	}
	return names, nil
}

// resolveRouteRefs replaces name-based references in a Route with Vrata IDs.
func resolveRouteRefs(route vrata.Route, destIDs, mwIDs map[string]string) vrata.Route {
	if route.Forward != nil {
		if dests, ok := route.Forward["destinations"]; ok {
			if destSlice, ok := dests.([]map[string]any); ok {
				for i, d := range destSlice {
					if name, ok := d["destinationId"].(string); ok {
						if id, found := destIDs[name]; found {
							destSlice[i]["destinationId"] = id
						}
					}
				}
			}
		}
	}
	if len(route.MiddlewareIDs) > 0 && mwIDs != nil {
		for i, name := range route.MiddlewareIDs {
			if id, ok := mwIDs[name]; ok {
				route.MiddlewareIDs[i] = id
			}
		}
	}
	return route
}

// destinationNamesForRoute returns the k8s: names for all destinations
// in the mapped entity.
func destinationNamesForRoute(dks []mapper.DestinationKey) []string {
	names := make([]string, len(dks))
	for i, dk := range dks {
		names[i] = dk.DestinationName()
	}
	return names
}

// hasPrefix returns true if s starts with prefix.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// unique returns the deduplicated slice preserving order.
func unique(ss []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
