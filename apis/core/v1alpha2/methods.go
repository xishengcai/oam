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

// This code is manually implemented, but should be generated in the future.

package v1alpha2

import (
	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
)

// SetConditions of this VolumeClaim.
func (tr *VolumeClaim) SetConditions(c ...runtimev1alpha1.Condition) {
	tr.Status.SetConditions(c...)
}

// GetCondition of this VolumeClaim.
func (tr *VolumeClaim) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return tr.Status.GetCondition(ct)
}

// GetCondition of this ManualScalerTrait.
func (tr *ManualScalerTrait) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return tr.Status.GetCondition(ct)
}

// SetConditions of this ManualScalerTrait.
func (tr *ManualScalerTrait) SetConditions(c ...runtimev1alpha1.Condition) {
	tr.Status.SetConditions(c...)
}

// GetWorkloadReference of this ManualScalerTrait.
func (tr *ManualScalerTrait) GetWorkloadReference() runtimev1alpha1.TypedReference {
	return tr.Spec.WorkloadReference
}

// SetWorkloadReference of this ManualScalerTrait.
func (tr *ManualScalerTrait) SetWorkloadReference(r runtimev1alpha1.TypedReference) {
	tr.Spec.WorkloadReference = r
}

// GetCondition of this ApplicationConfiguration.
func (ac *ApplicationConfiguration) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return ac.Status.GetCondition(ct)
}

// SetConditions of this ApplicationConfiguration.
func (ac *ApplicationConfiguration) SetConditions(c ...runtimev1alpha1.Condition) {
	ac.Status.SetConditions(c...)
}

// GetCondition of this Component.
func (cm *Component) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return cm.Status.GetCondition(ct)
}

// SetConditions of this Component.
func (cm *Component) SetConditions(c ...runtimev1alpha1.Condition) {
	cm.Status.SetConditions(c...)
}

// GetCondition of this ContainerizedWorkload.
func (wl *ContainerizedWorkload) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return wl.Status.GetCondition(ct)
}

// SetConditions of this ContainerizedWorkload.
func (wl *ContainerizedWorkload) SetConditions(c ...runtimev1alpha1.Condition) {
	wl.Status.SetConditions(c...)
}

// GetCondition of this HealthScope.
func (hs *HealthScope) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return hs.Status.GetCondition(ct)
}

// SetConditions of this HealthScope.
func (hs *HealthScope) SetConditions(c ...runtimev1alpha1.Condition) {
	hs.Status.SetConditions(c...)
}

// GetWorkloadReferences to get all workload references for scope.
func (hs *HealthScope) GetWorkloadReferences() []runtimev1alpha1.TypedReference {
	return hs.Spec.WorkloadReferences
}

// AddWorkloadReference to add a workload reference to this scope.
func (hs *HealthScope) AddWorkloadReference(r runtimev1alpha1.TypedReference) {
	hs.Spec.WorkloadReferences = append(hs.Spec.WorkloadReferences, r)
}

// GetCondition of this HorizontalPodAutoscalerTrait.
func (h *HorizontalPodAutoscalerTrait) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return h.Status.GetCondition(ct)
}

// SetConditions of this HorizontalPodAutoscalerTrait.
func (h *HorizontalPodAutoscalerTrait) SetConditions(c ...runtimev1alpha1.Condition) {
	h.Status.SetConditions(c...)
}

// GetWorkloadReference of this HorizontalPodAutoscalerTrait.
func (h *HorizontalPodAutoscalerTrait) GetWorkloadReference() runtimev1alpha1.TypedReference {
	return h.Spec.WorkloadReference
}

// SetWorkloadReference of this HorizontalPodAutoscalerTrait.
func (h *HorizontalPodAutoscalerTrait) SetWorkloadReference(r runtimev1alpha1.TypedReference) {
	h.Spec.WorkloadReference = r
}

// GetCondition of this VolumeTrait.
func (vt *VolumeTrait) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return vt.Status.GetCondition(ct)
}

// SetConditions of this VolumeTrait.
func (vt *VolumeTrait) SetConditions(c ...runtimev1alpha1.Condition) {
	vt.Status.SetConditions(c...)
}

// GetWorkloadReference of this VolumeTrait.
func (vt *VolumeTrait) GetWorkloadReference() runtimev1alpha1.TypedReference {
	return vt.Spec.WorkloadReference
}

// SetWorkloadReference of this VolumeTrait.
func (vt *VolumeTrait) SetWorkloadReference(r runtimev1alpha1.TypedReference) {
	vt.Spec.WorkloadReference = r
}

// GetCondition of this CanaryTrait.
func (ca *CanaryTrait) GetCondition(ct runtimev1alpha1.ConditionType) runtimev1alpha1.Condition {
	return ca.Status.GetCondition(ct)
}

// SetConditions of this CanaryTrait.
func (ca *CanaryTrait) SetConditions(c ...runtimev1alpha1.Condition) {
	ca.Status.SetConditions(c...)
}

// GetWorkloadReference of this CanaryTrait.
func (ca *CanaryTrait) GetWorkloadReference() runtimev1alpha1.TypedReference {
	return ca.Spec.WorkloadReference
}

// SetWorkloadReference of this CanaryTrait.
func (ca *CanaryTrait) SetWorkloadReference(r runtimev1alpha1.TypedReference) {
	ca.Spec.WorkloadReference = r
}
