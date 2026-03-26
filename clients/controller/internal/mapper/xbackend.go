// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package mapper

import (
	"fmt"

	"github.com/achetronic/vrata/clients/controller/apis/agentic"
	"github.com/achetronic/vrata/clients/controller/internal/vrata"
)

// AgenticPrefix is the naming prefix for entities created from agentic resources.
const AgenticPrefix = "k8s:agentic:"

// IsAgenticOwned returns true if the entity name starts with the agentic prefix.
func IsAgenticOwned(name string) bool {
	return len(name) > len(AgenticPrefix) && name[:len(AgenticPrefix)] == AgenticPrefix
}

// MappedXBackend holds the Vrata entities produced from a single XBackend.
type MappedXBackend struct {
	Destination vrata.Destination
	Route       vrata.Route
}

// MapXBackend translates an XBackend into a Vrata Destination and Route.
func MapXBackend(backend *agentic.XBackend) MappedXBackend {
	ns := backend.Namespace
	name := backend.Name
	prefix := fmt.Sprintf("%s%s/%s", AgenticPrefix, ns, name)

	mcp := backend.Spec.MCP
	path := mcp.Path
	if path == "" {
		path = "/mcp"
	}

	var host string
	var opts map[string]any
	if mcp.ServiceName != nil && *mcp.ServiceName != "" {
		host = fmt.Sprintf("%s.%s.svc.cluster.local", *mcp.ServiceName, ns)
	} else if mcp.Hostname != nil && *mcp.Hostname != "" {
		host = *mcp.Hostname
		opts = map[string]any{"tls": map[string]any{"mode": "tls"}}
	}

	dest := vrata.Destination{
		Name:    prefix,
		Host:    host,
		Port:    uint32(mcp.Port),
		Options: opts,
	}

	route := vrata.Route{
		Name:  prefix + "/mcp",
		Match: map[string]any{"pathPrefix": path},
		Forward: map[string]any{
			"destinations": []map[string]any{
				{"destinationId": prefix},
			},
		},
	}

	return MappedXBackend{
		Destination: dest,
		Route:       route,
	}
}
