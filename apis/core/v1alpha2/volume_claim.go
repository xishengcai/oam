package v1alpha2

import (
	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VolumeClaimSpec struct {
	// type enum:"HostPath,StorageClass", default is StorageClass.
	HostPath         string `json:"hostPath,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	Size             string `json:"size,omitempty"`
}

type VolumeClaimStatus struct {
	// The generation observed by the component controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration"`

	runtimev1alpha1.ConditionedStatus `json:",inline"`

	// LatestRevision of component
	// +optional
	LatestRevision *Revision `json:"latestRevision,omitempty"`
}

// +kubebuilder:object:root=true

// A VolumeClaim describes how an OAM VolumeClaim kind may be instantiated.
// +kubebuilder:resource:categories={crossplane,oam}
// +kubebuilder:subresource:status
type VolumeClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VolumeClaimSpec   `json:"spec,omitempty"`
	Status VolumeClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type VolumeClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VolumeClaim `json:"items"`
}

type VolumeClaimConfig struct {
	Name string `json:"name"`
	// type enum:"HostPath,StorageClass", default is StorageClass.
	HostPath         string `json:"hostPath,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	Size             string `json:"size,omitempty"`
}
