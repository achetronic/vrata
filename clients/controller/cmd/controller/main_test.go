// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestParseGroupName(t *testing.T) {
	tests := []struct {
		input     string
		wantNS    string
		wantName  string
		wantOK    bool
	}{
		{"k8s:default/my-route", "default", "my-route", true},
		{"k8s:prod/api-gateway", "prod", "api-gateway", true},
		{"k8s:ns/name/with/slashes", "ns", "name/with/slashes", true},
		{"k8s:noslash", "", "", false},
		{"manual:default/route", "", "", false},
		{"", "", "", false},
	}

	for _, tt := range tests {
		ns, name, ok := parseGroupName(tt.input)
		if ok != tt.wantOK {
			t.Errorf("parseGroupName(%q): ok = %v, want %v", tt.input, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if ns != tt.wantNS {
			t.Errorf("parseGroupName(%q): ns = %q, want %q", tt.input, ns, tt.wantNS)
		}
		if name != tt.wantName {
			t.Errorf("parseGroupName(%q): name = %q, want %q", tt.input, name, tt.wantName)
		}
	}
}
