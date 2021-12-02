/*
Copyright 2020 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha2

import (
	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/xishengcai/oam/pkg/oam"
)

var _ oam.Trait = &VolumeTrait{}

// A VolumeTraitSpec defines the desired state of a VolumeTrait.
type VolumeTraitSpec struct {
	VolumeList []VolumeMountItem `json:"volumeList"`
	// WorkloadReference to the workload this trait applies to.
	WorkloadReference runtimev1alpha1.TypedReference `json:"workloadRef"`
}

// VolumeMountItem contains path of container
type VolumeMountItem struct {
	ContainerIndex int        `json:"containerIndex"`
	Paths          []PathItem `json:"paths"`
}

// PathItem define storageClass, size, path, name
type PathItem struct {
	Name                  string `json:"name"`
	PersistentVolumeClaim string `json:"persistentVolumeClaim"`
	Path                  string `json:"path"`

	// 下面的字段废弃
	Type             string `json:"type,omitempty"`
	HostPath         string `json:"hostPath,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`
	Size             string `json:"size,omitempty"`
}

// A VolumeTraitStatus represents the observed state of a
// VolumeTrait.
type VolumeTraitStatus struct {
	runtimev1alpha1.ConditionedStatus `json:",inline"`

	// Resources managed by this containerised workload.
	Resources []runtimev1alpha1.TypedReference `json:"resources,omitempty"`
}

// +kubebuilder:object:root=true

// A VolumeTrait determines how many replicas a workload should have.
// +kubebuilder:resource:categories={crossplane,oam}
// +kubebuilder:subresource:status
type VolumeTrait struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VolumeTraitSpec   `json:"spec,omitempty"`
	Status VolumeTraitStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VolumeTraitList contains a list of VolumeTrait.
type VolumeTraitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VolumeTrait `json:"items"`
}
