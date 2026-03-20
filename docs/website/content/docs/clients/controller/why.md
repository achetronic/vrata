---
title: "Why the Controller?"
weight: 1
---

If you already use Gateway API to manage routing in Kubernetes, you don't want to rewrite everything for Vrata's REST API. The controller bridges the gap.

## The Gateway API problem

Gateway API is the Kubernetes standard for routing. But it has hard limits baked into the spec:

- **16 hostnames** per HTTPRoute (etcd object size constraints)
- **64 rules** per HTTPRoute
- CEL validations that prevent larger objects

If you serve the same app on 74 country-specific subdomains (like `fr.example.com`, `de.example.com`, ...), you need **5 identical HTTPRoutes** just to shard hostnames. At scale this creates thousands of duplicate objects that are painful to manage and slow to reconcile.

These limits were one of the motivations behind Vrata. A single Vrata RouteGroup can carry any number of hostnames with no artificial ceiling. The controller includes a **SuperHTTPRoute** CRD that removes these constraints while staying compatible with the Gateway API spec — same fields, same semantics, no limits.

## What the controller does

1. Watches `HTTPRoute`, `Gateway`, and `SuperHTTPRoute` resources in your cluster.
2. Translates each resource into Vrata entities (Routes, RouteGroups, Destinations, Listeners, Middlewares).
3. Calls the Vrata REST API to create/update/delete those entities.
4. Batches changes and creates a versioned snapshot when the batch is ready.

It's unidirectional: Kubernetes → Vrata. The controller never writes config back to Kubernetes (except status conditions on HTTPRoutes).

## What it doesn't do

- Does not optimise or collapse routes.
- Does not manage TLS certificates (Gateway references Secrets; this is a known gap).
- Does not touch entities created manually via the API (`k8s:` prefix is the ownership boundary).
