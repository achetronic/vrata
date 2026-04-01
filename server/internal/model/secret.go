// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package model

// Secret is a flat, first-class entity that holds a single sensitive value.
// Secrets are stored in the control plane and resolved into entity fields
// at snapshot build time via the {{secret:value:<id>}} reference pattern.
// They never travel in the snapshot — only their resolved values do.
type Secret struct {
	// ID is the unique identifier of this secret.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label (e.g. "prod-tls-cert", "jwt-signing-key").
	Name string `json:"name" yaml:"name"`

	// Value is the secret content (PEM certificate, private key, token, etc.).
	// Must NEVER be logged.
	Value string `json:"value" yaml:"value"`
}

// SecretSummary is a lightweight representation returned by list endpoints.
// It omits the Value field to prevent accidental bulk exposure.
type SecretSummary struct {
	// ID is the unique identifier of this secret.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label.
	Name string `json:"name" yaml:"name"`
}
