# Project TODO — Vrata

## In Progress

### Documentation website — deep content iteration

The website structure exists (Hugo + custom theme + 36 pages) but the content
is too shallow. Each concept page needs to become a **section with sub-pages**
that explain every sub-concept in depth.

**Sidebar redesign:**
- Collapsible tree in the sidebar
- Sections expand on click, default expanded for active section
- Sub-pages visible under their parent when expanded

**Content restructuring — each concept becomes a section:**

```
docs/concepts/
├── _index.md                    # Overview of all concepts
├── listeners/
│   ├── _index.md                # What listeners are, when you need them
│   ├── tls.md                   # TLS termination in depth
│   ├── timeouts.md              # Client timeouts with diagrams
│   ├── metrics.md               # Per-listener Prometheus metrics
│   └── http2.md                 # HTTP/2 and h2c
├── destinations/
│   ├── _index.md                # What destinations are, how they work
│   ├── timeouts.md              # All 7 destination timeouts explained
│   ├── endpoint-balancing.md    # 6 algorithms with when-to-use guidance
│   ├── circuit-breaker.md       # How it works, threshold, open duration
│   ├── health-checks.md         # Active probes, thresholds, intervals
│   ├── outlier-detection.md     # Passive ejection, consecutive errors
│   ├── tls-upstream.md          # TLS/mTLS to backends
│   └── kubernetes-discovery.md  # EndpointSlice, ExternalName
├── routes/
│   ├── _index.md                # What routes are, the three action modes
│   ├── matching.md              # Path, headers, methods, query, CEL
│   ├── forward.md               # Forward action, destination refs, weights
│   ├── destination-balancing.md # Level 1: WR, WCH, STICKY
│   ├── retry.md                 # Retry with backoff, per-attempt timeout
│   ├── rewrite.md               # Path prefix, regex, host override
│   ├── mirror.md                # Traffic mirroring, fire-and-forget
│   ├── redirect.md              # HTTP redirects, status codes
│   ├── direct-response.md       # Fixed responses, maintenance pages
│   └── on-error.md              # Fallback routes, error types, wildcards
├── groups/
│   ├── _index.md                # What groups are, path composition
│   ├── path-composition.md      # All 8 composition cases with examples
│   ├── hostname-merging.md      # Union semantics, no limits
│   └── middleware-inheritance.md # Override precedence
├── middlewares/
│   ├── _index.md                # Overview of all 7 types
│   ├── jwt.md                   # Full JWT deep dive
│   ├── cors.md                  # CORS with regex origins
│   ├── rate-limit.md            # Token bucket, trusted proxies
│   ├── ext-authz.md             # HTTP + gRPC modes
│   ├── ext-proc.md              # All body modes, observe-only
│   ├── headers.md               # Header manipulation
│   ├── access-log.md            # Structured logging
│   └── conditions.md            # skipWhen, onlyWhen, disabled
├── snapshots/
│   ├── _index.md                # Versioned config lifecycle
│   └── rollback.md              # Instant rollback workflow
└── metrics/
    ├── _index.md                # Overview, 5 dimensions
    ├── route-metrics.md         # 7 route metrics detailed
    ├── destination-metrics.md   # 4 destination metrics
    ├── endpoint-metrics.md      # 4 endpoint metrics (high cardinality)
    ├── middleware-metrics.md     # 3 middleware metrics
    └── listener-metrics.md      # 3 listener metrics
```

**Each sub-page must include:**
1. Plain English explanation — what it is, why you need it
2. How it works internally (brief, not code-level)
3. Full JSON example with ALL fields
4. Field reference table with types, defaults, descriptions
5. Common use cases with complete examples
6. Gotchas / things to watch out for

**Landing page:**
- Remove comparison table (move to why-vrata.md)
- Keep hero, feature cards, architecture

**Clients already inside docs/clients/ — no change needed.**
