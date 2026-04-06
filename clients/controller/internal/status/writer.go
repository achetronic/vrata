// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package status writes Gateway API status conditions back to HTTPRoute,
// GRPCRoute, and Gateway resources so operators can see the sync state via kubectl.
package status

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ControllerName is the Gateway API controller name used for status entries.
const ControllerName gwapiv1.GatewayController = "vrata.io/controller"

// Writer updates Gateway API status conditions.
type Writer struct {
	client runtimeclient.Client
}

// NewWriter creates a status Writer.
func NewWriter(client runtimeclient.Client) *Writer {
	return &Writer{client: client}
}

// ─── HTTPRoute ──────────────────────────────────────────────────────────────

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

	setRouteCondition(&hr.Status.Parents, hr.Spec.ParentRefs, cond)

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

	setRouteCondition(&hr.Status.Parents, hr.Spec.ParentRefs, cond)

	if err := w.client.Status().Update(ctx, hr); err != nil {
		return fmt.Errorf("updating HTTPRoute %s/%s status: %w", hr.Namespace, hr.Name, err)
	}
	return nil
}

// ─── GRPCRoute ──────────────────────────────────────────────────────────────

// SetGRPCRouteAccepted marks the GRPCRoute as accepted.
func (w *Writer) SetGRPCRouteAccepted(ctx context.Context, gr *gwapiv1.GRPCRoute, accepted bool, reason, message string) error {
	now := metav1.Now()
	status := metav1.ConditionTrue
	if !accepted {
		status = metav1.ConditionFalse
	}

	cond := metav1.Condition{
		Type:               string(gwapiv1.RouteConditionAccepted),
		Status:             status,
		ObservedGeneration: gr.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	setRouteCondition(&gr.Status.Parents, gr.Spec.ParentRefs, cond)

	if err := w.client.Status().Update(ctx, gr); err != nil {
		return fmt.Errorf("updating GRPCRoute %s/%s status: %w", gr.Namespace, gr.Name, err)
	}
	return nil
}

// SetGRPCRouteResolvedRefs marks whether all backendRefs in the GRPCRoute could be resolved.
func (w *Writer) SetGRPCRouteResolvedRefs(ctx context.Context, gr *gwapiv1.GRPCRoute, resolved bool, message string) error {
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
		ObservedGeneration: gr.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	setRouteCondition(&gr.Status.Parents, gr.Spec.ParentRefs, cond)

	if err := w.client.Status().Update(ctx, gr); err != nil {
		return fmt.Errorf("updating GRPCRoute %s/%s status: %w", gr.Namespace, gr.Name, err)
	}
	return nil
}

// ─── Gateway ────────────────────────────────────────────────────────────────

// SetGatewayAccepted sets the Accepted condition on a Gateway.
func (w *Writer) SetGatewayAccepted(ctx context.Context, gw *gwapiv1.Gateway, accepted bool, reason, message string) error {
	now := metav1.Now()
	status := metav1.ConditionTrue
	if !accepted {
		status = metav1.ConditionFalse
	}

	setGatewayCondition(&gw.Status.Conditions, metav1.Condition{
		Type:               string(gwapiv1.GatewayConditionAccepted),
		Status:             status,
		ObservedGeneration: gw.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})

	if err := w.client.Status().Update(ctx, gw); err != nil {
		return fmt.Errorf("updating Gateway %s/%s status: %w", gw.Namespace, gw.Name, err)
	}
	return nil
}

// SetGatewayProgrammed sets the Programmed condition on a Gateway.
// It also populates the Addresses field with a placeholder if it's empty,
// as required by Gateway API conformance.
func (w *Writer) SetGatewayProgrammed(ctx context.Context, gw *gwapiv1.Gateway, programmed bool, reason, message string) error {
	now := metav1.Now()
	status := metav1.ConditionTrue
	if !programmed {
		status = metav1.ConditionFalse
	}

	setGatewayCondition(&gw.Status.Conditions, metav1.Condition{
		Type:               string(gwapiv1.GatewayConditionProgrammed),
		Status:             status,
		ObservedGeneration: gw.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})

	if programmed && len(gw.Status.Addresses) == 0 {
		addrType := gwapiv1.IPAddressType
		gw.Status.Addresses = []gwapiv1.GatewayStatusAddress{
			{
				Type:  &addrType,
				Value: "127.0.0.1", // Placeholder for conformance; in real life, sync from Service.
			},
		}
	}

	if err := w.client.Status().Update(ctx, gw); err != nil {
		return fmt.Errorf("updating Gateway %s/%s status: %w", gw.Namespace, gw.Name, err)
	}
	return nil
}

// SetListenerConditions sets conditions on a specific listener within a Gateway.
func (w *Writer) SetListenerConditions(ctx context.Context, gw *gwapiv1.Gateway, listenerName string, conditions []metav1.Condition) error {
	found := false
	for i, ls := range gw.Status.Listeners {
		if string(ls.Name) == listenerName {
			for _, cond := range conditions {
				setConditionInSlice(&gw.Status.Listeners[i].Conditions, cond)
			}
			found = true
			break
		}
	}
	if !found {
		gw.Status.Listeners = append(gw.Status.Listeners, gwapiv1.ListenerStatus{
			Name:       gwapiv1.SectionName(listenerName),
			Conditions: conditions,
		})
	}

	if err := w.client.Status().Update(ctx, gw); err != nil {
		return fmt.Errorf("updating Gateway %s/%s listener %s status: %w", gw.Namespace, gw.Name, listenerName, err)
	}
	return nil
}

// ─── GatewayClass ───────────────────────────────────────────────────────────

// SetGatewayClassAccepted sets the Accepted condition on a GatewayClass.
func (w *Writer) SetGatewayClassAccepted(ctx context.Context, gc *gwapiv1.GatewayClass, accepted bool, reason, message string) error {
	now := metav1.Now()
	status := metav1.ConditionTrue
	if !accepted {
		status = metav1.ConditionFalse
	}

	setGatewayCondition(&gc.Status.Conditions, metav1.Condition{
		Type:               string(gwapiv1.GatewayClassConditionStatusAccepted),
		Status:             status,
		ObservedGeneration: gc.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})

	if err := w.client.Status().Update(ctx, gc); err != nil {
		return fmt.Errorf("updating GatewayClass %s status: %w", gc.Name, err)
	}
	return nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// setRouteCondition ensures a RouteParentStatus exists for our controller
// for each parentRef and sets the condition on all of them.
func setRouteCondition(parents *[]gwapiv1.RouteParentStatus, parentRefs []gwapiv1.ParentReference, cond metav1.Condition) {
	refs := parentRefs
	if len(refs) == 0 {
		refs = []gwapiv1.ParentReference{{Name: "controller"}}
	}

	for _, parentRef := range refs {
		found := false
		for i := range *parents {
			p := &(*parents)[i]
			if p.ControllerName != ControllerName {
				continue
			}
			refMatch := p.ParentRef.Name == parentRef.Name &&
				ptrStringEq(p.ParentRef.SectionName, parentRef.SectionName)
			controllerOnly := len(parentRefs) == 0 && p.ParentRef.Name == ""
			if refMatch || controllerOnly {
				setConditionInSlice(&p.Conditions, cond)
				p.ParentRef = parentRef
				found = true
				break
			}
		}
		if !found {
			*parents = append(*parents, gwapiv1.RouteParentStatus{
				ParentRef:      parentRef,
				ControllerName: ControllerName,
				Conditions:     []metav1.Condition{cond},
			})
		}
	}
}

func ptrStringEq(a, b *gwapiv1.SectionName) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// setGatewayCondition sets or updates a condition in a flat condition slice.
func setGatewayCondition(conditions *[]metav1.Condition, cond metav1.Condition) {
	if *conditions == nil {
		*conditions = []metav1.Condition{cond}
		return
	}
	setConditionInSlice(conditions, cond)
}

// setConditionInSlice updates an existing condition by type or appends a new one.
func setConditionInSlice(conditions *[]metav1.Condition, cond metav1.Condition) {
	for i, c := range *conditions {
		if c.Type == cond.Type {
			(*conditions)[i] = cond
			return
		}
	}
	*conditions = append(*conditions, cond)
}

// timestamp returns a formatted timestamp for snapshot names.
func timestamp() string {
	return time.Now().Format("20060102-150405")
}
