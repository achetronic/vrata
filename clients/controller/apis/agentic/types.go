// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package agentic defines minimal Go types for the Kube Agentic Networking CRDs
// (agentic.prototype.x-k8s.io/v0alpha0). Only the fields needed by the Vrata
// controller are included. These types are registered in the controller's scheme
// to enable informer-based watching.
//
// +groupName=agentic.prototype.x-k8s.io
package agentic

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SchemeGroupVersion is the group version for agentic networking CRDs.
var SchemeGroupVersion = schema.GroupVersion{
	Group:   "agentic.prototype.x-k8s.io",
	Version: "v0alpha0",
}

var (
	// SchemeBuilder registers the agentic types with a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// Install adds agentic types to a scheme.
	Install = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&XBackend{},
		&XBackendList{},
		&XAccessPolicy{},
		&XAccessPolicyList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

// ─── XBackend ───────────────────────────────────────────────────────────────

// XBackend describes an MCP (Model Context Protocol) backend.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type XBackend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              XBackendSpec   `json:"spec"`
	Status            XBackendStatus `json:"status,omitempty"`
}

// XBackendSpec defines the desired state of an XBackend.
type XBackendSpec struct {
	MCP MCPBackend `json:"mcp"`
}

// MCPBackend describes the MCP backend endpoint. Exactly one of
// ServiceName or Hostname must be set.
type MCPBackend struct {
	ServiceName *string `json:"serviceName,omitempty"`
	Hostname    *string `json:"hostname,omitempty"`
	Port        int32   `json:"port"`
	Path        string  `json:"path,omitempty"`
}

// XBackendStatus defines the observed state of an XBackend.
type XBackendStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// XBackendList contains a list of XBackend resources.
//
// +kubebuilder:object:root=true
type XBackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []XBackend `json:"items"`
}

// ─── XAccessPolicy ──────────────────────────────────────────────────────────

// XAccessPolicy defines authorization rules for agentic backends.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type XAccessPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              XAccessPolicySpec   `json:"spec"`
	Status            XAccessPolicyStatus `json:"status,omitempty"`
}

// XAccessPolicySpec defines the desired state of an XAccessPolicy.
type XAccessPolicySpec struct {
	TargetRefs []PolicyTargetRef `json:"targetRefs"`
	Rules      []AccessRule      `json:"rules"`
}

// PolicyTargetRef identifies the target of the policy.
type PolicyTargetRef struct {
	Group string `json:"group"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
}

// AccessRule specifies an authorization rule.
type AccessRule struct {
	Name          string             `json:"name"`
	Source        Source             `json:"source"`
	Authorization *AuthorizationRule `json:"authorization,omitempty"`
}

// Source identifies who is making the request.
type Source struct {
	Type           string                  `json:"type"` // "ServiceAccount" or "SPIFFE"
	SPIFFE         *string                 `json:"spiffe,omitempty"`
	ServiceAccount *SourceServiceAccount   `json:"serviceAccount,omitempty"`
}

// SourceServiceAccount identifies a Kubernetes ServiceAccount.
type SourceServiceAccount struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// AuthorizationRule specifies what the source can do.
type AuthorizationRule struct {
	Type         string                  `json:"type"` // "InlineTools" or "ExternalAuth"
	Tools        []string                `json:"tools,omitempty"`
	ExternalAuth *ExternalAuthConfig     `json:"externalAuth,omitempty"`
}

// ExternalAuthConfig configures delegation to an external auth service.
type ExternalAuthConfig struct {
	BackendRef  BackendRef `json:"backendRef"`
	Protocol    string     `json:"protocol"` // "HTTP" or "GRPC"
}

// BackendRef references a backend service.
type BackendRef struct {
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Port      *int32 `json:"port,omitempty"`
}

// XAccessPolicyStatus defines the observed state.
type XAccessPolicyStatus struct {
	Ancestors []PolicyAncestorStatus `json:"ancestors,omitempty"`
}

// XAccessPolicyList contains a list of XAccessPolicy resources.
//
// +kubebuilder:object:root=true
type XAccessPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []XAccessPolicy `json:"items"`
}

// PolicyAncestorStatus describes the status with respect to an ancestor.
type PolicyAncestorStatus struct {
	AncestorRef    PolicyTargetRef    `json:"ancestorRef"`
	ControllerName string             `json:"controllerName"`
	Conditions     []metav1.Condition `json:"conditions"`
}

// ─── DeepCopy implementations (required by runtime.Object) ──────────────────

func (in *XBackend) DeepCopyObject() runtime.Object {
	out := new(XBackend)
	in.DeepCopyInto(out)
	return out
}

func (in *XBackend) DeepCopyInto(out *XBackend) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	if in.Spec.MCP.ServiceName != nil {
		s := *in.Spec.MCP.ServiceName
		out.Spec.MCP.ServiceName = &s
	}
	if in.Spec.MCP.Hostname != nil {
		s := *in.Spec.MCP.Hostname
		out.Spec.MCP.Hostname = &s
	}
	if in.Status.Conditions != nil {
		out.Status.Conditions = make([]metav1.Condition, len(in.Status.Conditions))
		copy(out.Status.Conditions, in.Status.Conditions)
	}
}

func (in *XBackendList) DeepCopyObject() runtime.Object {
	out := new(XBackendList)
	in.DeepCopyInto(out)
	return out
}

func (in *XBackendList) DeepCopyInto(out *XBackendList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]XBackend, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *XAccessPolicy) DeepCopyObject() runtime.Object {
	out := new(XAccessPolicy)
	in.DeepCopyInto(out)
	return out
}

func (in *XAccessPolicy) DeepCopyInto(out *XAccessPolicy) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec.TargetRefs = make([]PolicyTargetRef, len(in.Spec.TargetRefs))
	copy(out.Spec.TargetRefs, in.Spec.TargetRefs)
	out.Spec.Rules = make([]AccessRule, len(in.Spec.Rules))
	for i, r := range in.Spec.Rules {
		out.Spec.Rules[i] = r
		if r.Source.SPIFFE != nil {
			s := *r.Source.SPIFFE
			out.Spec.Rules[i].Source.SPIFFE = &s
		}
		if r.Source.ServiceAccount != nil {
			sa := *r.Source.ServiceAccount
			out.Spec.Rules[i].Source.ServiceAccount = &sa
		}
		if r.Authorization != nil {
			auth := *r.Authorization
			if auth.Tools != nil {
				auth.Tools = make([]string, len(r.Authorization.Tools))
				copy(auth.Tools, r.Authorization.Tools)
			}
			if auth.ExternalAuth != nil {
				ea := *r.Authorization.ExternalAuth
				auth.ExternalAuth = &ea
			}
			out.Spec.Rules[i].Authorization = &auth
		}
	}
	if in.Status.Ancestors != nil {
		out.Status.Ancestors = make([]PolicyAncestorStatus, len(in.Status.Ancestors))
		for i, a := range in.Status.Ancestors {
			out.Status.Ancestors[i] = a
			if a.Conditions != nil {
				out.Status.Ancestors[i].Conditions = make([]metav1.Condition, len(a.Conditions))
				copy(out.Status.Ancestors[i].Conditions, a.Conditions)
			}
		}
	}
}

func (in *XAccessPolicyList) DeepCopyObject() runtime.Object {
	out := new(XAccessPolicyList)
	in.DeepCopyInto(out)
	return out
}

func (in *XAccessPolicyList) DeepCopyInto(out *XAccessPolicyList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]XAccessPolicy, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
