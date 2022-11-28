package v1alpha2

import (
	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/xishengcai/oam/pkg/oam"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ oam.Object = &VolumeClaim{}

type VolumeClaimSpec struct {
	// type enum:"HostPath,StorageClass", default is StorageClass
	Type             string `json:"type,omitempty"`
	HostPath         string `json:"hostPath,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	// type enum:"ReadWriteOnce,ReadOnlyMany,ReadWriteMany"
	// ReadWriteOnce – the volume can be mounted as read-write by a single node
	// ReadOnlyMany – the volume can be mounted read-only by many nodes
	// ReadWriteMany – the volume can be mounted as read-write by many nodes
	AccessMode       v1.PersistentVolumeAccessMode `json:"accessMode,omitempty"`
	Size             string `json:"size,omitempty"`
}

type VolumeClaimStatus struct {
	// The generation observed by the component controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration"`

	// +optional
	runtimev1alpha1.ConditionedStatus `json:",inline"`
	// Resources managed by this canary trait
	Resources []runtimev1alpha1.TypedReference `json:"resources,omitempty"`

	// LatestRevision of applicationConfig
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

	// +kubebuilder:validation:Enum=HostPath;StorageClass
	Type             string `json:"type,omitempty"`
	HostPath         string `json:"hostPath,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	Size             string `json:"size,omitempty"`
	AccessMode       v1.PersistentVolumeAccessMode `json:"accessMode,omitempty"`

}
