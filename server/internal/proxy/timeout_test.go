package proxy

import (
	"testing"
	"time"

	"github.com/achetronic/vrata/internal/model"
)

func TestParseDurationOrDefault_Nil(t *testing.T) {
	got := parseDurationOrDefault[model.ListenerTimeouts](nil, func(t *model.ListenerTimeouts) string { return t.ClientHeader }, 10*time.Second)
	if got != 10*time.Second {
		t.Errorf("expected 10s fallback, got %v", got)
	}
}

func TestParseDurationOrDefault_Empty(t *testing.T) {
	cfg := &model.ListenerTimeouts{}
	got := parseDurationOrDefault(cfg, func(t *model.ListenerTimeouts) string { return t.ClientHeader }, 10*time.Second)
	if got != 10*time.Second {
		t.Errorf("expected 10s fallback for empty, got %v", got)
	}
}

func TestParseDurationOrDefault_Invalid(t *testing.T) {
	cfg := &model.ListenerTimeouts{ClientHeader: "notaduration"}
	got := parseDurationOrDefault(cfg, func(t *model.ListenerTimeouts) string { return t.ClientHeader }, 10*time.Second)
	if got != 10*time.Second {
		t.Errorf("expected 10s fallback for invalid, got %v", got)
	}
}

func TestParseDurationOrDefault_Valid(t *testing.T) {
	cfg := &model.ListenerTimeouts{ClientHeader: "15s"}
	got := parseDurationOrDefault(cfg, func(t *model.ListenerTimeouts) string { return t.ClientHeader }, 10*time.Second)
	if got != 15*time.Second {
		t.Errorf("expected 15s, got %v", got)
	}
}

func TestNewEndpoint_DefaultTimeouts(t *testing.T) {
	d := model.Destination{ID: "d1", Host: "127.0.0.1", Port: 8080}
	ep, err := NewEndpoint(model.Endpoint{Host: "127.0.0.1", Port: 8080}, d)
	if err != nil {
		t.Fatal(err)
	}
	tr := ep.Transport
	if tr.TLSHandshakeTimeout != 5*time.Second {
		t.Errorf("TLSHandshakeTimeout: got %v, want 5s", tr.TLSHandshakeTimeout)
	}
	if tr.ResponseHeaderTimeout != 10*time.Second {
		t.Errorf("ResponseHeaderTimeout: got %v, want 10s", tr.ResponseHeaderTimeout)
	}
	if tr.ExpectContinueTimeout != 1*time.Second {
		t.Errorf("ExpectContinueTimeout: got %v, want 1s", tr.ExpectContinueTimeout)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout: got %v, want 90s", tr.IdleConnTimeout)
	}
}

func TestNewEndpoint_CustomTimeouts(t *testing.T) {
	d := model.Destination{
		ID: "d1", Host: "127.0.0.1", Port: 8080,
		Options: &model.DestinationOptions{
			Timeouts: &model.DestinationTimeouts{
				Connect:           "2s",
				TLSHandshake:      "3s",
				ResponseHeader:    "7s",
				IdleConnection:    "45s",
				ExpectContinue:    "500ms",
				DualStackFallback: "100ms",
			},
		},
	}
	ep, err := NewEndpoint(model.Endpoint{Host: "127.0.0.1", Port: 8080}, d)
	if err != nil {
		t.Fatal(err)
	}
	tr := ep.Transport
	if tr.TLSHandshakeTimeout != 3*time.Second {
		t.Errorf("TLSHandshakeTimeout: got %v, want 3s", tr.TLSHandshakeTimeout)
	}
	if tr.ResponseHeaderTimeout != 7*time.Second {
		t.Errorf("ResponseHeaderTimeout: got %v, want 7s", tr.ResponseHeaderTimeout)
	}
	if tr.ExpectContinueTimeout != 500*time.Millisecond {
		t.Errorf("ExpectContinueTimeout: got %v, want 500ms", tr.ExpectContinueTimeout)
	}
	if tr.IdleConnTimeout != 45*time.Second {
		t.Errorf("IdleConnTimeout: got %v, want 45s", tr.IdleConnTimeout)
	}
}
