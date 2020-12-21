/*


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
	"github.com/crossplane/oam-kubernetes-runtime2/pkg/oam"
	"k8s.io/api/autoscaling/v2beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ oam.Trait = &HorizontalPodAutoscalerTrait{}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories={crossplane,oam}
// +kubebuilder:subresource:status
// HorizontalPodAutoscalerTrait is the Schema for the horizontalpodautoscalertraits API
type HorizontalPodAutoscalerTrait struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HorizontalPodAutoscalerTraitSpec   `json:"spec,omitempty"`
	Status HorizontalPodAutoscalerTraitStatus `json:"status,omitempty"`
}

// HorizontalPodAutoscalerTraitSpec defines the desired state of HorizontalPodAutoscalerTrait
type HorizontalPodAutoscalerTraitSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// minReplicas is the lower limit for the number of replicas to which the autoscaler
	// can scale down.  It defaults to 1 pod.  minReplicas is allowed to be 0 if the
	// alpha feature gate HPAScaleToZero is enabled and at least one Object or External
	// metric is configured.  Scaling is active as long as at least one metric value is
	// available.
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty"`
	// upper limit for the number of pods that can be set by the autoscaler; cannot be smaller than MinReplicas.
	MaxReplicas int32 `json:"maxReplicas"`
	// metrics contains the specifications for which to use to calculate the
	// desired replica count (the maximum replica count across all metrics will
	// be used).  The desired replica count is calculated multiplying the
	// ratio between the target value and the current value by the current
	// number of pods.  Ergo, metrics used must decrease as the pod count is
	// increased, and vice-versa.  See the individual metric source types for
	// more information about how each type of metric must respond.
	// +optional
	Metrics []v2beta1.MetricSpec `json:"metrics,omitempty" protobuf:"bytes,4,rep,name=metrics"`

	// WorkloadReference to the workload this trait applies to.
	WorkloadReference runtimev1alpha1.TypedReference `json:"workloadRef,omitempty"`
}

// HorizontalPodAutoscalerTraitStatus defines the observed state of HorizontalPodAutoscalerTrait
type HorizontalPodAutoscalerTraitStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	runtimev1alpha1.ConditionedStatus `json:",inline"`

	// Resources managed by this service trait
	Resources []runtimev1alpha1.TypedReference `json:"resources,omitempty"`
}

// +kubebuilder:object:root=true

// HorizontalPodAutoscalerTraitList contains a list of HorizontalPodAutoscalerTrait
type HorizontalPodAutoscalerTraitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HorizontalPodAutoscalerTrait `json:"items"`
}
