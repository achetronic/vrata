// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package model

// InlineAuthzConfig configures inline authorization via CEL rules.
// Rules are evaluated in order — first match wins. If no rule matches,
// the defaultAction is applied.
type InlineAuthzConfig struct {
	// Rules is an ordered list of authorization rules.
	Rules []InlineAuthzRule `json:"rules" yaml:"rules"`

	// DefaultAction is applied when no rule matches.
	// Must be "allow" or "deny". Default: "deny".
	DefaultAction string `json:"defaultAction" yaml:"defaultAction"`

	// DenyStatus is the HTTP status code returned on deny.
	// Default: 403.
	DenyStatus uint32 `json:"denyStatus,omitempty" yaml:"denyStatus,omitempty"`

	// DenyBody is the response body returned on deny.
	// Default: {"error":"access denied"}.
	DenyBody string `json:"denyBody,omitempty" yaml:"denyBody,omitempty"`
}

// InlineAuthzRule is a single authorization rule with a CEL expression
// and an action.
type InlineAuthzRule struct {
	// CEL is the expression to evaluate against the request.
	// Must return bool.
	CEL string `json:"cel" yaml:"cel"`

	// Action is what to do when the expression matches.
	// Must be "allow" or "deny".
	Action string `json:"action" yaml:"action"`
}
