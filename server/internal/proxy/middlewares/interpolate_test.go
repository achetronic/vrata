package middlewares

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestInterpolateBasicFields(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/foo/bar", nil)
	r.Host = "example.com:443"

	tests := []struct {
		template string
		want     string
	}{
		{"${request.method}", "GET"},
		{"${request.path}", "/foo/bar"},
		{"${request.host}", "example.com"},
		{"${request.authority}", "example.com:443"},
		{"${request.scheme}", "http"}, // no TLS on test request
		{"no placeholders", "no placeholders"},
		{"https://${request.host}${request.path}", "https://example.com/foo/bar"},
	}

	for _, tt := range tests {
		got := Interpolate(tt.template, r)
		if got != tt.want {
			t.Errorf("Interpolate(%q) = %q, want %q", tt.template, got, tt.want)
		}
	}
}

func TestInterpolateWithTLS(t *testing.T) {
	r := httptest.NewRequest("GET", "https://example.com/", nil)
	r.TLS = &tls.ConnectionState{}

	got := Interpolate("${request.scheme}", r)
	if got != "https" {
		t.Errorf("scheme with TLS = %q, want https", got)
	}
}

func TestInterpolateHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Custom", "myvalue")

	got := Interpolate("val=${request.header.X-Custom}", r)
	if got != "val=myvalue" {
		t.Errorf("header interpolation = %q, want val=myvalue", got)
	}
}

func TestInterpolateMissingHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)

	got := Interpolate("val=${request.header.Missing}", r)
	if got != "val=" {
		t.Errorf("missing header = %q, want val=", got)
	}
}
