---
title: "Configuration"
weight: 5
---

Vrata uses a single YAML file for configuration. The binary accepts `--config path/to/config.yaml`.

All string values support `${ENV_VAR}` substitution — the raw YAML is expanded before parsing.

| Page | What it covers |
|------|---------------|
| [Server Config]({{< relref "server" >}}) | Full `config.yaml` reference for both modes |
| [HA with Raft]({{< relref "ha-raft" >}}) | Multi-node control plane with embedded Raft consensus |
