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
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
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

func envoyFilterName(ft model.MiddlewareType) string {
	switch ft {
	case model.MiddlewareTypeCORS:
		return filterNameCORS
	case model.MiddlewareTypeJWT:
		return filterNameJWT
	case model.MiddlewareTypeExtAuthz:
		return filterNameExtAuthz
	case model.MiddlewareTypeExtProc:
		return filterNameExtProc
	case model.MiddlewareTypeRateLimit:
		return filterNameRateLimit
	case model.MiddlewareTypeHeaders:
		return filterNameHeaders
	default:
		return ""
	}
}

// buildHTTPFilterReal builds a fully configured Envoy HttpFilter from a Rutoso
// Filter entity. This replaces the stubs that were in builder.go.
func buildHTTPFilterReal(f model.Middleware) (*hcmv3.HttpFilter, error) {
	name := envoyFilterName(f.Type)
	if name == "" {
		return nil, nil
	}
	var msg *anypb.Any
	var err error

	switch f.Type {
	case model.MiddlewareTypeCORS:
		msg, err = marshalCORS(f.CORS)
	case model.MiddlewareTypeJWT:
		msg, err = marshalJWT(f.JWT)
	case model.MiddlewareTypeExtAuthz:
		msg, err = marshalExtAuthz(f.ExtAuthz)
	case model.MiddlewareTypeExtProc:
		msg, err = marshalExtProc(f.ExtProc)
	case model.MiddlewareTypeRateLimit:
		msg, err = marshalRateLimit(f.RateLimit)
	case model.MiddlewareTypeHeaders:
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
	// CORS filter in HCM is always the empty Cors{} wrapper.
	// The actual CORS policy goes in per_filter_config on each route.
	return anypb.New(&corsv3.Cors{})
}

// marshalCORSPolicy builds the per-route CorsPolicy from the model config.
func marshalCORSPolicy(c *model.CORSConfig) (*anypb.Any, error) {
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
	// filter_enabled must be set to 100% or Envoy treats the CORS policy as
	// disabled. Without this field the preflight is forwarded to the upstream.
	p.FilterEnabled = &corev3.RuntimeFractionalPercent{
		DefaultValue: &typev3.FractionalPercent{
			Numerator:   100,
			Denominator: typev3.FractionalPercent_HUNDRED,
		},
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
		if p.JWKsURI != "" && p.JWKsDestinationID != "" {
			jp.JwksSourceSpecifier = &jwtauthnv3.JwtProvider_RemoteJwks{
				RemoteJwks: &jwtauthnv3.RemoteJwks{
					HttpUri: &corev3.HttpUri{
						Uri:              p.JWKsURI,
						HttpUpstreamType: &corev3.HttpUri_Cluster{Cluster: p.JWKsDestinationID},
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
	if c != nil && c.DestinationID != "" {
		ea.FailureModeAllow = c.FailureModeAllow
		timeout := durationpb.New(5 * time.Second)
		if c.Timeout != "" {
			if dur, err := time.ParseDuration(c.Timeout); err == nil {
				timeout = durationpb.New(dur)
			}
		}
		if c.Mode == "grpc" {
			ea.Services = &extauthzv3.ExtAuthz_GrpcService{
				GrpcService: &corev3.GrpcService{
					TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
						EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{ClusterName: c.DestinationID},
					},
					Timeout: timeout,
				},
			}
		} else {
			uri := c.DestinationID
			if c.PathPrefix != "" {
				uri = c.PathPrefix
			}
			ea.Services = &extauthzv3.ExtAuthz_HttpService{
				HttpService: &extauthzv3.HttpService{
					ServerUri: &corev3.HttpUri{
						Uri:              uri,
						HttpUpstreamType: &corev3.HttpUri_Cluster{Cluster: c.DestinationID},
						Timeout:          timeout,
					},
				},
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
	if c != nil && c.DestinationID != "" {
		gs := &corev3.GrpcService{
			TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{ClusterName: c.DestinationID},
			},
		}
		if c.Timeout != "" {
			if dur, err := time.ParseDuration(c.Timeout); err == nil {
				gs.Timeout = durationpb.New(dur)
			}
		}
		ep.GrpcService = gs
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
	if c != nil && c.DestinationID != "" {
		rl.Domain = c.Domain
		rl.FailureModeDeny = c.FailureModeDeny
		rl.RateLimitService = &rlconfv3.RateLimitServiceConfig{
			GrpcService: &corev3.GrpcService{
				TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{ClusterName: c.DestinationID},
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

// ─── per_filter_config (MiddlewareOverrides) ─────────────────────────────────────

// buildPerFilterConfig builds the Envoy typed_per_filter_config for a single
// route. Middlewares in the HCM pipeline that are NOT active for this route
// are disabled. Active middlewares get their override config (if any).
//
// "Active" means the middleware ID appears in the group's or route's
// MiddlewareIDs. Group overrides are the base; route overrides win.
func buildPerFilterConfig(
	groupMwIDs []string,
	routeMwIDs []string,
	groupOverrides map[string]model.MiddlewareOverride,
	routeOverrides map[string]model.MiddlewareOverride,
	usedMwIDs map[string]bool,
	mwByID map[string]model.Middleware,
) map[string]*anypb.Any {
	if len(usedMwIDs) == 0 {
		return nil
	}

	// Build set of active middleware IDs for this route.
	active := make(map[string]bool)
	for _, id := range groupMwIDs {
		active[id] = true
	}
	for _, id := range routeMwIDs {
		active[id] = true
	}

	// Merge overrides: group base, route wins.
	mergedOv := make(map[string]model.MiddlewareOverride)
	for id, ov := range groupOverrides {
		mergedOv[id] = ov
	}
	for id, ov := range routeOverrides {
		mergedOv[id] = ov
	}

	result := make(map[string]*anypb.Any)
	for mwID := range usedMwIDs {
		mw, ok := mwByID[mwID]
		if !ok {
			continue
		}
		name := envoyFilterName(mw.Type)
		if name == "" {
			continue
		}

		if !active[mwID] {
			// Middleware is in pipeline but not active for this route — disable it.
			if a, err := marshalDisabledOverride(mw.Type); err == nil && a != nil {
				result[name] = a
			}
			continue
		}

		// Middleware is active. Apply override if present, or inject config for
		// filter types that require per-route config (e.g. CORS).
		if ov, hasOv := mergedOv[mwID]; hasOv {
			if ov.Disabled {
				if a, err := marshalDisabledOverride(mw.Type); err == nil && a != nil {
					result[name] = a
				}
			} else {
				if a, err := marshalMiddlewareOverride(mw.Type, ov); err == nil && a != nil {
					result[name] = a
				}
			}
		} else if mw.Type == model.MiddlewareTypeCORS {
			// CORS is special: HCM gets empty Cors{}, the actual policy MUST go
			// in per_filter_config on every route where it's active.
			if a, err := marshalCORSPolicy(mw.CORS); err == nil && a != nil {
				result[name] = a
			}
		}
		// If no override, the middleware runs with its global config (no per_filter_config needed).
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func marshalDisabledOverride(ft model.MiddlewareType) (*anypb.Any, error) {
	switch ft {
	case model.MiddlewareTypeCORS:
		return anypb.New(&corsv3.CorsPolicy{})
	case model.MiddlewareTypeJWT:
		return anypb.New(&jwtauthnv3.PerRouteConfig{
			RequirementSpecifier: &jwtauthnv3.PerRouteConfig_Disabled{Disabled: true},
		})
	case model.MiddlewareTypeExtAuthz:
		return anypb.New(&extauthzv3.ExtAuthzPerRoute{
			Override: &extauthzv3.ExtAuthzPerRoute_Disabled{Disabled: true},
		})
	case model.MiddlewareTypeExtProc:
		return anypb.New(&extprocv3.ExtProcPerRoute{
			Override: &extprocv3.ExtProcPerRoute_Disabled{Disabled: true},
		})
	case model.MiddlewareTypeRateLimit:
		return anypb.New(&ratelimitv3.RateLimitPerRoute{})
	case model.MiddlewareTypeHeaders:
		return anypb.New(&headermutv3.HeaderMutationPerRoute{})
	default:
		return nil, nil
	}
}

func marshalMiddlewareOverride(ft model.MiddlewareType, ov model.MiddlewareOverride) (*anypb.Any, error) {
	switch ft {
	case model.MiddlewareTypeJWT:
		if ov.JWTProvider != "" {
			return anypb.New(&jwtauthnv3.PerRouteConfig{
				RequirementSpecifier: &jwtauthnv3.PerRouteConfig_RequirementName{
					RequirementName: ov.JWTProvider,
				},
			})
		}
	case model.MiddlewareTypeExtAuthz:
		if len(ov.ExtAuthzContextExtensions) > 0 {
			return anypb.New(&extauthzv3.ExtAuthzPerRoute{
				Override: &extauthzv3.ExtAuthzPerRoute_CheckSettings{
					CheckSettings: &extauthzv3.CheckSettings{
						ContextExtensions: ov.ExtAuthzContextExtensions,
					},
				},
			})
		}
	case model.MiddlewareTypeExtProc:
		if ov.ExtProcMode != nil {
			return anypb.New(&extprocv3.ExtProcPerRoute{
				Override: &extprocv3.ExtProcPerRoute_Overrides{
					Overrides: &extprocv3.ExtProcOverrides{
						ProcessingMode: mapExtProcMode(ov.ExtProcMode),
					},
				},
			})
		}
	case model.MiddlewareTypeHeaders:
		if ov.Headers != nil {
			return anypb.New(&headermutv3.HeaderMutationPerRoute{
				Mutations: buildMutations(ov.Headers),
			})
		}
	}
	return nil, nil
}
