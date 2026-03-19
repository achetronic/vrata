package middlewares

import "testing"

func TestHeaderMatchesAny(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"exact match", []string{"authorization"}, true},
		{"exact case insensitive", []string{"Authorization"}, true},
		{"wildcard match", []string{"x-auth-request-*"}, true},
		{"wildcard no match", []string{"x-other-*"}, false},
		{"no patterns", []string{}, false},
		{"multiple patterns", []string{"cookie", "x-auth-*"}, false},
	}

	for _, tt := range tests {
		header := "authorization"
		if tt.name == "wildcard match" {
			header = "x-auth-request-user"
		}
		got := headerMatchesAny(header, tt.patterns)
		if got != tt.want {
			t.Errorf("headerMatchesAny(%q, %v) = %v, want %v", header, tt.patterns, got, tt.want)
		}
	}
}

func TestHeaderMatchWildcard(t *testing.T) {
	patterns := []string{"x-auth-request-*"}

	tests := []struct {
		header string
		want   bool
	}{
		{"x-auth-request-user", true},
		{"x-auth-request-email", true},
		{"x-auth-request-", true},
		{"x-auth-other", false},
		{"authorization", false},
	}

	for _, tt := range tests {
		got := headerMatchesAny(tt.header, patterns)
		if got != tt.want {
			t.Errorf("headerMatchesAny(%q) = %v, want %v", tt.header, got, tt.want)
		}
	}
}
