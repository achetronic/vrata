---
title: "Snapshots"
weight: 6
---

A Snapshot is an immutable, versioned copy of the entire proxy configuration at a point in time. Snapshots are how Vrata implements safe, atomic configuration updates with instant rollback.

## Why snapshots exist

Most proxies apply changes immediately — you push a new route and it's live. If the config is wrong, traffic breaks.

Vrata separates **editing** from **deploying**. You create, update, and delete entities via the API — but nothing reaches the running proxies until you explicitly capture a snapshot and activate it. This gives you:

- **Atomic updates** — all changes go live at once, or none do
- **Instant rollback** — activate any previous snapshot in one API call
- **Audit trail** — every snapshot is a named, timestamped record of what was deployed
- **Safe experimentation** — make changes, review them, then decide whether to deploy

## The flow

```
Edit → Capture → Activate → (optional) Rollback
```

1. **Edit** — create/update/delete routes, destinations, middlewares, listeners, groups via the API. Changes are staged.
2. **Capture** — `POST /api/v1/snapshots` takes a point-in-time copy of all current entities.
3. **Activate** — `POST /api/v1/snapshots/{id}/activate` pushes the snapshot to all connected proxies via SSE.
4. **Rollback** — activate any previous snapshot. One call, instant. See [Rollback]({{< relref "rollback" >}}).

## What's inside a snapshot

A snapshot is a complete, self-contained JSON document containing all Listeners, Destinations, Routes, RouteGroups, and Middlewares. The proxy doesn't need access to the database — it only needs the snapshot.

## How proxies receive snapshots

The control plane serves an SSE stream at `GET /sync/snapshot`. Proxy-mode instances connect and receive the active snapshot immediately, plus any future activations. If a proxy restarts, it reconnects and gets the current snapshot automatically.
