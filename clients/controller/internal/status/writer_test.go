// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package status

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestSetCondition_NewParent(t *testing.T) {
	var parents []gwapiv1.RouteParentStatus
	cond := metav1.Condition{
		Type:   string(gwapiv1.RouteConditionAccepted),
		Status: metav1.ConditionTrue,
		Reason: "Synced",
	}
	setCondition(&parents, cond)

	if len(parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(parents))
	}
	if parents[0].ControllerName != "vrata.io/controller" {
		t.Errorf("expected controller name vrata.io/controller, got %q", parents[0].ControllerName)
	}
	if len(parents[0].Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(parents[0].Conditions))
	}
	if parents[0].Conditions[0].Reason != "Synced" {
		t.Errorf("expected reason Synced, got %q", parents[0].Conditions[0].Reason)
	}
}

func TestSetCondition_UpdateExisting(t *testing.T) {
	parents := []gwapiv1.RouteParentStatus{
		{
			ControllerName: "vrata.io/controller",
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
	setCondition(&parents, cond)

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

func TestSetCondition_AddSecondCondition(t *testing.T) {
	parents := []gwapiv1.RouteParentStatus{
		{
			ControllerName: "vrata.io/controller",
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
	setCondition(&parents, cond)

	if len(parents[0].Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(parents[0].Conditions))
	}
}

func TestSetCondition_OtherControllerUntouched(t *testing.T) {
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
	setCondition(&parents, cond)

	if len(parents) != 2 {
		t.Fatalf("expected 2 parents (other + ours), got %d", len(parents))
	}
	if parents[0].Conditions[0].Reason != "Other" {
		t.Error("other controller's condition should be untouched")
	}
	if parents[1].ControllerName != "vrata.io/controller" {
		t.Errorf("expected our controller, got %q", parents[1].ControllerName)
	}
}

func TestTimestamp(t *testing.T) {
	ts := Timestamp()
	if len(ts) != 15 {
		t.Errorf("expected 15-char timestamp (YYYYMMDD-HHMMSS), got %q (%d)", ts, len(ts))
	}
}
