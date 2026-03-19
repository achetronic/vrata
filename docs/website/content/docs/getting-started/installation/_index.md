---
title: "Installation"
weight: 4
---

Three ways to run Vrata — pick the one that fits your setup.

| Method | Best for | What you get |
|--------|----------|-------------|
| [Binary]({{< relref "binary" >}}) | Bare metal, VMs, local dev | Download a single binary from the GitHub release |
| [Docker]({{< relref "docker" >}}) | Containers, Docker Compose | Official multi-arch image with server + controller |
| [Helm]({{< relref "helm" >}}) | Kubernetes | Full chart with control plane, proxy, and optional controller |

All three methods use the same YAML config format and expose the same REST API.
