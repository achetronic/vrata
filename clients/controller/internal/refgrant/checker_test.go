// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package refgrant

import (
	"testing"

	gwapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestMatchesGrant_Allowed(t *testing.T) {
	grant := gwapiv1beta1.ReferenceGrant{
		Spec: gwapiv1beta1.ReferenceGrantSpec{
			From: []gwapiv1beta1.ReferenceGrantFrom{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: gwapiv1.Namespace("frontend")},
			},
			To: []gwapiv1beta1.ReferenceGrantTo{
				{Group: "", Kind: "Service"},
			},
		},
	}
	if !matchesGrant(grant, "frontend") {
		t.Error("should allow frontend → Service")
	}
}

func TestMatchesGrant_WrongSourceNamespace(t *testing.T) {
	grant := gwapiv1beta1.ReferenceGrant{
		Spec: gwapiv1beta1.ReferenceGrantSpec{
			From: []gwapiv1beta1.ReferenceGrantFrom{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "frontend"},
			},
			To: []gwapiv1beta1.ReferenceGrantTo{
				{Group: "", Kind: "Service"},
			},
		},
	}
	if matchesGrant(grant, "attacker") {
		t.Error("should not allow attacker namespace")
	}
}

func TestMatchesGrant_GRPCRouteKind(t *testing.T) {
	grant := gwapiv1beta1.ReferenceGrant{
		Spec: gwapiv1beta1.ReferenceGrantSpec{
			From: []gwapiv1beta1.ReferenceGrantFrom{
				{Group: "gateway.networking.k8s.io", Kind: "GRPCRoute", Namespace: "frontend"},
			},
			To: []gwapiv1beta1.ReferenceGrantTo{
				{Group: "", Kind: "Service"},
			},
		},
	}
	if !matchesGrant(grant, "frontend") {
		t.Error("should match GRPCRoute kind")
	}
}

func TestMatchesGrant_WrongKind(t *testing.T) {
	grant := gwapiv1beta1.ReferenceGrant{
		Spec: gwapiv1beta1.ReferenceGrantSpec{
			From: []gwapiv1beta1.ReferenceGrantFrom{
				{Group: "gateway.networking.k8s.io", Kind: "TCPRoute", Namespace: "frontend"},
			},
			To: []gwapiv1beta1.ReferenceGrantTo{
				{Group: "", Kind: "Service"},
			},
		},
	}
	if matchesGrant(grant, "frontend") {
		t.Error("should not match TCPRoute kind")
	}
}

func TestMatchesGrant_WrongToKind(t *testing.T) {
	grant := gwapiv1beta1.ReferenceGrant{
		Spec: gwapiv1beta1.ReferenceGrantSpec{
			From: []gwapiv1beta1.ReferenceGrantFrom{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "frontend"},
			},
			To: []gwapiv1beta1.ReferenceGrantTo{
				{Group: "", Kind: "Secret"},
			},
		},
	}
	if matchesGrant(grant, "frontend") {
		t.Error("should not match Secret target kind")
	}
}

func TestMatchesGrant_MultipleFrom(t *testing.T) {
	grant := gwapiv1beta1.ReferenceGrant{
		Spec: gwapiv1beta1.ReferenceGrantSpec{
			From: []gwapiv1beta1.ReferenceGrantFrom{
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "ns-a"},
				{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "ns-b"},
			},
			To: []gwapiv1beta1.ReferenceGrantTo{
				{Group: "", Kind: "Service"},
			},
		},
	}
	if !matchesGrant(grant, "ns-b") {
		t.Error("should allow ns-b from multiple From entries")
	}
}
