// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package middlewares implements HTTP middleware functions for the Vrata proxy.
// This file implements the external processor middleware, which sends HTTP
// request and response phases to an external gRPC or HTTP service for
// inspection, mutation, or rejection.
package middlewares

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"

	"github.com/achetronic/vrata/internal/model"
	extprocv1 "github.com/achetronic/vrata/proto/extproc/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ExtProcMiddleware creates an external processor middleware.
func ExtProcMiddleware(cfg *model.ExtProcConfig, services map[string]Service) Middleware {
	m, _ := ExtProcMiddlewareWithStop(cfg, services)
	return m
}

// ExtProcMiddlewareWithStop creates an external processor middleware and
// returns a stop function that closes the gRPC connection (if gRPC mode).
func ExtProcMiddlewareWithStop(cfg *model.ExtProcConfig, services map[string]Service) (Middleware, func()) {
	if cfg == nil || cfg.DestinationID == "" {
		return passthrough, func() {}
	}

	svc, ok := services[cfg.DestinationID]
	if !ok {
		slog.Error("extproc: destination not found", slog.String("destinationId", cfg.DestinationID))
		return passthrough, func() {}
	}

	timeout := 200 * time.Millisecond
	if cfg.PhaseTimeout != "" {
		if d, err := time.ParseDuration(cfg.PhaseTimeout); err == nil {
			timeout = d
		}
	}

	statusOnError := 500
	if cfg.StatusOnError > 0 {
		statusOnError = int(cfg.StatusOnError)
	}

	phases := resolvePhases(cfg.Phases)

	queueSize := observeQueueSize(cfg)

	ep := &extProc{
		cfg:           cfg,
		svc:           svc,
		timeout:       timeout,
		statusOnError: statusOnError,
		phases:        phases,
		asyncQueue:    make(chan *extprocv1.ProcessingRequest, queueSize),
	}

	var cleanups []func()

	if cfg.Mode != "http" {
		target := strings.TrimPrefix(strings.TrimPrefix(svc.BaseURL, "http://"), "https://")
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			slog.Error("extproc: failed to create gRPC connection", slog.String("error", err.Error()))
		} else {
			ep.grpcConn = conn
			cleanups = append(cleanups, func() { _ = conn.Close() }) // Best-effort gRPC connection cleanup
		}
	}

	if cfg.ObserveMode != nil && cfg.ObserveMode.Enabled {
		stopWorkers := ep.startAsyncWorkers()
		cleanups = append(cleanups, stopWorkers)
	}

	mw := Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ep.handle(next, w, r)
		})
	})

	cleanup := func() {
		for _, fn := range cleanups {
			fn()
		}
	}

	return mw, cleanup
}

// ─────────────────────────────────────────────────────────────────────────────
// Core
// ─────────────────────────────────────────────────────────────────────────────

// extProc holds the resolved configuration for one external processor instance.
type extProc struct {
	cfg           *model.ExtProcConfig
	svc           Service
	timeout       time.Duration
	statusOnError int
	phases        resolvedPhases
	grpcConn      *grpc.ClientConn
	asyncQueue    chan *extprocv1.ProcessingRequest
}

const (
	defaultAsyncWorkers  = 64
	defaultAsyncQueueCap = 4096
)

// observeEnabled returns true when observe mode is active.
func (ep *extProc) observeEnabled() bool {
	return ep.cfg.ObserveMode != nil && ep.cfg.ObserveMode.Enabled
}

// observeWorkerCount returns the configured worker count or the default.
func (ep *extProc) observeWorkerCount() int {
	if ep.cfg.ObserveMode != nil && ep.cfg.ObserveMode.Workers > 0 {
		return ep.cfg.ObserveMode.Workers
	}
	return defaultAsyncWorkers
}

// observeQueueSize returns the configured queue size from a config.
func observeQueueSize(cfg *model.ExtProcConfig) int {
	if cfg.ObserveMode != nil && cfg.ObserveMode.QueueSize > 0 {
		return cfg.ObserveMode.QueueSize
	}
	return defaultAsyncQueueCap
}

// resolvedPhases holds the resolved phase configuration with defaults applied.
type resolvedPhases struct {
	requestHeaders  bool
	responseHeaders bool
	requestBody     model.BodyMode
	responseBody    model.BodyMode
}

// handle runs the external processor pipeline for a single HTTP transaction.
func (ep *extProc) handle(next http.Handler, w http.ResponseWriter, r *http.Request) {
	// Phase 1: Request headers.
	if ep.phases.requestHeaders {
		resp, err := ep.processRequestHeaders(r)
		if err != nil {
			if ep.onError(w) {
				next.ServeHTTP(w, r)
			}
			return
		}
		if resp != nil {
			if reject := resp.GetReject(); reject != nil {
				if !ep.cfg.DisableReject {
					writeReject(w, reject)
					return
				}
			}
			if ha := resp.GetRequestHeaders(); ha != nil {
				applyHeadersAction(r.Header, ha, ep.cfg.AllowedMutations)
				if ha.Status == extprocv1.ActionStatus_CONTINUE_AND_REPLACE && ha.ReplaceBody != nil {
					r.Body = io.NopCloser(bytes.NewReader(ha.ReplaceBody))
					r.ContentLength = int64(len(ha.ReplaceBody))
				}
			}
		}
	}

	// Phase 2: Request body.
	if ep.phases.requestBody != model.BodyModeNone && ep.phases.requestBody != "" {
		resp, err := ep.processRequestBody(r)
		if err != nil {
			if ep.onError(w) {
				next.ServeHTTP(w, r)
			}
			return
		}
		if resp != nil {
			if reject := resp.GetReject(); reject != nil && !ep.cfg.DisableReject {
				writeReject(w, reject)
				return
			}
			if ba := resp.GetRequestBody(); ba != nil {
				applyBodyAction(r, ba, ep.cfg.AllowedMutations)
			}
		}
	}

	// Call upstream, intercepting the response for post-processing.
	// Use httpsnoop to preserve optional interfaces (Flusher, Hijacker, etc.)
	captureHeaders := ep.phases.responseHeaders
	captureBody := ep.phases.responseBody != model.BodyModeNone && ep.phases.responseBody != ""
	needCapture := captureHeaders || captureBody

	var capturedStatus int
	var capturedBody []byte
	headersSent := false

	hooked := httpsnoop.Wrap(w, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(code int) {
				capturedStatus = code
				if !needCapture {
					headersSent = true
					next(code)
				}
			}
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(b []byte) (int, error) {
				if captureBody {
					capturedBody = append(capturedBody, b...)
					return len(b), nil
				}
				if !headersSent {
					w.WriteHeader(capturedStatus)
					headersSent = true
				}
				return next(b)
			}
		},
	})

	next.ServeHTTP(hooked, r)

	if capturedStatus == 0 {
		capturedStatus = http.StatusOK
	}

	// Phase 3: Response headers.
	if captureHeaders {
		resp, err := ep.processResponseHeaders(capturedStatus, w.Header(), len(capturedBody) == 0)
		if err != nil {
			if !ep.cfg.AllowOnError {
				slog.Error("extproc: response headers processing failed",
					slog.String("error", err.Error()),
				)
			}
		} else if resp != nil {
			if ha := resp.GetResponseHeaders(); ha != nil {
				applyHeadersAction(w.Header(), ha, ep.cfg.AllowedMutations)
			}
		}
	}

	// Phase 4: Response body.
	if captureBody && len(capturedBody) > 0 {
		resp, err := ep.processResponseBody(capturedBody)
		if err != nil {
			if !ep.cfg.AllowOnError {
				slog.Error("extproc: response body processing failed",
					slog.String("error", err.Error()),
				)
			}
		} else if resp != nil {
			if ba := resp.GetResponseBody(); ba != nil {
				capturedBody = applyResponseBodyAction(capturedBody, ba)
			}
		}
	}

	// Flush the (possibly mutated) response to the client.
	if !headersSent {
		w.WriteHeader(capturedStatus)
		if len(capturedBody) > 0 {
			if _, err := w.Write(capturedBody); err != nil {
				slog.Warn("extproc: failed to flush response body", slog.String("error", err.Error()))
			}
		}
	}
}

// onError handles processor failures. Returns true if the request should
// continue (allow-on-error), false if the error response was written.
func (ep *extProc) onError(w http.ResponseWriter) bool {
	if ep.cfg.AllowOnError {
		return true
	}
	http.Error(w, http.StatusText(ep.statusOnError), ep.statusOnError)
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase processors
// ─────────────────────────────────────────────────────────────────────────────

// processRequestHeaders sends request headers to the processor.
func (ep *extProc) processRequestHeaders(r *http.Request) (*extprocv1.ProcessingResponse, error) {
	pairs := headersToProto(r.Header, ep.cfg.ForwardRules)
	pairs = append(pairs,
		&extprocv1.HeaderPair{Key: ":method", Value: r.Method},
		&extprocv1.HeaderPair{Key: ":path", Value: r.URL.RequestURI()},
		&extprocv1.HeaderPair{Key: ":authority", Value: r.Host},
	)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	pairs = append(pairs, &extprocv1.HeaderPair{Key: ":scheme", Value: scheme})

	req := &extprocv1.ProcessingRequest{
		Phase: &extprocv1.ProcessingRequest_RequestHeaders{
			RequestHeaders: &extprocv1.HttpHeaders{
				Headers:     pairs,
				EndOfStream: r.ContentLength == 0,
			},
		},
		ObserveOnly: ep.observeEnabled(),
	}

	return ep.send(req)
}

// processRequestBody reads and sends the request body to the processor.
func (ep *extProc) processRequestBody(r *http.Request) (*extprocv1.ProcessingResponse, error) {
	if r.Body == nil {
		return nil, nil
	}

	body, err := ep.readBody(r.Body, ep.phases.requestBody)
	if err != nil {
		return nil, fmt.Errorf("reading request body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	if ep.phases.requestBody == model.BodyModeStreamed {
		return ep.sendBodyChunks(body, func(chunk []byte, eos bool) *extprocv1.ProcessingRequest {
			return &extprocv1.ProcessingRequest{
				Phase: &extprocv1.ProcessingRequest_RequestBody{
					RequestBody: &extprocv1.HttpBody{Body: chunk, EndOfStream: eos},
				},
				ObserveOnly: ep.observeEnabled(),
			}
		})
	}

	req := &extprocv1.ProcessingRequest{
		Phase: &extprocv1.ProcessingRequest_RequestBody{
			RequestBody: &extprocv1.HttpBody{Body: body, EndOfStream: true},
		},
		ObserveOnly: ep.observeEnabled(),
	}
	return ep.send(req)
}

// processResponseHeaders sends response headers to the processor.
// processResponseHeaders sends the response headers to the external processor.
func (ep *extProc) processResponseHeaders(statusCode int, headers http.Header, endOfStream bool) (*extprocv1.ProcessingResponse, error) {
	pairs := headersToProto(headers, ep.cfg.ForwardRules)
	pairs = append(pairs, &extprocv1.HeaderPair{
		Key:   ":status",
		Value: fmt.Sprintf("%d", statusCode),
	})

	req := &extprocv1.ProcessingRequest{
		Phase: &extprocv1.ProcessingRequest_ResponseHeaders{
			ResponseHeaders: &extprocv1.HttpHeaders{
				Headers:     pairs,
				EndOfStream: endOfStream,
			},
		},
		ObserveOnly: ep.observeEnabled(),
	}

	return ep.send(req)
}

// processResponseBody sends the response body to the processor.
func (ep *extProc) processResponseBody(body []byte) (*extprocv1.ProcessingResponse, error) {
	if ep.phases.responseBody == model.BodyModeBufferedPartial {
		limit := ep.maxBodyBytes()
		if int64(len(body)) > limit {
			body = body[:limit]
		}
	}

	if ep.phases.responseBody == model.BodyModeStreamed {
		return ep.sendBodyChunks(body, func(chunk []byte, eos bool) *extprocv1.ProcessingRequest {
			return &extprocv1.ProcessingRequest{
				Phase: &extprocv1.ProcessingRequest_ResponseBody{
					ResponseBody: &extprocv1.HttpBody{Body: chunk, EndOfStream: eos},
				},
				ObserveOnly: ep.observeEnabled(),
			}
		})
	}

	req := &extprocv1.ProcessingRequest{
		Phase: &extprocv1.ProcessingRequest_ResponseBody{
			ResponseBody: &extprocv1.HttpBody{Body: body, EndOfStream: true},
		},
		ObserveOnly: ep.observeEnabled(),
	}
	return ep.send(req)
}

// ─────────────────────────────────────────────────────────────────────────────
// Transport (gRPC / HTTP)
// ─────────────────────────────────────────────────────────────────────────────

// send dispatches a ProcessingRequest to the processor via gRPC or HTTP.
func (ep *extProc) send(req *extprocv1.ProcessingRequest) (*extprocv1.ProcessingResponse, error) {
	if ep.observeEnabled() {
		select {
		case ep.asyncQueue <- req:
		default:
			slog.Warn("extproc: observe-only queue full, dropping request")
		}
		return nil, nil
	}

	if ep.cfg.Mode == "http" {
		return ep.sendHTTP(req)
	}
	return ep.sendGRPC(req)
}

// startAsyncWorkers launches a pool of workers that drain the async queue.
// Returns a stop function that shuts down all workers.
func (ep *extProc) startAsyncWorkers() func() {
	stop := make(chan struct{})
	var wg sync.WaitGroup

	numWorkers := ep.observeWorkerCount()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case req, ok := <-ep.asyncQueue:
					if !ok {
						return
					}
					ep.sendAsync(req)
				case <-stop:
					return
				}
			}
		}()
	}

	return func() {
		close(stop)
		wg.Wait()
	}
}

// sendAsync sends a request without waiting for a response (observe-only).
func (ep *extProc) sendAsync(req *extprocv1.ProcessingRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), ep.timeout)
	defer cancel()

	if ep.cfg.Mode == "http" {
		ep.doHTTP(ctx, req)
	} else {
		ep.doGRPC(ctx, req)
	}
}

// sendGRPC sends a single request/response exchange over gRPC.
func (ep *extProc) sendGRPC(req *extprocv1.ProcessingRequest) (*extprocv1.ProcessingResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ep.timeout)
	defer cancel()

	return ep.doGRPC(ctx, req)
}

// doGRPC performs the gRPC call using the pooled connection.
func (ep *extProc) doGRPC(ctx context.Context, req *extprocv1.ProcessingRequest) (*extprocv1.ProcessingResponse, error) {
	if ep.grpcConn == nil {
		return nil, fmt.Errorf("no gRPC connection available")
	}

	client := extprocv1.NewProcessorClient(ep.grpcConn)
	stream, err := client.Process(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening processor stream: %w", err)
	}

	if err := stream.Send(req); err != nil {
		return nil, fmt.Errorf("sending to processor: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receiving from processor: %w", err)
	}

	return resp, nil
}

// sendHTTP sends a single request/response exchange over HTTP POST.
func (ep *extProc) sendHTTP(req *extprocv1.ProcessingRequest) (*extprocv1.ProcessingResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ep.timeout)
	defer cancel()

	return ep.doHTTP(ctx, req)
}

// doHTTP performs the HTTP POST call with a JSON-encoded ProcessingRequest.
func (ep *extProc) doHTTP(ctx context.Context, req *extprocv1.ProcessingRequest) (*extprocv1.ProcessingResponse, error) {
	jsonReq := protoRequestToJSON(req)
	body, err := json.Marshal(jsonReq)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	url := strings.TrimRight(ep.svc.BaseURL, "/")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: ep.svc.Transport}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling processor: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading processor response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("processor returned %d: %s", httpResp.StatusCode, string(respBody))
	}

	return jsonToProtoResponse(respBody)
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP JSON encoding (for HTTP mode parity)
// ─────────────────────────────────────────────────────────────────────────────

// httpRequest is the JSON representation of a ProcessingRequest for HTTP mode.
type httpRequest struct {
	Phase       string       `json:"phase"`
	Headers     []headerPair `json:"headers,omitempty"`
	Body        []byte       `json:"body,omitempty"`
	EndOfStream bool         `json:"endOfStream"`
	ObserveOnly bool         `json:"observeOnly,omitempty"`
}

// httpResponse is the JSON representation of a ProcessingResponse for HTTP mode.
type httpResponse struct {
	Action        string       `json:"action"`
	Status        string       `json:"status,omitempty"`
	SetHeaders    []headerPair `json:"setHeaders,omitempty"`
	RemoveHeaders []string     `json:"removeHeaders,omitempty"`
	ReplaceBody   []byte       `json:"replaceBody,omitempty"`
	ClearBody     bool         `json:"clearBody,omitempty"`
	RejectStatus  uint32       `json:"rejectStatus,omitempty"`
	RejectHeaders []headerPair `json:"rejectHeaders,omitempty"`
	RejectBody    []byte       `json:"rejectBody,omitempty"`
}

type headerPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// protoRequestToJSON converts a ProcessingRequest to its JSON representation.
func protoRequestToJSON(req *extprocv1.ProcessingRequest) httpRequest {
	jr := httpRequest{ObserveOnly: req.ObserveOnly}

	switch p := req.Phase.(type) {
	case *extprocv1.ProcessingRequest_RequestHeaders:
		jr.Phase = "requestHeaders"
		jr.Headers = protoPairsToJSON(p.RequestHeaders.Headers)
		jr.EndOfStream = p.RequestHeaders.EndOfStream
	case *extprocv1.ProcessingRequest_ResponseHeaders:
		jr.Phase = "responseHeaders"
		jr.Headers = protoPairsToJSON(p.ResponseHeaders.Headers)
		jr.EndOfStream = p.ResponseHeaders.EndOfStream
	case *extprocv1.ProcessingRequest_RequestBody:
		jr.Phase = "requestBody"
		jr.Body = p.RequestBody.Body
		jr.EndOfStream = p.RequestBody.EndOfStream
	case *extprocv1.ProcessingRequest_ResponseBody:
		jr.Phase = "responseBody"
		jr.Body = p.ResponseBody.Body
		jr.EndOfStream = p.ResponseBody.EndOfStream
	}

	return jr
}

// jsonToProtoResponse converts a JSON response body to a ProcessingResponse.
func jsonToProtoResponse(data []byte) (*extprocv1.ProcessingResponse, error) {
	var jr httpResponse
	if err := json.Unmarshal(data, &jr); err != nil {
		return nil, fmt.Errorf("decoding processor response: %w", err)
	}

	resp := &extprocv1.ProcessingResponse{}
	status := extprocv1.ActionStatus_CONTINUE
	if jr.Status == "continueAndReplace" {
		status = extprocv1.ActionStatus_CONTINUE_AND_REPLACE
	}

	switch jr.Action {
	case "requestHeaders", "responseHeaders":
		ha := &extprocv1.HeadersAction{
			Status:        status,
			SetHeaders:    jsonPairsToProto(jr.SetHeaders),
			RemoveHeaders: jr.RemoveHeaders,
			ReplaceBody:   jr.ReplaceBody,
		}
		if jr.Action == "requestHeaders" {
			resp.Action = &extprocv1.ProcessingResponse_RequestHeaders{RequestHeaders: ha}
		} else {
			resp.Action = &extprocv1.ProcessingResponse_ResponseHeaders{ResponseHeaders: ha}
		}
	case "requestBody", "responseBody":
		ba := &extprocv1.BodyAction{
			Status:        status,
			SetHeaders:    jsonPairsToProto(jr.SetHeaders),
			RemoveHeaders: jr.RemoveHeaders,
		}
		if jr.ClearBody {
			ba.BodyMutation = &extprocv1.BodyAction_ClearBody{ClearBody: true}
		} else if jr.ReplaceBody != nil {
			ba.BodyMutation = &extprocv1.BodyAction_ReplaceBody{ReplaceBody: jr.ReplaceBody}
		}
		if jr.Action == "requestBody" {
			resp.Action = &extprocv1.ProcessingResponse_RequestBody{RequestBody: ba}
		} else {
			resp.Action = &extprocv1.ProcessingResponse_ResponseBody{ResponseBody: ba}
		}
	case "reject":
		resp.Action = &extprocv1.ProcessingResponse_Reject{
			Reject: &extprocv1.RejectRequest{
				Status:  jr.RejectStatus,
				Headers: jsonPairsToProto(jr.RejectHeaders),
				Body:    jr.RejectBody,
			},
		}
	default:
		return nil, fmt.Errorf("unknown action: %q", jr.Action)
	}

	return resp, nil
}

func protoPairsToJSON(pairs []*extprocv1.HeaderPair) []headerPair {
	out := make([]headerPair, len(pairs))
	for i, p := range pairs {
		out[i] = headerPair{Key: p.Key, Value: p.Value}
	}
	return out
}

func jsonPairsToProto(pairs []headerPair) []*extprocv1.HeaderPair {
	out := make([]*extprocv1.HeaderPair, len(pairs))
	for i, p := range pairs {
		out[i] = &extprocv1.HeaderPair{Key: p.Key, Value: p.Value}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Header/body mutation helpers
// ─────────────────────────────────────────────────────────────────────────────

// headersToProto converts http.Header to proto HeaderPair, applying forward rules.
func headersToProto(h http.Header, rules *model.ForwardRules) []*extprocv1.HeaderPair {
	var pairs []*extprocv1.HeaderPair
	for key, values := range h {
		lower := strings.ToLower(key)
		if !shouldForward(lower, rules) {
			continue
		}
		for _, v := range values {
			pairs = append(pairs, &extprocv1.HeaderPair{Key: lower, Value: v})
		}
	}
	return pairs
}

// shouldForward checks whether a header should be sent to the processor.
func shouldForward(name string, rules *model.ForwardRules) bool {
	if rules == nil {
		return true
	}
	for _, deny := range rules.DenyHeaders {
		if strings.EqualFold(deny, name) {
			return false
		}
	}
	if len(rules.AllowHeaders) == 0 {
		return true
	}
	for _, allow := range rules.AllowHeaders {
		if strings.EqualFold(allow, name) {
			return true
		}
	}
	return false
}

// canMutate checks whether a header mutation is allowed by the rules.
func canMutate(name string, rules *model.MutationRules) bool {
	if rules == nil {
		return true
	}
	lower := strings.ToLower(name)
	for _, deny := range rules.DenyHeaders {
		if strings.EqualFold(deny, lower) {
			return false
		}
	}
	if len(rules.AllowHeaders) == 0 {
		return true
	}
	for _, allow := range rules.AllowHeaders {
		if strings.EqualFold(allow, lower) {
			return true
		}
	}
	return false
}

// applyHeadersAction applies header mutations from a HeadersAction to an http.Header.
func applyHeadersAction(h http.Header, ha *extprocv1.HeadersAction, rules *model.MutationRules) {
	for _, rm := range ha.RemoveHeaders {
		if canMutate(rm, rules) {
			h.Del(rm)
		}
	}
	for _, pair := range ha.SetHeaders {
		if canMutate(pair.Key, rules) {
			h.Set(pair.Key, pair.Value)
		}
	}
}

// applyBodyAction applies body mutations from a BodyAction to the request.
func applyBodyAction(r *http.Request, ba *extprocv1.BodyAction, rules *model.MutationRules) {
	for _, rm := range ba.RemoveHeaders {
		if canMutate(rm, rules) {
			r.Header.Del(rm)
		}
	}
	for _, pair := range ba.SetHeaders {
		if canMutate(pair.Key, rules) {
			r.Header.Set(pair.Key, pair.Value)
		}
	}
	switch m := ba.BodyMutation.(type) {
	case *extprocv1.BodyAction_ReplaceBody:
		r.Body = io.NopCloser(bytes.NewReader(m.ReplaceBody))
		r.ContentLength = int64(len(m.ReplaceBody))
	case *extprocv1.BodyAction_ClearBody:
		if m.ClearBody {
			r.Body = io.NopCloser(bytes.NewReader(nil))
			r.ContentLength = 0
		}
	}
}

// applyResponseBodyAction applies body mutations from a BodyAction to the response body.
func applyResponseBodyAction(original []byte, ba *extprocv1.BodyAction) []byte {
	switch m := ba.BodyMutation.(type) {
	case *extprocv1.BodyAction_ReplaceBody:
		return m.ReplaceBody
	case *extprocv1.BodyAction_ClearBody:
		if m.ClearBody {
			return nil
		}
	}
	return original
}

// writeReject writes a RejectRequest to the client.
func writeReject(w http.ResponseWriter, reject *extprocv1.RejectRequest) {
	for _, pair := range reject.Headers {
		w.Header().Set(pair.Key, pair.Value)
	}
	status := int(reject.Status)
	if status == 0 {
		status = http.StatusForbidden
	}
	w.WriteHeader(status)
	if reject.Body != nil {
		if _, err := w.Write(reject.Body); err != nil {
			slog.Warn("extproc: failed to write reject body", slog.String("error", err.Error()))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Config helpers
// ─────────────────────────────────────────────────────────────────────────────

// readBody reads the body respecting the body mode limit.
func (ep *extProc) readBody(body io.Reader, mode model.BodyMode) ([]byte, error) {
	if mode == model.BodyModeBufferedPartial {
		limit := ep.maxBodyBytes()
		return io.ReadAll(io.LimitReader(body, limit))
	}
	return io.ReadAll(body)
}

// maxBodyBytes returns the configured max body size for bufferedPartial.
func (ep *extProc) maxBodyBytes() int64 {
	if ep.cfg.Phases != nil && ep.cfg.Phases.MaxBodyBytes > 0 {
		return ep.cfg.Phases.MaxBodyBytes
	}
	return 1048576
}

const streamChunkSize = 32 * 1024

// sendBodyChunks sends body in chunks for streamed mode. Returns the last response.
func (ep *extProc) sendBodyChunks(body []byte, buildReq func(chunk []byte, eos bool) *extprocv1.ProcessingRequest) (*extprocv1.ProcessingResponse, error) {
	var lastResp *extprocv1.ProcessingResponse

	for len(body) > 0 {
		chunk := body
		if len(chunk) > streamChunkSize {
			chunk = body[:streamChunkSize]
		}
		body = body[len(chunk):]
		eos := len(body) == 0

		resp, err := ep.send(buildReq(chunk, eos))
		if err != nil {
			return nil, err
		}
		lastResp = resp
	}

	if lastResp == nil {
		return ep.send(buildReq(nil, true))
	}
	return lastResp, nil
}

// resolvePhases applies defaults to the phase configuration.
func resolvePhases(p *model.ExtProcPhases) resolvedPhases {
	rp := resolvedPhases{
		requestHeaders:  true,
		responseHeaders: true,
		requestBody:     model.BodyModeNone,
		responseBody:    model.BodyModeNone,
	}
	if p == nil {
		return rp
	}
	if p.RequestHeaders == model.PhaseModeSkip {
		rp.requestHeaders = false
	}
	if p.ResponseHeaders == model.PhaseModeSkip {
		rp.responseHeaders = false
	}
	if p.RequestBody != "" {
		rp.requestBody = p.RequestBody
	}
	if p.ResponseBody != "" {
		rp.responseBody = p.ResponseBody
	}
	return rp
}
