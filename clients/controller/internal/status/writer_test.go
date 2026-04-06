// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package status

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestSetRouteCondition_NewParent(t *testing.T) {
	var parents []gwapiv1.RouteParentStatus
	cond := metav1.Condition{
		Type:   string(gwapiv1.RouteConditionAccepted),
		Status: metav1.ConditionTrue,
		Reason: "Synced",
	}
	setRouteCondition(&parents, nil, cond)

	if len(parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(parents))
	}
	if parents[0].ControllerName != ControllerName {
		t.Errorf("expected controller name %s, got %q", ControllerName, parents[0].ControllerName)
	}
	if len(parents[0].Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(parents[0].Conditions))
	}
	if parents[0].Conditions[0].Reason != "Synced" {
		t.Errorf("expected reason Synced, got %q", parents[0].Conditions[0].Reason)
	}
}

func TestSetRouteCondition_UpdateExisting(t *testing.T) {
	parents := []gwapiv1.RouteParentStatus{
		{
			ControllerName: ControllerName,
			Conditions: []metav1.Condition{
				{Type: string(gwapiv1.RouteConditionAccepted), Status: metav1.ConditionFalse, Reason: "Failed"},
			},
		},
	}

	cond := metav1.Condition{
		Type:   string(gwapiv1.RouteConditionAccepted),
		Status: metav1.ConditionTrue,
		Reason: "Synced",
	}
	setRouteCondition(&parents, nil, cond)

	if len(parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(parents))
	}
	if len(parents[0].Conditions) != 1 {
		t.Fatalf("expected 1 condition (updated), got %d", len(parents[0].Conditions))
	}
	if parents[0].Conditions[0].Reason != "Synced" {
		t.Errorf("expected updated reason Synced, got %q", parents[0].Conditions[0].Reason)
	}
	if parents[0].Conditions[0].Status != metav1.ConditionTrue {
		t.Error("expected status True")
	}
}

func TestSetRouteCondition_AddSecondCondition(t *testing.T) {
	parents := []gwapiv1.RouteParentStatus{
		{
			ControllerName: ControllerName,
			Conditions: []metav1.Condition{
				{Type: string(gwapiv1.RouteConditionAccepted), Status: metav1.ConditionTrue, Reason: "Synced"},
			},
		},
	}

	cond := metav1.Condition{
		Type:   string(gwapiv1.RouteConditionResolvedRefs),
		Status: metav1.ConditionTrue,
		Reason: string(gwapiv1.RouteReasonResolvedRefs),
	}
	setRouteCondition(&parents, nil, cond)

	if len(parents[0].Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(parents[0].Conditions))
	}
}

func TestSetRouteCondition_OtherControllerUntouched(t *testing.T) {
	parents := []gwapiv1.RouteParentStatus{
		{
			ControllerName: "other-controller",
			Conditions: []metav1.Condition{
				{Type: string(gwapiv1.RouteConditionAccepted), Status: metav1.ConditionTrue, Reason: "Other"},
			},
		},
	}

	cond := metav1.Condition{
		Type:   string(gwapiv1.RouteConditionAccepted),
		Status: metav1.ConditionTrue,
		Reason: "Synced",
	}
	setRouteCondition(&parents, nil, cond)

	if len(parents) != 2 {
		t.Fatalf("expected 2 parents (other + ours), got %d", len(parents))
	}
	if parents[0].Conditions[0].Reason != "Other" {
		t.Error("other controller's condition should be untouched")
	}
	if parents[1].ControllerName != ControllerName {
		t.Errorf("expected our controller, got %q", parents[1].ControllerName)
	}
}

func TestSetRouteCondition_UsesParentRef(t *testing.T) {
	var parents []gwapiv1.RouteParentStatus
	gwName := gwapiv1.ObjectName("my-gateway")
	parentRefs := []gwapiv1.ParentReference{
		{Name: gwName},
	}
	cond := metav1.Condition{
		Type:   string(gwapiv1.RouteConditionAccepted),
		Status: metav1.ConditionTrue,
		Reason: "Synced",
	}
	setRouteCondition(&parents, parentRefs, cond)

	if len(parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(parents))
	}
	if parents[0].ParentRef.Name != gwName {
		t.Errorf("expected parentRef name %q, got %q", gwName, parents[0].ParentRef.Name)
	}
}

func TestSetGatewayCondition(t *testing.T) {
	var conditions []metav1.Condition
	cond := metav1.Condition{
		Type:   string(gwapiv1.GatewayConditionAccepted),
		Status: metav1.ConditionTrue,
		Reason: "Accepted",
	}
	setGatewayCondition(&conditions, cond)

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	if conditions[0].Reason != "Accepted" {
		t.Errorf("expected reason Accepted, got %q", conditions[0].Reason)
	}

	cond2 := metav1.Condition{
		Type:   string(gwapiv1.GatewayConditionAccepted),
		Status: metav1.ConditionFalse,
		Reason: "Invalid",
	}
	setGatewayCondition(&conditions, cond2)

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition (updated), got %d", len(conditions))
	}
	if conditions[0].Reason != "Invalid" {
		t.Errorf("expected updated reason Invalid, got %q", conditions[0].Reason)
	}
}

func TestSetConditionInSlice_Append(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: "TypeA", Status: metav1.ConditionTrue, Reason: "A"},
	}
	setConditionInSlice(&conditions, metav1.Condition{
		Type: "TypeB", Status: metav1.ConditionFalse, Reason: "B",
	})
	if len(conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(conditions))
	}
}
