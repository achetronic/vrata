package middlewares

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/achetronic/vrata/internal/model"
	extauthzv1 "github.com/achetronic/vrata/proto/extauthz/v1"
	"google.golang.org/grpc"
)

func TestExtAuthzAllow(t *testing.T) {
	authz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Auth-User", "user-1")
		w.WriteHeader(200)
	}))
	defer authz.Close()

	cfg := &model.ExtAuthzConfig{
		DestinationID: "authz-1",
		Path:          "/check",
		OnAllow:       &model.ExtAuthzOnAllow{CopyToUpstream: []string{"x-auth-user"}},
	}

	services := map[string]Service{"authz-1": {BaseURL: authz.URL}}
	mw := ExtAuthzMiddleware(cfg, services)

	var capturedUser string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = r.Header.Get("X-Auth-User")
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedUser != "user-1" {
		t.Errorf("expected X-Auth-User=user-1, got %q", capturedUser)
	}
}

func TestExtAuthzDeny(t *testing.T) {
	authz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://login.example.com")
		w.WriteHeader(302)
		w.Write([]byte("please login"))
	}))
	defer authz.Close()

	cfg := &model.ExtAuthzConfig{
		DestinationID: "authz-1",
		Path:          "/check",
	}

	services := map[string]Service{"authz-1": {BaseURL: authz.URL}}
	mw := ExtAuthzMiddleware(cfg, services)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called on deny")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 302 {
		t.Errorf("expected 302, got %d", w.Code)
	}
	if w.Header().Get("Location") != "https://login.example.com" {
		t.Errorf("expected Location header copied to client")
	}
}

func TestExtAuthzFailureModeAllow(t *testing.T) {
	cfg := &model.ExtAuthzConfig{
		DestinationID:    "authz-1",
		DecisionTimeout:  "50ms",
		FailureModeAllow: true,
	}

	services := map[string]Service{"authz-1": {BaseURL: "http://127.0.0.1:1"}}
	mw := ExtAuthzMiddleware(cfg, services)

	var reached bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Error("expected request to pass through on failure (allow mode)")
	}
}

func TestExtAuthzFailureModeClose(t *testing.T) {
	cfg := &model.ExtAuthzConfig{
		DestinationID:    "authz-1",
		DecisionTimeout:  "50ms",
		FailureModeAllow: false,
	}

	services := map[string]Service{"authz-1": {BaseURL: "http://127.0.0.1:1"}}
	mw := ExtAuthzMiddleware(cfg, services)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called on failure (close mode)")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("expected 403 on authz failure, got %d", w.Code)
	}
}

func TestExtAuthzForwardHeaders(t *testing.T) {
	var receivedAuth string
	authz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer authz.Close()

	cfg := &model.ExtAuthzConfig{
		DestinationID: "authz-1",
		OnCheck: &model.ExtAuthzOnCheck{
			ForwardHeaders: []string{"authorization"},
		},
	}

	services := map[string]Service{"authz-1": {BaseURL: authz.URL}}
	mw := ExtAuthzMiddleware(cfg, services)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer token123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if receivedAuth != "Bearer token123" {
		t.Errorf("expected Authorization forwarded, got %q", receivedAuth)
	}
}

func TestExtAuthzIncludeBody(t *testing.T) {
	var receivedBody string
	authz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(200)
	}))
	defer authz.Close()

	cfg := &model.ExtAuthzConfig{
		DestinationID: "authz-1",
		IncludeBody:   true,
	}

	services := map[string]Service{"authz-1": {BaseURL: authz.URL}}
	mw := ExtAuthzMiddleware(cfg, services)

	var upstreamBody string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("POST", "/test", httptest.NewRequest("POST", "/", nil).Body)
	req.Body = io.NopCloser(ioReader("request-body"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if receivedBody != "request-body" {
		t.Errorf("expected authz to receive body, got %q", receivedBody)
	}
	if upstreamBody != "request-body" {
		t.Errorf("expected upstream to still receive body, got %q", upstreamBody)
	}
}

func TestExtAuthzNilConfig(t *testing.T) {
	mw := ExtAuthzMiddleware(nil, nil)
	w := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if w.Code != 200 {
		t.Errorf("expected passthrough 200, got %d", w.Code)
	}
}

func ioReader(s string) io.ReadCloser {
	return io.NopCloser(ioStringReader(s))
}

type ioStringReader string

func (s ioStringReader) Read(p []byte) (int, error) {
	n := copy(p, s)
	return n, io.EOF
}

// ─── gRPC mode ──────────────────────────────────────────────────────────────

type fakeAuthzServer struct {
	extauthzv1.UnimplementedAuthorizerServer
	handler func(req *extauthzv1.CheckRequest) *extauthzv1.CheckResponse
}

func (f *fakeAuthzServer) Check(_ context.Context, req *extauthzv1.CheckRequest) (*extauthzv1.CheckResponse, error) {
	return f.handler(req), nil
}

func startFakeAuthzGRPC(t *testing.T, handler func(*extauthzv1.CheckRequest) *extauthzv1.CheckResponse) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	extauthzv1.RegisterAuthorizerServer(srv, &fakeAuthzServer{handler: handler})
	go srv.Serve(lis)
	t.Cleanup(srv.GracefulStop)
	return lis.Addr().String()
}

func TestExtAuthzGRPCAllow(t *testing.T) {
	addr := startFakeAuthzGRPC(t, func(req *extauthzv1.CheckRequest) *extauthzv1.CheckResponse {
		return &extauthzv1.CheckResponse{
			Allowed: true,
			Headers: []*extauthzv1.HeaderPair{{Key: "x-grpc-user", Value: "user-1"}},
		}
	})

	cfg := &model.ExtAuthzConfig{
		DestinationID: "authz-1",
		Mode:          "grpc",
	}
	services := map[string]Service{"authz-1": {BaseURL: fmt.Sprintf("http://%s", addr)}}
	mw := ExtAuthzMiddleware(cfg, services)

	var capturedUser string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = r.Header.Get("X-Grpc-User")
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedUser != "user-1" {
		t.Errorf("expected x-grpc-user=user-1, got %q", capturedUser)
	}
}

func TestExtAuthzGRPCDeny(t *testing.T) {
	addr := startFakeAuthzGRPC(t, func(req *extauthzv1.CheckRequest) *extauthzv1.CheckResponse {
		return &extauthzv1.CheckResponse{
			Allowed:      false,
			DeniedStatus: 403,
			DeniedBody:   []byte("grpc-denied"),
		}
	})

	cfg := &model.ExtAuthzConfig{
		DestinationID: "authz-1",
		Mode:          "grpc",
	}
	services := map[string]Service{"authz-1": {BaseURL: fmt.Sprintf("http://%s", addr)}}
	mw := ExtAuthzMiddleware(cfg, services)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if w.Body.String() != "grpc-denied" {
		t.Errorf("expected body 'grpc-denied', got %q", w.Body.String())
	}
}

func TestExtAuthzGRPCFailureModeAllow(t *testing.T) {
	cfg := &model.ExtAuthzConfig{
		DestinationID:    "authz-1",
		Mode:             "grpc",
		DecisionTimeout:  "50ms",
		FailureModeAllow: true,
	}
	services := map[string]Service{"authz-1": {BaseURL: "http://127.0.0.1:1"}}
	mw := ExtAuthzMiddleware(cfg, services)

	var reached bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Error("expected passthrough on gRPC failure with allow mode")
	}
}
