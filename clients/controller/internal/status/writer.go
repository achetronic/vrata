// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package status writes Gateway API status conditions back to HTTPRoute
// resources so operators can see the sync state via kubectl.
package status

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/achetronic/vrata/clients/controller/apis/agentic"
)

// Writer updates HTTPRoute status conditions.
type Writer struct {
	client runtimeclient.Client
}

// NewWriter creates a status Writer.
func NewWriter(client runtimeclient.Client) *Writer {
	return &Writer{client: client}
}

// SetAccepted marks the HTTPRoute as accepted (successfully synced to Vrata).
func (w *Writer) SetAccepted(ctx context.Context, hr *gwapiv1.HTTPRoute, accepted bool, reason, message string) error {
	now := metav1.Now()
	status := metav1.ConditionTrue
	if !accepted {
		status = metav1.ConditionFalse
	}

	cond := metav1.Condition{
		Type:               string(gwapiv1.RouteConditionAccepted),
		Status:             status,
		ObservedGeneration: hr.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	setCondition(&hr.Status.Parents, cond)

	if err := w.client.Status().Update(ctx, hr); err != nil {
		return fmt.Errorf("updating HTTPRoute %s/%s status: %w", hr.Namespace, hr.Name, err)
	}
	return nil
}

// SetResolvedRefs marks whether all backendRefs in the HTTPRoute could be resolved.
func (w *Writer) SetResolvedRefs(ctx context.Context, hr *gwapiv1.HTTPRoute, resolved bool, message string) error {
	now := metav1.Now()
	status := metav1.ConditionTrue
	reason := string(gwapiv1.RouteReasonResolvedRefs)
	if !resolved {
		status = metav1.ConditionFalse
		reason = string(gwapiv1.RouteReasonBackendNotFound)
	}

	cond := metav1.Condition{
		Type:               string(gwapiv1.RouteConditionResolvedRefs),
		Status:             status,
		ObservedGeneration: hr.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	setCondition(&hr.Status.Parents, cond)

	if err := w.client.Status().Update(ctx, hr); err != nil {
		return fmt.Errorf("updating HTTPRoute %s/%s status: %w", hr.Namespace, hr.Name, err)
	}
	return nil
}

// setCondition ensures a RouteParentStatus exists and sets the condition.
func setCondition(parents *[]gwapiv1.RouteParentStatus, cond metav1.Condition) {
	if len(*parents) == 0 {
		*parents = []gwapiv1.RouteParentStatus{{
			ParentRef: gwapiv1.ParentReference{
				Name: "controller",
			},
			ControllerName: "vrata.io/controller",
			Conditions:     []metav1.Condition{cond},
		}}
		return
	}

	// Update existing parent status.
	for i := range *parents {
		if (*parents)[i].ControllerName == "vrata.io/controller" {
			conditions := &(*parents)[i].Conditions
			for j, c := range *conditions {
				if c.Type == cond.Type {
					(*conditions)[j] = cond
					return
				}
			}
			*conditions = append(*conditions, cond)
			return
		}
	}

	// Add new parent status for our controller.
	*parents = append(*parents, gwapiv1.RouteParentStatus{
		ParentRef: gwapiv1.ParentReference{
			Name: "controller",
		},
		ControllerName: "vrata.io/controller",
		Conditions:     []metav1.Condition{cond},
	})
}

// Timestamp returns a formatted timestamp for snapshot names.
func Timestamp() string {
	return time.Now().Format("20060102-150405")
}

// SetXBackendAvailable sets the Available condition on an XBackend.
func (w *Writer) SetXBackendAvailable(ctx context.Context, b *agentic.XBackend, available bool, reason, message string) error {
	now := metav1.Now()
	status := metav1.ConditionTrue
	if !available {
		status = metav1.ConditionFalse
	}
	cond := metav1.Condition{
		Type:               "Available",
		Status:             status,
		ObservedGeneration: b.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
	setXBackendCondition(&b.Status.Conditions, cond)
	if err := w.client.Status().Update(ctx, b); err != nil {
		return fmt.Errorf("updating XBackend %s/%s status: %w", b.Namespace, b.Name, err)
	}
	return nil
}

// setXBackendCondition upserts a condition by type on a conditions slice.
func setXBackendCondition(conditions *[]metav1.Condition, cond metav1.Condition) {
	for i, c := range *conditions {
		if c.Type == cond.Type {
			(*conditions)[i] = cond
			return
		}
	}
	*conditions = append(*conditions, cond)
}

// SetXAccessPolicyAccepted sets the Accepted condition on an XAccessPolicy
// for a specific ancestor target.
func (w *Writer) SetXAccessPolicyAccepted(ctx context.Context, p *agentic.XAccessPolicy, target agentic.PolicyTargetRef, accepted bool, reason, message string) error {
	now := metav1.Now()
	status := metav1.ConditionTrue
	if !accepted {
		status = metav1.ConditionFalse
	}
	cond := metav1.Condition{
		Type:               "Accepted",
		Status:             status,
		ObservedGeneration: p.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	found := false
	for i := range p.Status.Ancestors {
		a := &p.Status.Ancestors[i]
		if a.ControllerName == "vrata.io/controller" && a.AncestorRef.Name == target.Name {
			for j, c := range a.Conditions {
				if c.Type == cond.Type {
					a.Conditions[j] = cond
					found = true
					break
				}
			}
			if !found {
				a.Conditions = append(a.Conditions, cond)
			}
			found = true
			break
		}
	}
	if !found {
		p.Status.Ancestors = append(p.Status.Ancestors, agentic.PolicyAncestorStatus{
			AncestorRef:    target,
			ControllerName: "vrata.io/controller",
			Conditions:     []metav1.Condition{cond},
		})
	}

	if err := w.client.Status().Update(ctx, p); err != nil {
		return fmt.Errorf("updating XAccessPolicy %s/%s status: %w", p.Namespace, p.Name, err)
	}
	return nil
}
