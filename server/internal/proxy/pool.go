// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

// DestinationPool holds all endpoints for a single Destination and the
// balancer that selects between them. This is the level-2 abstraction:
// level 1 picks a DestinationPool, level 2 picks an endpoint within it.
type DestinationPool struct {
	Destination    model.Destination
	Endpoints      []*Endpoint
	Balancer       Balancer
	CircuitBreaker *CircuitBreaker
	SessionStore   SessionStore
}

// Pick selects an endpoint from the pool using the configured balancer.
// For consistent hash balancers, the caller must provide the request and
// response writer (for cookie generation). Falls back to round-robin if
// no balancer is configured.
func (dp *DestinationPool) Pick(r *http.Request, w http.ResponseWriter) *Endpoint {
	if len(dp.Endpoints) == 0 {
		return nil
	}
	if len(dp.Endpoints) == 1 {
		ep := dp.Endpoints[0]
		if !isHealthy(ep) {
			return nil
		}
		return ep
	}

	healthy := dp.healthyEndpoints()
	if len(healthy) == 0 {
		return nil
	}
	if len(healthy) == 1 {
		return healthy[0]
	}

	if dp.isStickyEndpoint() {
		return dp.pickStickyEndpoint(r, w, healthy)
	}

	if dp.Balancer != nil {
		refs := dp.endpointRefs(healthy)
		epMap := dp.endpointMap(healthy)
		return dp.Balancer.Pick(r, refs, epMap)
	}

	return healthy[rand.Intn(len(healthy))]
}

// PickByHash selects an endpoint using a pre-computed hash key.
// Used when the endpointBalancing algorithm is RING_HASH or MAGLEV
// with a hashPolicy.
func (dp *DestinationPool) PickByHash(h uint32, r *http.Request, w http.ResponseWriter) *Endpoint {
	if len(dp.Endpoints) == 0 {
		return nil
	}
	if len(dp.Endpoints) == 1 {
		ep := dp.Endpoints[0]
		if !isHealthy(ep) {
			return nil
		}
		return ep
	}

	healthy := dp.healthyEndpoints()
	if len(healthy) == 0 {
		return nil
	}

	if hb, ok := dp.Balancer.(HashBalancer); ok {
		refs := dp.endpointRefs(healthy)
		epMap := dp.endpointMap(healthy)
		return hb.PickByHash(h, refs, epMap)
	}

	return dp.Pick(r, w)
}

// ReverseProxy creates an httputil.ReverseProxy targeting a specific endpoint.
func (dp *DestinationPool) ReverseProxyFor(ep *Endpoint) *httputil.ReverseProxy {
	scheme := "http"
	d := dp.Destination
	if d.Options != nil && d.Options.TLS != nil &&
		d.Options.TLS.Mode != model.TLSModeNone && d.Options.TLS.Mode != "" {
		scheme = "https"
	}
	target := &url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", ep.Host, ep.Port),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = ep.RoundTripper
	proxy.FlushInterval = -1
	return proxy
}

// EndpointHashPolicies returns the hash policies configured for this destination.
func (dp *DestinationPool) EndpointHashPolicies() []model.HashPolicy {
	if dp.Destination.Options == nil || dp.Destination.Options.EndpointBalancing == nil {
		return nil
	}
	eb := dp.Destination.Options.EndpointBalancing
	switch eb.Algorithm {
	case model.EndpointLBRingHash:
		if eb.RingHash != nil {
			return eb.RingHash.HashPolicy
		}
	case model.EndpointLBMaglev:
		if eb.Maglev != nil {
			return eb.Maglev.HashPolicy
		}
	}
	return nil
}

func (dp *DestinationPool) healthyEndpoints() []*Endpoint {
	healthy := make([]*Endpoint, 0, len(dp.Endpoints))
	for _, ep := range dp.Endpoints {
		if isHealthy(ep) {
			healthy = append(healthy, ep)
		}
	}
	return healthy
}

func (dp *DestinationPool) endpointRefs(eps []*Endpoint) []model.DestinationRef {
	refs := make([]model.DestinationRef, len(eps))
	for i, ep := range eps {
		refs[i] = model.DestinationRef{
			DestinationID: ep.ID,
			Weight:        1,
		}
	}
	return refs
}

func (dp *DestinationPool) endpointMap(eps []*Endpoint) map[string]*Endpoint {
	m := make(map[string]*Endpoint, len(eps))
	for _, ep := range eps {
		m[ep.ID] = ep
	}
	return m
}

// isStickyEndpoint returns true if this pool uses STICKY endpoint balancing
// and has a session store available.
func (dp *DestinationPool) isStickyEndpoint() bool {
	d := dp.Destination
	return d.Options != nil &&
		d.Options.EndpointBalancing != nil &&
		d.Options.EndpointBalancing.Algorithm == model.EndpointLBSticky &&
		dp.SessionStore != nil
}

// pickStickyEndpoint uses the session store to pin clients to endpoints.
// New clients get a random healthy endpoint; existing clients return to
// their pinned endpoint if it's still healthy.
func (dp *DestinationPool) pickStickyEndpoint(r *http.Request, w http.ResponseWriter, healthy []*Endpoint) *Endpoint {
	cfg := dp.Destination.Options.EndpointBalancing.Sticky
	cookieName := "_vrata_endpoint_pin"
	var ttlStr string
	if cfg != nil && cfg.Cookie != nil {
		if cfg.Cookie.Name != "" {
			cookieName = cfg.Cookie.Name
		}
		ttlStr = cfg.Cookie.TTL
	}

	sid := ""
	if c, err := r.Cookie(cookieName); err == nil {
		sid = c.Value
	}

	isNew := sid == ""
	if isNew {
		sid = generateSessionID()
		ttl := parseTTL(ttlStr, time.Hour)
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    sid,
			Path:     "/",
			MaxAge:   int(ttl.Seconds()),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	destID := dp.Destination.ID
	storeKey := sid + ":" + destID

	healthySet := make(map[string]*Endpoint, len(healthy))
	for _, ep := range healthy {
		healthySet[ep.ID] = ep
	}

	if !isNew {
		if epID, err := dp.SessionStore.Get(r.Context(), storeKey, "ep"); err == nil && epID != "" {
			if ep, ok := healthySet[epID]; ok {
				return ep
			}
		}
	}

	ep := healthy[rand.Intn(len(healthy))]
	ttlSec := int(parseTTL(ttlStr, time.Hour).Seconds())
	if err := dp.SessionStore.Set(r.Context(), storeKey, "ep", ep.ID, ttlSec); err != nil {
		slog.Warn("sticky endpoint: failed to persist session", slog.String("error", err.Error()))
	}
	return ep
}


