# Endpoint Balancing — E2E Test Report

**Date**: 2026-03-18
**Binary**: `vrata` (branch `feat/native-proxy`)
**Architecture**: `Destination` → `Endpoints []model.Endpoint` → `proxy.DestinationPool` → N `proxy.Endpoint`

## Summary

| # | Test | Algorithm | Endpoints | Requests | Result |
|---|---|---|---|---|---|
| 1 | RoundRobin | ROUND_ROBIN | 3 | 6,000 | **PASS** — 33.3% / 33.3% / 33.3% |
| 2 | Random | RANDOM | 3 | 6,000 | **PASS** — 33.4% / 33.2% / 33.4% |
| 3 | RingHash_Header | RING_HASH (header) | 3 | 5,000 | **PASS** — 100/100 users stable |
| 4 | RingHash_Cookie | RING_HASH (cookie) | 3 | 5,000 | **PASS** — 100/100 users stable |
| 5 | RingHash_SourceIP | RING_HASH (sourceIP) | 3 | 5,000 | **PASS** — 100% same endpoint |
| 6 | Maglev | MAGLEV (header) | 3 | 5,000 | **PASS** — 100/100 users stable |
| 7 | Combined L1+L2 | WCH + RING_HASH | 2×3 | 5,000 | **PASS** — 50/50 concurrent stable |
| 8 | Default | none (random fallback) | 3 | 6,000 | **PASS** — traffic to all 3 |
| | **Total** | | | **43,000** | **8/8 PASS** |

## Full Test Suite

| Category | Tests | Passing |
|---|---|---|
| API CRUD | 6 | 6 |
| Destination balancing (WR + WCH + STICKY) | 12 | 12 |
| Endpoint balancing | 8 | 8 |
| Middlewares | 9 | 9 |
| Pinning (legacy) | 5 | 5 |
| Proxy features | 5 | 5 |
| Routing | 12 | 12 |
| Snapshots + Sync | 2 | 2 |
| Weighted backends | 2 | 2 |
| **Total e2e** | **60** | **60** |

## Unit Tests

| Package | Tests |
|---|---|
| All 13 packages | 242 |
| **All pass** | **242/242** |

## Key Results

### ROUND_ROBIN
Perfect 33.3% distribution across 3 endpoints — exactly N/3 requests each.

### RANDOM
Near-uniform distribution (32.5–33.8%) — expected variance for random.

### RING_HASH (header)
100/100 users × 50 requests each = 5,000 requests. Every user always hit the same
endpoint. The X-User-ID header deterministically selects the endpoint via the hash ring.

### RING_HASH (cookie)
Auto-generated `_vrata_ep_test` cookie pins each client to the same endpoint.
100/100 users × 50 requests = 5,000 requests, zero broken.

### RING_HASH (sourceIP)
All 5,000 requests from 127.0.0.1 went to the same endpoint (deterministic).

### MAGLEV
Same stickiness as RING_HASH — 100/100 users stable via Maglev lookup table.
Maglev provides better distribution uniformity with minimal disruption on
endpoint changes.

### Combined L1 (WCH) + L2 (RING_HASH)
50 concurrent goroutines × 100 requests each = 5,000 requests.
Level 1 pins each user to a destination (A or B) via cookie hash.
Level 2 pins within that destination to a specific endpoint via header hash.
Both levels stable simultaneously under concurrent load.

### Default (no endpointBalancing)
Without explicit configuration, traffic reaches all 3 endpoints via random
selection. No endpoint is starved.
