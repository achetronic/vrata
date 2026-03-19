// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package refgrant checks whether cross-namespace backend references are
// permitted by ReferenceGrant resources, as required by the Gateway API spec.
package refgrant

import (
	"context"
	"fmt"
	"log/slog"

	gwapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Checker verifies cross-namespace backendRef permissions via ReferenceGrants.
type Checker struct {
	client runtimeclient.Client
	logger *slog.Logger
}

// NewChecker creates a ReferenceGrant Checker.
func NewChecker(client runtimeclient.Client, logger *slog.Logger) *Checker {
	return &Checker{client: client, logger: logger}
}

// AllowedBackendRef returns true if a backendRef from sourceNamespace to
// targetNamespace/targetName is permitted. Same-namespace refs are always allowed.
func (c *Checker) AllowedBackendRef(ctx context.Context, sourceNamespace, targetNamespace, targetName string) (bool, error) {
	if sourceNamespace == targetNamespace {
		return true, nil
	}

	var grants gwapiv1beta1.ReferenceGrantList
	if err := c.client.List(ctx, &grants, runtimeclient.InNamespace(targetNamespace)); err != nil {
		return false, fmt.Errorf("listing ReferenceGrants in %q: %w", targetNamespace, err)
	}

	for _, grant := range grants.Items {
		if matchesGrant(grant, sourceNamespace) {
			c.logger.Debug("cross-namespace ref allowed by ReferenceGrant",
				slog.String("grant", grant.Name),
				slog.String("from", sourceNamespace),
				slog.String("to", fmt.Sprintf("%s/%s", targetNamespace, targetName)),
			)
			return true, nil
		}
	}

	c.logger.Warn("cross-namespace backendRef denied: no matching ReferenceGrant",
		slog.String("from", sourceNamespace),
		slog.String("to", fmt.Sprintf("%s/%s", targetNamespace, targetName)),
	)
	return false, nil
}

// matchesGrant checks if a ReferenceGrant allows references from the given
// source namespace to Services in the grant's namespace.
func matchesGrant(grant gwapiv1beta1.ReferenceGrant, sourceNamespace string) bool {
	fromMatch := false
	for _, from := range grant.Spec.From {
		if from.Group == "gateway.networking.k8s.io" &&
			from.Kind == "HTTPRoute" &&
			string(from.Namespace) == sourceNamespace {
			fromMatch = true
			break
		}
	}
	if !fromMatch {
		return false
	}

	for _, to := range grant.Spec.To {
		if to.Group == "" && to.Kind == "Service" {
			return true
		}
	}
	return false
}
