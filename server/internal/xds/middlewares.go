// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// This file translates Vrata Middleware entities into Envoy HTTP filter configs.
package xds

import (
	"time"

	accesslogv3 "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	corsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	extauthzv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	mutationrulesv3 "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	headermutationv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/header_mutation/v3"
	jwtv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	ratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/local_ratelimit/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	fileaccesslogv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	httpmgr "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	stringmatcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/protobuf/types/known/anypb"
	durationpbpkg "google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/achetronic/vrata/internal/model"
)

// buildHTTPFilters builds the ordered list of Envoy HTTP filters for an HCM.
// withXFCC injects the xfcc Go filter first (when listener has mTLS).
// The router filter is always last.
func buildHTTPFilters(middlewares []model.Middleware, withXFCC bool) []*httpmgr.HttpFilter {
	filters := make([]*httpmgr.HttpFilter, 0, len(middlewares)+3)

	if withXFCC {
		if f := buildGoPluginFilter("vrata.xfcc", "/etc/envoy/extensions/xfcc.so"); f != nil {
			filters = append(filters, f)
		}
	}

	for _, mw := range middlewares {
		var f *httpmgr.HttpFilter
		switch mw.Type {
		case model.MiddlewareTypeCORS:
			f = buildCORSFilter(mw)
		case model.MiddlewareTypeJWT:
			f = buildJWTFilter(mw)
		case model.MiddlewareTypeExtAuthz:
			f = buildExtAuthzFilter(mw)
		case model.MiddlewareTypeRateLimit:
			f = buildRateLimitFilter(mw)
		case model.MiddlewareTypeHeaders:
			f = buildHeadersFilter(mw)
		case model.MiddlewareTypeAccessLog:
			// AccessLog is handled at the HCM level, not as an HTTP filter.
		case "inlineAuthz":
			f = buildGoPluginFilter("vrata.inlineauthz", "/etc/envoy/extensions/inlineauthz.so")
		}
		if f != nil {
			filters = append(filters, f)
		}
	}

	routerAny, _ := anypb.New(&routerv3.Router{})
	filters = append(filters, &httpmgr.HttpFilter{
		Name:       "envoy.filters.http.router",
		ConfigType: &httpmgr.HttpFilter_TypedConfig{TypedConfig: routerAny},
	})

	return filters
}

// ─────────────────────────────────────────────────────────────────────────────
// CORS
// ─────────────────────────────────────────────────────────────────────────────

func buildCORSFilter(mw model.Middleware) *httpmgr.HttpFilter {
	if mw.CORS == nil {
		return nil
	}

	cfg := &corsv3.CorsPolicy{}
	for _, o := range mw.CORS.AllowOrigins {
		var sm *stringmatcherv3.StringMatcher
		if o.Regex {
			sm = &stringmatcherv3.StringMatcher{
				MatchPattern: &stringmatcherv3.StringMatcher_SafeRegex{
					SafeRegex: &matcherv3.RegexMatcher{
						EngineType: &matcherv3.RegexMatcher_GoogleRe2{GoogleRe2: &matcherv3.RegexMatcher_GoogleRE2{}},
						Regex:      o.Value,
					},
				},
			}
		} else {
			sm = &stringmatcherv3.StringMatcher{MatchPattern: &stringmatcherv3.StringMatcher_Exact{Exact: o.Value}}
		}
		cfg.AllowOriginStringMatch = append(cfg.AllowOriginStringMatch, sm)
	}
	cfg.AllowMethods = joinStrings(mw.CORS.AllowMethods)
	cfg.AllowHeaders = joinStrings(mw.CORS.AllowHeaders)
	cfg.ExposeHeaders = joinStrings(mw.CORS.ExposeHeaders)

	cfgAny, _ := anypb.New(cfg)
	return &httpmgr.HttpFilter{
		Name:       "envoy.filters.http.cors",
		ConfigType: &httpmgr.HttpFilter_TypedConfig{TypedConfig: cfgAny},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// JWT
// ─────────────────────────────────────────────────────────────────────────────

func buildJWTFilter(mw model.Middleware) *httpmgr.HttpFilter {
	if mw.JWT == nil {
		return nil
	}

	provider := &jwtv3.JwtProvider{
		Issuer:    mw.JWT.Issuer,
		Audiences: mw.JWT.Audiences,
	}

	if mw.JWT.JWKsInline != "" {
		provider.JwksSourceSpecifier = &jwtv3.JwtProvider_LocalJwks{
			LocalJwks: &corev3.DataSource{
				Specifier: &corev3.DataSource_InlineString{InlineString: mw.JWT.JWKsInline},
			},
		}
	} else if mw.JWT.JWKsPath != "" && mw.JWT.JWKsDestinationID != "" {
		provider.JwksSourceSpecifier = &jwtv3.JwtProvider_RemoteJwks{
			RemoteJwks: &jwtv3.RemoteJwks{
				HttpUri: &corev3.HttpUri{
					Uri:     "http://" + mw.JWT.JWKsDestinationID + mw.JWT.JWKsPath,
					Timeout: durationpbpkg.New(5 * time.Second),
					HttpUpstreamType: &corev3.HttpUri_Cluster{
						Cluster: clusterName(mw.JWT.JWKsDestinationID),
					},
				},
			},
		}
	}

	cfg := &jwtv3.JwtAuthentication{
		Providers: map[string]*jwtv3.JwtProvider{mw.ID: provider},
		Rules: []*jwtv3.RequirementRule{
			{
				Match: &routev3.RouteMatch{
					PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: "/"},
				},
				RequirementType: &jwtv3.RequirementRule_Requires{
					Requires: &jwtv3.JwtRequirement{
						RequiresType: &jwtv3.JwtRequirement_ProviderName{ProviderName: mw.ID},
					},
				},
			},
		},
	}

	cfgAny, _ := anypb.New(cfg)
	return &httpmgr.HttpFilter{
		Name:       "envoy.filters.http.jwt_authn",
		ConfigType: &httpmgr.HttpFilter_TypedConfig{TypedConfig: cfgAny},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ExtAuthz
// ─────────────────────────────────────────────────────────────────────────────

func buildExtAuthzFilter(mw model.Middleware) *httpmgr.HttpFilter {
	if mw.ExtAuthz == nil {
		return nil
	}

	timeout := 5 * time.Second
	if mw.ExtAuthz.DecisionTimeout != "" {
		if d, err := time.ParseDuration(mw.ExtAuthz.DecisionTimeout); err == nil {
			timeout = d
		}
	}

	var cfg *extauthzv3.ExtAuthz
	if mw.ExtAuthz.Mode == "grpc" {
		cfg = &extauthzv3.ExtAuthz{
			FailureModeAllow: mw.ExtAuthz.FailureModeAllow,
			Services: &extauthzv3.ExtAuthz_GrpcService{
				GrpcService: &corev3.GrpcService{
					TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
						EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{
							ClusterName: clusterName(mw.ExtAuthz.DestinationID),
						},
					},
					Timeout: durationpbpkg.New(timeout),
				},
			},
		}
	} else {
		path := mw.ExtAuthz.Path
		if path == "" {
			path = "/"
		}
		cfg = &extauthzv3.ExtAuthz{
			FailureModeAllow: mw.ExtAuthz.FailureModeAllow,
			Services: &extauthzv3.ExtAuthz_HttpService{
				HttpService: &extauthzv3.HttpService{
					ServerUri: &corev3.HttpUri{
						Uri:     "http://" + mw.ExtAuthz.DestinationID + path,
						Timeout: durationpbpkg.New(timeout),
						HttpUpstreamType: &corev3.HttpUri_Cluster{
							Cluster: clusterName(mw.ExtAuthz.DestinationID),
						},
					},
				},
			},
		}
	}

	cfgAny, _ := anypb.New(cfg)
	return &httpmgr.HttpFilter{
		Name:       "envoy.filters.http.ext_authz",
		ConfigType: &httpmgr.HttpFilter_TypedConfig{TypedConfig: cfgAny},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RateLimit
// ─────────────────────────────────────────────────────────────────────────────

func buildRateLimitFilter(mw model.Middleware) *httpmgr.HttpFilter {
	if mw.RateLimit == nil {
		return nil
	}

	rps := mw.RateLimit.RequestsPerSecond
	if rps <= 0 {
		rps = 10
	}
	burst := mw.RateLimit.Burst
	if burst <= 0 {
		burst = int(rps)
	}

	cfg := &ratelimitv3.LocalRateLimit{
		StatPrefix: "vrata_rl_" + mw.ID,
		TokenBucket: &typev3.TokenBucket{
			MaxTokens:     uint32(burst),
			TokensPerFill: wrapperspb.UInt32(uint32(rps)),
			FillInterval:  durationpbpkg.New(time.Second),
		},
	}

	cfgAny, _ := anypb.New(cfg)
	return &httpmgr.HttpFilter{
		Name:       "envoy.filters.http.local_ratelimit",
		ConfigType: &httpmgr.HttpFilter_TypedConfig{TypedConfig: cfgAny},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Go plugin filter
// ─────────────────────────────────────────────────────────────────────────────

// buildGoPluginFilter builds a filter entry that loads a Go .so plugin.
// Uses envoy.filters.http.dynamic_modules (Envoy 1.33+).
func buildGoPluginFilter(pluginName, libPath string) *httpmgr.HttpFilter {
	s, err := structpb.NewStruct(map[string]interface{}{
		"dynamic_library_path": libPath,
		"filter_name":          pluginName,
	})
	if err != nil {
		return nil
	}
	cfgAny, _ := anypb.New(s)
	return &httpmgr.HttpFilter{
		Name:       "envoy.filters.http.dynamic_modules",
		ConfigType: &httpmgr.HttpFilter_TypedConfig{TypedConfig: cfgAny},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Headers (header mutation)
// ─────────────────────────────────────────────────────────────────────────────

func buildHeadersFilter(mw model.Middleware) *httpmgr.HttpFilter {
	if mw.Headers == nil {
		return nil
	}

	cfg := &headermutationv3.HeaderMutation{
		Mutations: &headermutationv3.Mutations{},
	}

	for _, h := range mw.Headers.RequestHeadersToAdd {
		cfg.Mutations.RequestMutations = append(cfg.Mutations.RequestMutations,
			&mutationrulesv3.HeaderMutation{
				Action: &mutationrulesv3.HeaderMutation_Append{
					Append: &corev3.HeaderValueOption{
						Header:       &corev3.HeaderValue{Key: h.Key, Value: h.Value},
						AppendAction: corev3.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
					},
				},
			},
		)
	}
	for _, name := range mw.Headers.RequestHeadersToRemove {
		cfg.Mutations.RequestMutations = append(cfg.Mutations.RequestMutations,
			&mutationrulesv3.HeaderMutation{
				Action: &mutationrulesv3.HeaderMutation_Remove{Remove: name},
			},
		)
	}
	for _, h := range mw.Headers.ResponseHeadersToAdd {
		cfg.Mutations.ResponseMutations = append(cfg.Mutations.ResponseMutations,
			&mutationrulesv3.HeaderMutation{
				Action: &mutationrulesv3.HeaderMutation_Append{
					Append: &corev3.HeaderValueOption{
						Header:       &corev3.HeaderValue{Key: h.Key, Value: h.Value},
						AppendAction: corev3.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
					},
				},
			},
		)
	}
	for _, name := range mw.Headers.ResponseHeadersToRemove {
		cfg.Mutations.ResponseMutations = append(cfg.Mutations.ResponseMutations,
			&mutationrulesv3.HeaderMutation{
				Action: &mutationrulesv3.HeaderMutation_Remove{Remove: name},
			},
		)
	}

	cfgAny, _ := anypb.New(cfg)
	return &httpmgr.HttpFilter{
		Name:       "envoy.filters.http.header_mutation",
		ConfigType: &httpmgr.HttpFilter_TypedConfig{TypedConfig: cfgAny},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AccessLog (file-based, translated to HCM access_log config)
// ─────────────────────────────────────────────────────────────────────────────

// buildAccessLogs translates Vrata AccessLog middlewares into Envoy access log
// configs that are attached to the HCM, not as HTTP filters.
func buildAccessLogs(middlewares []model.Middleware) []*accesslogv3.AccessLog {
	var logs []*accesslogv3.AccessLog
	for _, mw := range middlewares {
		if mw.Type != model.MiddlewareTypeAccessLog || mw.AccessLog == nil {
			continue
		}

		path := mw.AccessLog.Path
		if path == "" || path == "stdout" {
			path = "/dev/stdout"
		}

		var fileCfg *fileaccesslogv3.FileAccessLog
		if mw.AccessLog.JSON {
			fields := map[string]interface{}{}
			if mw.AccessLog.OnResponse != nil {
				for k, v := range mw.AccessLog.OnResponse.Fields {
					fields[k] = envoyAccessLogVar(v)
				}
			}
			if mw.AccessLog.OnRequest != nil {
				for k, v := range mw.AccessLog.OnRequest.Fields {
					fields[k] = envoyAccessLogVar(v)
				}
			}
			if len(fields) == 0 {
				fields = defaultAccessLogFields()
			}
			jsonFormat, _ := structpb.NewStruct(fields)
			fileCfg = &fileaccesslogv3.FileAccessLog{
				Path: path,
				AccessLogFormat: &fileaccesslogv3.FileAccessLog_LogFormat{
					LogFormat: &corev3.SubstitutionFormatString{
						Format: &corev3.SubstitutionFormatString_JsonFormat{JsonFormat: jsonFormat},
					},
				},
			}
		} else {
			fileCfg = &fileaccesslogv3.FileAccessLog{
				Path: path,
			}
		}

		fileAny, err := anypb.New(fileCfg)
		if err != nil {
			continue
		}
		logs = append(logs, &accesslogv3.AccessLog{
			Name:       "envoy.access_loggers.file",
			ConfigType: &accesslogv3.AccessLog_TypedConfig{TypedConfig: fileAny},
		})
	}
	return logs
}

// envoyAccessLogVar translates Vrata access log variables to Envoy command operators.
func envoyAccessLogVar(v string) string {
	replacements := map[string]string{
		"${request.method}":    "%REQ(:METHOD)%",
		"${request.path}":      "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%",
		"${request.host}":      "%REQ(:AUTHORITY)%",
		"${request.authority}":  "%REQ(:AUTHORITY)%",
		"${request.scheme}":    "%REQ(X-FORWARDED-PROTO)%",
		"${request.clientIp}":  "%DOWNSTREAM_REMOTE_ADDRESS_WITHOUT_PORT%",
		"${response.status}":   "%RESPONSE_CODE%",
		"${response.bytes}":    "%BYTES_SENT%",
		"${duration.ms}":       "%DURATION%",
		"${duration.us}":       "%DURATION%",
		"${duration.s}":        "%DURATION%",
	}
	for from, to := range replacements {
		if v == from {
			return to
		}
	}
	return v
}

// defaultAccessLogFields returns a sensible set of access log fields.
func defaultAccessLogFields() map[string]interface{} {
	return map[string]interface{}{
		"method":     "%REQ(:METHOD)%",
		"path":       "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%",
		"authority":  "%REQ(:AUTHORITY)%",
		"status":     "%RESPONSE_CODE%",
		"duration":   "%DURATION%",
		"bytes_sent": "%BYTES_SENT%",
		"upstream":   "%UPSTREAM_HOST%",
		"client_ip":  "%DOWNSTREAM_REMOTE_ADDRESS_WITHOUT_PORT%",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers local to this file
// ─────────────────────────────────────────────────────────────────────────────

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ","
		}
		result += s
	}
	return result
}

// structpbFromMap creates a structpb.Struct from a map. Alias for clarity.
func structpbFromMap(m map[string]interface{}) (*structpb.Struct, error) {
	return structpb.NewStruct(m)
}
