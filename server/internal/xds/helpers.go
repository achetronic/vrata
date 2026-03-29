// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package xds

import (
	"fmt"
	"time"

	accesslogv3 "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	httpmgr "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/types/known/anypb"
	durationpbpkg "google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// buildHCM builds an Envoy HTTP Connection Manager filter with the given
// route config name, HTTP filters (middlewares + router), and access logs.
func buildHCM(routeConfigName string, filters []*httpmgr.HttpFilter, accessLogs []*accesslogv3.AccessLog) *listenerv3.Filter {
	if len(filters) == 0 {
		filters = buildHTTPFilters(nil, false)
	}

	hcm := &httpmgr.HttpConnectionManager{
		StatPrefix: "ingress_http",
		RouteSpecifier: &httpmgr.HttpConnectionManager_Rds{
			Rds: &httpmgr.Rds{
				RouteConfigName: routeConfigName,
				ConfigSource: &corev3.ConfigSource{
					ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
					ResourceApiVersion:    corev3.ApiVersion_V3,
				},
			},
		},
		HttpFilters: filters,
		AccessLog:   accessLogs,
	}

	hcmAny, _ := anypb.New(hcm)
	return &listenerv3.Filter{
		Name:       "envoy.filters.network.http_connection_manager",
		ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: hcmAny},
	}
}

// buildDownstreamTLS builds an Envoy DownstreamTlsContext for TLS termination.
// certPath and keyPath are paths inside the Envoy container.
// minVersion/maxVersion map to TLS protocol versions ("TLSv1_2", "TLSv1_3", ...).
// If requireClientCert is true, mTLS client auth is enabled with the given caPath.
func buildDownstreamTLS(certPath, keyPath, caPath, minVersion, maxVersion string, requireClientCert bool) (*anypb.Any, error) {
	tlsContext := &tlsv3.DownstreamTlsContext{
		CommonTlsContext: &tlsv3.CommonTlsContext{
			TlsCertificates: []*tlsv3.TlsCertificate{
				{
					CertificateChain: &corev3.DataSource{
						Specifier: &corev3.DataSource_Filename{Filename: certPath},
					},
					PrivateKey: &corev3.DataSource{
						Specifier: &corev3.DataSource_Filename{Filename: keyPath},
					},
				},
			},
			TlsParams: buildTLSParams(minVersion, maxVersion),
		},
	}

	if requireClientCert && caPath != "" {
		tlsContext.RequireClientCertificate = wrapperspb.Bool(true)
		tlsContext.CommonTlsContext.ValidationContextType = &tlsv3.CommonTlsContext_ValidationContext{
			ValidationContext: &tlsv3.CertificateValidationContext{
				TrustedCa: &corev3.DataSource{
					Specifier: &corev3.DataSource_Filename{Filename: caPath},
				},
			},
		}
	}

	a, err := anypb.New(tlsContext)
	if err != nil {
		return nil, fmt.Errorf("marshalling downstream TLS context: %w", err)
	}
	return a, nil
}

// buildTLSParams maps Vrata TLS version strings to Envoy TlsParameters.
func buildTLSParams(minVersion, maxVersion string) *tlsv3.TlsParameters {
	params := &tlsv3.TlsParameters{}

	minMap := map[string]tlsv3.TlsParameters_TlsProtocol{
		"TLSv1_0": tlsv3.TlsParameters_TLSv1_0,
		"TLSv1_1": tlsv3.TlsParameters_TLSv1_1,
		"TLSv1_2": tlsv3.TlsParameters_TLSv1_2,
		"TLSv1_3": tlsv3.TlsParameters_TLSv1_3,
	}

	if v, ok := minMap[minVersion]; ok {
		params.TlsMinimumProtocolVersion = v
	} else {
		params.TlsMinimumProtocolVersion = tlsv3.TlsParameters_TLSv1_2
	}

	if v, ok := minMap[maxVersion]; ok {
		params.TlsMaximumProtocolVersion = v
	}

	return params
}

// weightedCluster holds a cluster name and its relative weight for weighted routing.
type weightedCluster struct {
	name   string
	weight int
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

