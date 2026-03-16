package xds

import (
	"fmt"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	mutationrulesv3 "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	rlconfv3 "github.com/envoyproxy/go-control-plane/envoy/config/ratelimit/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	corsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	extauthzv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	headermutv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/header_mutation/v3"
	jwtauthnv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	ratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ratelimit/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/achetronic/rutoso/internal/model"
)

const (
	filterNameCORS      = "envoy.filters.http.cors"
	filterNameJWT       = "envoy.filters.http.jwt_authn"
	filterNameExtAuthz  = "envoy.filters.http.ext_authz"
	filterNameExtProc   = "envoy.filters.http.ext_proc"
	filterNameRateLimit = "envoy.filters.http.ratelimit"
	filterNameHeaders   = "envoy.filters.http.header_mutation"
)

func envoyFilterName(ft model.FilterType) string {
	switch ft {
	case model.FilterTypeCORS:
		return filterNameCORS
	case model.FilterTypeJWT:
		return filterNameJWT
	case model.FilterTypeExtAuthz:
		return filterNameExtAuthz
	case model.FilterTypeExtProc:
		return filterNameExtProc
	case model.FilterTypeRateLimit:
		return filterNameRateLimit
	case model.FilterTypeHeaders:
		return filterNameHeaders
	default:
		return ""
	}
}

// buildHTTPFilterReal builds a fully configured Envoy HttpFilter from a Rutoso
// Filter entity. This replaces the stubs that were in builder.go.
func buildHTTPFilterReal(f model.Filter) (*hcmv3.HttpFilter, error) {
	name := envoyFilterName(f.Type)
	if name == "" {
		return nil, nil
	}
	var msg *anypb.Any
	var err error

	switch f.Type {
	case model.FilterTypeCORS:
		msg, err = marshalCORS(f.CORS)
	case model.FilterTypeJWT:
		msg, err = marshalJWT(f.JWT)
	case model.FilterTypeExtAuthz:
		msg, err = marshalExtAuthz(f.ExtAuthz)
	case model.FilterTypeExtProc:
		msg, err = marshalExtProc(f.ExtProc)
	case model.FilterTypeRateLimit:
		msg, err = marshalRateLimit(f.RateLimit)
	case model.FilterTypeHeaders:
		msg, err = marshalHeaders(f.Headers)
	}
	if err != nil {
		return nil, fmt.Errorf("marshalling filter %q config: %w", f.ID, err)
	}
	hf := &hcmv3.HttpFilter{Name: name}
	if msg != nil {
		hf.ConfigType = &hcmv3.HttpFilter_TypedConfig{TypedConfig: msg}
	}
	return hf, nil
}

// ─── CORS ────────────────────────────────────────────────────────────────────

func marshalCORS(c *model.CORSConfig) (*anypb.Any, error) {
	p := &corsv3.CorsPolicy{}
	if c != nil {
		for _, o := range c.AllowOrigins {
			if o.Regex {
				p.AllowOriginStringMatch = append(p.AllowOriginStringMatch, &matcherv3.StringMatcher{
					MatchPattern: &matcherv3.StringMatcher_SafeRegex{SafeRegex: &matcherv3.RegexMatcher{Regex: o.Value}},
				})
			} else {
				p.AllowOriginStringMatch = append(p.AllowOriginStringMatch, &matcherv3.StringMatcher{
					MatchPattern: &matcherv3.StringMatcher_Exact{Exact: o.Value},
				})
			}
		}
		if len(c.AllowMethods) > 0 {
			p.AllowMethods = strings.Join(c.AllowMethods, ",")
		}
		if len(c.AllowHeaders) > 0 {
			p.AllowHeaders = strings.Join(c.AllowHeaders, ",")
		}
		if len(c.ExposeHeaders) > 0 {
			p.ExposeHeaders = strings.Join(c.ExposeHeaders, ",")
		}
		if c.MaxAge != 0 {
			p.MaxAge = fmt.Sprintf("%d", c.MaxAge)
		}
		if c.AllowCredentials {
			p.AllowCredentials = wrapperspb.Bool(true)
		}
	}
	return anypb.New(p)
}

// ─── JWT ─────────────────────────────────────────────────────────────────────

func marshalJWT(c *model.JWTConfig) (*anypb.Any, error) {
	jwt := &jwtauthnv3.JwtAuthentication{}
	if c == nil || len(c.Providers) == 0 {
		return anypb.New(jwt)
	}
	jwt.Providers = make(map[string]*jwtauthnv3.JwtProvider, len(c.Providers))
	for name, p := range c.Providers {
		jp := &jwtauthnv3.JwtProvider{
			Issuer:    p.Issuer,
			Audiences: p.Audiences,
			Forward:   p.ForwardJWT,
		}
		if p.JWKsURI != "" {
			jp.JwksSourceSpecifier = &jwtauthnv3.JwtProvider_RemoteJwks{
				RemoteJwks: &jwtauthnv3.RemoteJwks{
					HttpUri: &corev3.HttpUri{
						Uri:              p.JWKsURI,
						HttpUpstreamType: &corev3.HttpUri_Cluster{Cluster: name + "_jwks"},
						Timeout:          durationpb.New(5 * time.Second),
					},
				},
			}
		} else if p.JWKsInline != "" {
			jp.JwksSourceSpecifier = &jwtauthnv3.JwtProvider_LocalJwks{
				LocalJwks: &corev3.DataSource{
					Specifier: &corev3.DataSource_InlineString{InlineString: p.JWKsInline},
				},
			}
		}
		for _, cth := range p.ClaimToHeaders {
			jp.ClaimToHeaders = append(jp.ClaimToHeaders, &jwtauthnv3.JwtClaimToHeader{
				HeaderName: cth.Header,
				ClaimName:  cth.Claim,
			})
		}
		jwt.Providers[name] = jp
	}
	for _, r := range c.Rules {
		rule := &jwtauthnv3.RequirementRule{
			Match: &routev3.RouteMatch{
				PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: r.Match},
			},
		}
		if len(r.Requires) == 1 {
			req := &jwtauthnv3.JwtRequirement{
				RequiresType: &jwtauthnv3.JwtRequirement_ProviderName{ProviderName: r.Requires[0]},
			}
			if r.AllowMissing {
				req = &jwtauthnv3.JwtRequirement{
					RequiresType: &jwtauthnv3.JwtRequirement_RequiresAny{
						RequiresAny: &jwtauthnv3.JwtRequirementOrList{
							Requirements: []*jwtauthnv3.JwtRequirement{
								req,
								{RequiresType: &jwtauthnv3.JwtRequirement_AllowMissing{
									AllowMissing: &emptypb.Empty{},
								}},
							},
						},
					},
				}
			}
			rule.RequirementType = &jwtauthnv3.RequirementRule_Requires{Requires: req}
		}
		jwt.Rules = append(jwt.Rules, rule)
	}
	return anypb.New(jwt)
}

// ─── ExtAuthz ────────────────────────────────────────────────────────────────

func marshalExtAuthz(c *model.ExtAuthzConfig) (*anypb.Any, error) {
	ea := &extauthzv3.ExtAuthz{}
	if c != nil {
		ea.FailureModeAllow = c.FailureModeAllow
		if c.GRPCService != "" {
			ea.Services = &extauthzv3.ExtAuthz_GrpcService{
				GrpcService: &corev3.GrpcService{
					TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
						EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{ClusterName: c.GRPCService},
					},
				},
			}
			if c.Timeout != "" {
				if dur, err := time.ParseDuration(c.Timeout); err == nil {
					ea.GetGrpcService().Timeout = durationpb.New(dur)
				}
			}
		} else if c.HTTPService != "" {
			ea.Services = &extauthzv3.ExtAuthz_HttpService{
				HttpService: &extauthzv3.HttpService{
					ServerUri: &corev3.HttpUri{
						Uri:              c.HTTPService,
						HttpUpstreamType: &corev3.HttpUri_Cluster{Cluster: "ext_authz_http"},
						Timeout:          durationpb.New(5 * time.Second),
					},
				},
			}
			if c.Timeout != "" {
				if dur, err := time.ParseDuration(c.Timeout); err == nil {
					ea.GetHttpService().ServerUri.Timeout = durationpb.New(dur)
				}
			}
		}
		if c.IncludeRequestBodyInCheck {
			ea.WithRequestBody = &extauthzv3.BufferSettings{
				MaxRequestBytes:     4096,
				AllowPartialMessage: true,
			}
		}
	}
	return anypb.New(ea)
}

// ─── ExtProc ─────────────────────────────────────────────────────────────────

func marshalExtProc(c *model.ExtProcConfig) (*anypb.Any, error) {
	ep := &extprocv3.ExternalProcessor{}
	if c != nil {
		ep.GrpcService = &corev3.GrpcService{
			TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{ClusterName: c.GRPCService},
			},
		}
		if c.Timeout != "" {
			if dur, err := time.ParseDuration(c.Timeout); err == nil {
				ep.GrpcService.Timeout = durationpb.New(dur)
			}
		}
		if c.ProcessingMode != nil {
			ep.ProcessingMode = mapExtProcMode(c.ProcessingMode)
		}
	}
	return anypb.New(ep)
}

func mapExtProcMode(m *model.ExtProcMode) *extprocv3.ProcessingMode {
	pm := &extprocv3.ProcessingMode{}
	switch m.RequestHeaderMode {
	case model.ExtProcPhaseSend:
		pm.RequestHeaderMode = extprocv3.ProcessingMode_SEND
	case model.ExtProcPhaseSkip:
		pm.RequestHeaderMode = extprocv3.ProcessingMode_SKIP
	}
	switch m.ResponseHeaderMode {
	case model.ExtProcPhaseSend:
		pm.ResponseHeaderMode = extprocv3.ProcessingMode_SEND
	case model.ExtProcPhaseSkip:
		pm.ResponseHeaderMode = extprocv3.ProcessingMode_SKIP
	}
	switch m.RequestBodyMode {
	case model.ExtProcPhaseSend:
		pm.RequestBodyMode = extprocv3.ProcessingMode_STREAMED
	case model.ExtProcPhaseBuffered:
		pm.RequestBodyMode = extprocv3.ProcessingMode_BUFFERED
	case model.ExtProcPhaseSkip:
		pm.RequestBodyMode = extprocv3.ProcessingMode_NONE
	}
	switch m.ResponseBodyMode {
	case model.ExtProcPhaseSend:
		pm.ResponseBodyMode = extprocv3.ProcessingMode_STREAMED
	case model.ExtProcPhaseBuffered:
		pm.ResponseBodyMode = extprocv3.ProcessingMode_BUFFERED
	case model.ExtProcPhaseSkip:
		pm.ResponseBodyMode = extprocv3.ProcessingMode_NONE
	}
	return pm
}

// ─── RateLimit ───────────────────────────────────────────────────────────────

func marshalRateLimit(c *model.RateLimitConfig) (*anypb.Any, error) {
	rl := &ratelimitv3.RateLimit{}
	if c != nil {
		rl.Domain = c.Domain
		rl.FailureModeDeny = c.FailureModeDeny
		rl.RateLimitService = &rlconfv3.RateLimitServiceConfig{
			GrpcService: &corev3.GrpcService{
				TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{ClusterName: c.GRPCService},
				},
			},
			TransportApiVersion: corev3.ApiVersion_V3,
		}
		if c.Timeout != "" {
			if dur, err := time.ParseDuration(c.Timeout); err == nil {
				rl.RateLimitService.GrpcService.Timeout = durationpb.New(dur)
			}
		}
	}
	return anypb.New(rl)
}

// ─── Headers (header mutation) ───────────────────────────────────────────────

func marshalHeaders(c *model.HeadersConfig) (*anypb.Any, error) {
	hm := &headermutv3.HeaderMutation{}
	if c != nil {
		hm.Mutations = buildMutations(c)
	}
	return anypb.New(hm)
}

func buildMutations(c *model.HeadersConfig) *headermutv3.Mutations {
	m := &headermutv3.Mutations{}
	for _, h := range c.RequestHeadersToAdd {
		m.RequestMutations = append(m.RequestMutations, &mutationrulesv3.HeaderMutation{
			Action: &mutationrulesv3.HeaderMutation_Append{
				Append: &corev3.HeaderValueOption{
					Header:       &corev3.HeaderValue{Key: h.Key, Value: h.Value},
					AppendAction: headerAppendAction(h.Append),
				},
			},
		})
	}
	for _, name := range c.RequestHeadersToRemove {
		m.RequestMutations = append(m.RequestMutations, &mutationrulesv3.HeaderMutation{
			Action: &mutationrulesv3.HeaderMutation_Remove{Remove: name},
		})
	}
	for _, h := range c.ResponseHeadersToAdd {
		m.ResponseMutations = append(m.ResponseMutations, &mutationrulesv3.HeaderMutation{
			Action: &mutationrulesv3.HeaderMutation_Append{
				Append: &corev3.HeaderValueOption{
					Header:       &corev3.HeaderValue{Key: h.Key, Value: h.Value},
					AppendAction: headerAppendAction(h.Append),
				},
			},
		})
	}
	for _, name := range c.ResponseHeadersToRemove {
		m.ResponseMutations = append(m.ResponseMutations, &mutationrulesv3.HeaderMutation{
			Action: &mutationrulesv3.HeaderMutation_Remove{Remove: name},
		})
	}
	return m
}

func headerAppendAction(appendMode bool) corev3.HeaderValueOption_HeaderAppendAction {
	if appendMode {
		return corev3.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD
	}
	return corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD
}

// ─── per_filter_config (FilterOverrides) ─────────────────────────────────────

// buildPerFilterConfig merges group and route FilterOverrides into the Envoy
// typed_per_filter_config map for a route. Group overrides are the base; route
// overrides win on key collision (more specific takes precedence).
func buildPerFilterConfig(
	groupOverrides map[string]model.FilterOverride,
	routeOverrides map[string]model.FilterOverride,
	filterByID map[string]model.Filter,
) map[string]*anypb.Any {
	merged := make(map[string]model.FilterOverride)
	for id, ov := range groupOverrides {
		merged[id] = ov
	}
	for id, ov := range routeOverrides {
		merged[id] = ov // route wins
	}
	if len(merged) == 0 {
		return nil
	}

	result := make(map[string]*anypb.Any)
	for filterID, ov := range merged {
		f, ok := filterByID[filterID]
		if !ok {
			continue
		}
		name := envoyFilterName(f.Type)
		if name == "" {
			continue
		}
		var a *anypb.Any
		var err error

		if ov.Disabled {
			a, err = marshalDisabledOverride(f.Type)
		} else {
			a, err = marshalFilterOverride(f.Type, ov)
		}
		if err != nil || a == nil {
			continue
		}
		result[name] = a
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func marshalDisabledOverride(ft model.FilterType) (*anypb.Any, error) {
	switch ft {
	case model.FilterTypeCORS:
		return anypb.New(&corsv3.CorsPolicy{})
	case model.FilterTypeJWT:
		return anypb.New(&jwtauthnv3.PerRouteConfig{
			RequirementSpecifier: &jwtauthnv3.PerRouteConfig_Disabled{Disabled: true},
		})
	case model.FilterTypeExtAuthz:
		return anypb.New(&extauthzv3.ExtAuthzPerRoute{
			Override: &extauthzv3.ExtAuthzPerRoute_Disabled{Disabled: true},
		})
	case model.FilterTypeExtProc:
		return anypb.New(&extprocv3.ExtProcPerRoute{
			Override: &extprocv3.ExtProcPerRoute_Disabled{Disabled: true},
		})
	case model.FilterTypeRateLimit:
		return anypb.New(&ratelimitv3.RateLimitPerRoute{})
	case model.FilterTypeHeaders:
		return anypb.New(&headermutv3.HeaderMutationPerRoute{})
	default:
		return nil, nil
	}
}

func marshalFilterOverride(ft model.FilterType, ov model.FilterOverride) (*anypb.Any, error) {
	switch ft {
	case model.FilterTypeJWT:
		if ov.JWTProvider != "" {
			return anypb.New(&jwtauthnv3.PerRouteConfig{
				RequirementSpecifier: &jwtauthnv3.PerRouteConfig_RequirementName{
					RequirementName: ov.JWTProvider,
				},
			})
		}
	case model.FilterTypeExtAuthz:
		if len(ov.ExtAuthzContextExtensions) > 0 {
			return anypb.New(&extauthzv3.ExtAuthzPerRoute{
				Override: &extauthzv3.ExtAuthzPerRoute_CheckSettings{
					CheckSettings: &extauthzv3.CheckSettings{
						ContextExtensions: ov.ExtAuthzContextExtensions,
					},
				},
			})
		}
	case model.FilterTypeExtProc:
		if ov.ExtProcMode != nil {
			return anypb.New(&extprocv3.ExtProcPerRoute{
				Override: &extprocv3.ExtProcPerRoute_Overrides{
					Overrides: &extprocv3.ExtProcOverrides{
						ProcessingMode: mapExtProcMode(ov.ExtProcMode),
					},
				},
			})
		}
	case model.FilterTypeHeaders:
		if ov.Headers != nil {
			return anypb.New(&headermutv3.HeaderMutationPerRoute{
				Mutations: buildMutations(ov.Headers),
			})
		}
	}
	return nil, nil
}
