// Package v1 defines the SuperHTTPRoute custom resource for the vrata.io API group.
// SuperHTTPRoute is functionally identical to Gateway API's HTTPRoute but without
// the maxItems and CEL validation constraints that limit the number of hostnames,
// rules, and matches per resource.
//
// The Spec reuses gateway-api types directly (DRY). Only the top-level type,
// TypeMeta, and List wrapper are defined here.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=shrt
// +groupName=vrata.io
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// SchemeGroupVersion is the group version used to register SuperHTTPRoute.
var SchemeGroupVersion = schema.GroupVersion{Group: "vrata.io", Version: "v1"}

// Resource returns the GroupResource for SuperHTTPRoute.
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

var (
	// SchemeBuilder registers the SuperHTTPRoute types with a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// Install adds SuperHTTPRoute types to a scheme.
	Install = SchemeBuilder.AddToScheme
)

// addKnownTypes registers SuperHTTPRoute and SuperHTTPRouteList.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&SuperHTTPRoute{},
		&SuperHTTPRouteList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

// SuperHTTPRoute is a Gateway API HTTPRoute without maxItems or CEL validation
// constraints. It allows unlimited hostnames, rules, matches, and backendRefs
// per resource, solving the etcd object size limitation that forces operators
// to shard routes across multiple HTTPRoute objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
type SuperHTTPRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the HTTPRoute spec from Gateway API, reused as-is.
	Spec gwapiv1.HTTPRouteSpec `json:"spec,omitempty"`

	// Status is the HTTPRoute status from Gateway API, reused as-is.
	Status gwapiv1.HTTPRouteStatus `json:"status,omitempty"`
}

// SuperHTTPRouteList contains a list of SuperHTTPRoute resources.
//
// +kubebuilder:object:root=true
type SuperHTTPRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SuperHTTPRoute `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (in *SuperHTTPRoute) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(SuperHTTPRoute)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into another SuperHTTPRoute.
func (in *SuperHTTPRoute) DeepCopyInto(out *SuperHTTPRoute) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyObject implements runtime.Object.
func (in *SuperHTTPRouteList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(SuperHTTPRouteList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into another SuperHTTPRouteList.
func (in *SuperHTTPRouteList) DeepCopyInto(out *SuperHTTPRouteList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]SuperHTTPRoute, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
