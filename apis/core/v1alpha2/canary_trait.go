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
	"github.com/xishengcai/oam/pkg/oam"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ oam.Trait = &CanaryTrait{}

// CanaryTraitSpec 灰度发布策略
type CanaryTraitSpec struct {
	// Type used by the server listening on this port.
	// +kubebuilder:validation:Enum=traffic;header
	// +optional
	Type string `json:"type"` // 灰度发布策略类型
	// +optional
	Header map[string]string `json:"header,omitempty"`

	// +optional
	Proportion int32 `json:"proportion,omitempty"` //灰度发布流量比例, range: 0-100

	// WorkloadReference to the workload this trait applies to.
	// +optional
	WorkloadReference runtimev1alpha1.TypedReference `json:"workloadRef"`
}

// A CanaryTraitStatus represents the observed state of a
// CanaryTrait.
type CanaryTraitStatus struct {
	// +optional
	runtimev1alpha1.ConditionedStatus `json:",inline"`
	// Resources managed by this canary trait
	Resources []runtimev1alpha1.TypedReference `json:"resources,omitempty"`
}

// +kubebuilder:object:root=true

// A CanaryTrait determines how many replicas a workload should have.
// +kubebuilder:resource:categories={crossplane,oam}
// +kubebuilder:subresource:status
type CanaryTrait struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CanaryTraitSpec `json:"spec,omitempty"`

	// +optional
	Status CanaryTraitStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CanaryTraitList contains a list of CanaryTrait.
type CanaryTraitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CanaryTrait `json:"items"`
}
