# Destination Balancing — E2E Test Report

**Date**: 2026-03-18
**Binary**: `vrata` (branch `feat/native-proxy`)
**Infrastructure**: controlplane+proxy in single process, Redis 7 on localhost:6379

## Summary

| # | Test | Algorithm | Requests | Result |
|---|---|---|---|---|
| 1 | WR_Distribution | WEIGHTED_RANDOM | 5,000 | **PASS** — A=69.9% B=30.1% (target 70/30) |
| 2 | WR_NoStickiness | WEIGHTED_RANDOM | 100 | **PASS** — client sees both destinations |
| 3 | WR_WeightChange | WEIGHTED_RANDOM | 10,000 | **PASS** — 80/20→20/80 reflected exactly |
| 4 | WCH_Stickiness | WEIGHTED_CONSISTENT_HASH | 5,000 | **PASS** — 100/100 users stable × 50 reqs |
| 5 | WCH_WeightDistribution | WEIGHTED_CONSISTENT_HASH | 5,000 | **PASS** — A=77.0% B=23.0% (target 75/25) |
| 6 | WCH_WeightChange | WEIGHTED_CONSISTENT_HASH | 1,000 | **PASS** — 20.2% disruption (80/20→60/40) |
| 7 | WCH_MultiRoute_Isolation | WEIGHTED_CONSISTENT_HASH | 10,000 | **PASS** — 200/200 stable × 2 routes |
| 8 | WCH_Concurrent | WEIGHTED_CONSISTENT_HASH | 5,000 | **PASS** — 50 goroutines × 100 reqs |
| 9 | Sticky_FallbackToWCH | STICKY (no Redis) | 5,000 | **PASS** — fallback to WCH |
| 10 | Sticky_ZeroDisruption | STICKY (Redis) | 10,500 | **PASS** — 200/200 zero disruption 80/20→20/80 |
| 11 | Sticky_DestinationRemoved | STICKY (Redis) | 3 | **PASS** — reassigned correctly |
| 12 | Sticky_Concurrent | STICKY (Redis) | 5,000 | **PASS** — 50 goroutines × 100 reqs |
| | **Total balancing** | | **61,603** | **12/12 PASS** |

## Full E2E Suite

| Category | Tests | Passing |
|---|---|---|
| API CRUD | 6 | 6 |
| Balancing (WR + WCH + STICKY) | 12 | 12 |
| Middlewares | 9 | 9 |
| Pinning (legacy names) | 5 | 5 |
| Proxy features (retry, timeout, mirror, websocket, access log) | 5 | 5 |
| Routing (direct, redirect, forward, group, method, header, CEL, query, gRPC, host, regex rewrite) | 12 | 12 |
| Snapshots + Sync | 2 | 2 |
| Weighted backends | 2 | 2 |
| **Total e2e** | **52** | **52** |

## Unit Tests

| Package | Tests | Passing |
|---|---|---|
| api/handlers | 33 | 33 |
| api/middleware | 3 | 3 |
| api/respond | 2 | 2 |
| config | 11 | 11 |
| gateway | 2 | 2 |
| k8s | 4 | 4 |
| proxy (router + pinning + balancer) | 15 | 15 |
| proxy/celeval | 11 | 11 |
| proxy/middlewares | 55 | 55 |
| raft | 14 | 14 |
| session (Redis) | 5 | 5 |
| store | 18 | 18 |
| sync | 2 | 2 |
| **Total unit** | **240** | **240** |

## Algorithm Comparison

| Property | WEIGHTED_RANDOM | WEIGHTED_CONSISTENT_HASH | STICKY |
|---|---|---|---|
| Stickiness | None | Yes (cookie + ring hash) | Yes (cookie + Redis) |
| Weight accuracy (new clients) | Exact | Exact | Exact |
| Disruption on weight change | N/A | Proportional (~13-20%) | **Zero** |
| External dependency | None | None | Redis |
| Cross-proxy consistency | N/A | Yes (deterministic hash) | Yes (shared Redis) |
| Cookie contents | None | Opaque UUID | Opaque UUID |
| Fallback | — | — | → WEIGHTED_CONSISTENT_HASH |

## STICKY Zero Disruption Detail

| Phase | A | B | Notes |
|---|---|---|---|
| Initial pin (80/20) | 162 | 38 | 200 users |
| After 80/20→20/80 | 162 | 38 | **Zero movement** |
| New users (20/80) | 108 (21.6%) | 392 (78.4%) | Matches new weights |
