---
title: "Rollback"
weight: 2
---

Rollback in Vrata is instant — activate any previous snapshot and every proxy receives the old configuration within milliseconds.

## How it works

1. List snapshots to find the target version:

```bash
curl localhost:8080/api/v1/snapshots
```

2. Activate the previous snapshot:

```bash
curl -X POST localhost:8080/api/v1/snapshots/<snapshot-id>/activate
```

That's it. The control plane pushes the snapshot to all connected proxies via SSE. In-flight requests complete on the current config; new requests use the rolled-back config. Zero dropped connections.

## What changes

When you activate a snapshot, **every entity** reverts to the state captured in that snapshot:

- Listeners
- Destinations
- Routes
- RouteGroups
- Middlewares

This is an atomic, all-or-nothing switch. There's no partial rollback — you activate the entire snapshot.

## Keeping old snapshots

Snapshots are immutable and persist in bbolt. You can keep as many as you want. Delete old ones when you no longer need them:

```bash
curl -X DELETE localhost:8080/api/v1/snapshots/<snapshot-id>
```

You cannot delete the currently active snapshot.

## Rollback strategies

### Blue/green deploys

1. Create snapshot `v2.0` with the new config
2. Activate `v2.0`
3. Monitor metrics for errors
4. If errors spike → activate `v1.9` (instant rollback)

### Canary with snapshots

1. Create snapshot `v2.0-canary` with weighted destinations (90% old, 10% new)
2. Activate and monitor
3. Gradually create new snapshots increasing the weight
4. If anything breaks → activate the pre-canary snapshot

### CI/CD integration

```bash
SNAP=$(curl -s -X POST localhost:8080/api/v1/snapshots \
  -H 'Content-Type: application/json' \
  -d '{"name": "deploy-'$BUILD_ID'"}' | jq -r .id)

curl -X POST localhost:8080/api/v1/snapshots/$SNAP/activate

sleep 30

ERROR_RATE=$(curl -s localhost:9090/api/v1/query?query=rate(vrata_proxy_errors_total[1m]) | jq '.data.result[0].value[1]')

if (( $(echo "$ERROR_RATE > 0.01" | bc -l) )); then
  curl -X POST localhost:8080/api/v1/snapshots/$PREVIOUS_SNAP/activate
  echo "Rolled back due to error rate: $ERROR_RATE"
fi
```
