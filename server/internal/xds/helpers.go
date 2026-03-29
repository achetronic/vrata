// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package xds

import (
	"fmt"
	"time"

	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	httpmgr "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/anypb"
	durationpbpkg "google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// buildHCM builds an Envoy HTTP Connection Manager filter that references an
// RDS route configuration by name.
func buildHCM(routeConfigName string) *listenerv3.Filter {
	hcm := &httpmgr.HttpConnectionManager{
		StatPrefix: "ingress_http",
		RouteSpecifier: &httpmgr.HttpConnectionManager_Rds{
			Rds: &httpmgr.Rds{
				RouteConfigName: routeConfigName,
			},
		},
		HttpFilters: []*httpmgr.HttpFilter{
			{Name: "envoy.filters.http.router"},
		},
	}

	hcmAny, _ := anypb.New(hcm)
	return &listenerv3.Filter{
		Name:       "envoy.filters.network.http_connection_manager",
		ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: hcmAny},
	}
}

// weightedCluster holds a cluster name and its relative weight.
type weightedCluster struct {
	name   string
	weight int
}

// buildWeightedClusters builds a weighted cluster action for load balancing
// across multiple destinations. Used when a Group has multiple destinations
// with weights.
func buildWeightedClusters(clusters []weightedCluster) *routev3.RouteAction_WeightedClusters {
	wc := make([]*routev3.WeightedCluster_ClusterWeight, 0, len(clusters))
	for _, c := range clusters {
		wc = append(wc, &routev3.WeightedCluster_ClusterWeight{
			Name:   c.name,
			Weight: wrapperspb.UInt32(uint32(c.weight)),
		})
	}
	return &routev3.RouteAction_WeightedClusters{
		WeightedClusters: &routev3.WeightedCluster{
			Clusters: wc,
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Naming helpers
// ─────────────────────────────────────────────────────────────────────────────

func clusterName(destinationID string) string {
	return "dest_" + destinationID
}

func routeConfigName(listenerID string) string {
	return "rc_" + listenerID
}

// ─────────────────────────────────────────────────────────────────────────────
// Protobuf helpers
// ─────────────────────────────────────────────────────────────────────────────

// durationpb creates a protobuf Duration from seconds.
func durationpb(seconds int64) *durationpbpkg.Duration {
	return &durationpbpkg.Duration{Seconds: seconds}
}

// parseDuration parses a Go duration string into a protobuf Duration.
func parseDuration(s string) (*durationpbpkg.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("parsing duration %q: %w", s, err)
	}
	return durationpbpkg.New(d), nil
}
