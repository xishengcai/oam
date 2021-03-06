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

package applicationconfiguration

import (
	"context"

	"github.com/pkg/errors"

	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/oam-dev/kubevela/pkg/utils/apply"

	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"github.com/xishengcai/oam/pkg/oam/util"
)

// Reconcile error strings.
const (
	errFmtApplyWorkload            = "cannot apply workload %q"
	errFmtSetWorkloadRef           = "cannot set trait %q reference to %q"
	errFmtSetScopeWorkloadRef      = "cannot set scope %q reference to %q"
	errFmtGetTraitDefinition       = "cannot find trait definition %q %q %q"
	errFmtGetScopeDefinition       = "cannot find scope definition %q %q %q"
	errFmtGetScopeWorkloadRef      = "cannot find scope workloadRef %q %q %q with workloadRefsPath %q"
	errFmtGetScopeWorkloadRefsPath = "cannot get workloadRefsPath for scope to be dereferenced %q %q %q"
	errFmtApplyTrait               = "cannot apply trait %q %q %q"
	errFmtApplyScope               = "cannot apply scope %q %q %q"

	workloadScopeFinalizer = "scope.finalizer.core.oam.dev"
)

// A WorkloadApplicator creates or updates or finalizes workloads and their traits.
type WorkloadApplicator interface {
	// Apply a workload and its traits.
	Apply(ctx context.Context, status []v1alpha2.WorkloadStatus, w []*Workload, ao ...apply.ApplyOption) error

	// Finalize implements pre-delete hooks on workloads
	Finalize(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) error
}

type workloads struct {
	applicator apply.Applicator
	rawClient  client.Client
	dm         discoverymapper.DiscoveryMapper
}

func (a *workloads) Apply(ctx context.Context, status []v1alpha2.WorkloadStatus, w []*Workload, ao ...apply.ApplyOption) error {
	if len(w) == 0 {
		return nil
	}

	// they are all in the same namespace
	var namespace = w[0].Workload.GetNamespace()
	for _, wl := range w {
		if !wl.HasDep {
			err := a.applicator.Apply(ctx, wl.Workload, ao...)
			if err != nil {
				if ok := errors.As(err, &GenerationUnchanged{}); !ok {
					// GenerationUnchanged only aborts applying current workload
					// but not blocks the whole reconciliation through returning an error
					return errors.Wrapf(err, errFmtApplyWorkload, wl.Workload.GetName())
				}
			}
		}
		for _, trait := range wl.Traits {
			if trait.HasDep {
				continue
			}
			t := trait.Object
			if err := a.applicator.Apply(ctx, &trait.Object, ao...); err != nil {
				if ok := errors.As(err, &GenerationUnchanged{}); !ok {
					// GenerationUnchanged only aborts applying current trait
					// but not blocks the whole reconciliation through returning an error
					return errors.Wrapf(err, errFmtApplyTrait, t.GetAPIVersion(), t.GetKind(), t.GetName())
				}
			}
		}
		workloadRef := runtimev1alpha1.TypedReference{
			APIVersion: wl.Workload.GetAPIVersion(),
			Kind:       wl.Workload.GetKind(),
			Name:       wl.Workload.GetName(),
		}
		for _, s := range wl.Scopes {
			if err := a.applyScope(ctx, *wl, s, workloadRef); err != nil {
				return err
			}
		}
	}

	return a.dereferenceScope(ctx, namespace, status, w)
}

func (a *workloads) Finalize(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) error {
	var namespace = ac.GetNamespace()

	if meta.FinalizerExists(&ac.ObjectMeta, workloadScopeFinalizer) {
		if err := a.dereferenceAllScopes(ctx, namespace, ac.Status.Workloads); err != nil {
			return err
		}
		meta.RemoveFinalizer(&ac.ObjectMeta, workloadScopeFinalizer)
	}

	// add finalizer logic here
	return nil
}

func (a *workloads) dereferenceScope(ctx context.Context, namespace string, status []v1alpha2.WorkloadStatus, w []*Workload) error {
	for _, st := range status {
		toBeDeferenced := st.Scopes
		for _, wl := range w {
			if (st.Reference.APIVersion == wl.Workload.GetAPIVersion()) &&
				(st.Reference.Kind == wl.Workload.GetKind()) &&
				(st.Reference.Name == wl.Workload.GetName()) {
				toBeDeferenced = findDereferencedScopes(st.Scopes, wl.Scopes)
			}
		}

		for _, s := range toBeDeferenced {
			if err := a.applyScopeRemoval(ctx, namespace, st.Reference, s); err != nil {
				return err
			}
		}
	}

	return nil
}

// dereferenceAllScope dereferences workloads owned by the appConfig being deleted from the scopes they belong to.
func (a *workloads) dereferenceAllScopes(ctx context.Context, namespace string, status []v1alpha2.WorkloadStatus) error {
	for _, st := range status {
		for _, sc := range st.Scopes {
			if err := a.applyScopeRemoval(ctx, namespace, st.Reference, sc); err != nil {
				return err
			}
		}
	}

	return nil
}

func findDereferencedScopes(statusScopes []v1alpha2.WorkloadScope, scopes []unstructured.Unstructured) []v1alpha2.WorkloadScope {
	var toBeDeferenced []v1alpha2.WorkloadScope
	for _, ss := range statusScopes {
		found := false
		for _, s := range scopes {
			if (s.GetAPIVersion() == ss.Reference.APIVersion) &&
				(s.GetKind() == ss.Reference.Kind) &&
				(s.GetName() == ss.Reference.Name) {
				found = true
				break
			}
		}

		if !found {
			toBeDeferenced = append(toBeDeferenced, ss)
		}
	}

	return toBeDeferenced
}

func (a *workloads) applyScope(ctx context.Context, wl Workload, s unstructured.Unstructured,
	workloadRef runtimev1alpha1.TypedReference) error {
	// get ScopeDefinition
	scopeDefinition, err := util.FetchScopeDefinition(ctx, a.rawClient, a.dm, &s)
	if err != nil {
		return errors.Wrapf(err, errFmtGetScopeDefinition, s.GetAPIVersion(), s.GetKind(), s.GetName())
	}
	// checkout whether scope asks for workloadRef
	workloadRefsPath := scopeDefinition.Spec.WorkloadRefsPath
	if workloadRefsPath == "" {
		// this scope does not ask for workloadRefs
		return nil
	}

	var refs []interface{}
	if value, err := fieldpath.Pave(s.UnstructuredContent()).GetValue(workloadRefsPath); err == nil {
		refs = value.([]interface{})

		for _, item := range refs {
			ref := item.(map[string]interface{})
			if (workloadRef.APIVersion == ref["apiVersion"]) &&
				(workloadRef.Kind == ref["kind"]) &&
				(workloadRef.Name == ref["name"]) {
				// workloadRef is already present, so no need to add it.
				return nil
			}
		}
	} else {
		return errors.Wrapf(err, errFmtGetScopeWorkloadRef, s.GetAPIVersion(), s.GetKind(), s.GetName(), workloadRefsPath)
	}

	refs = append(refs, workloadRef)
	if err := fieldpath.Pave(s.UnstructuredContent()).SetValue(workloadRefsPath, refs); err != nil {
		return errors.Wrapf(err, errFmtSetScopeWorkloadRef, s.GetName(), wl.Workload.GetName())
	}

	if err := a.rawClient.Update(ctx, &s); err != nil {
		return errors.Wrapf(err, errFmtApplyScope, s.GetAPIVersion(), s.GetKind(), s.GetName())
	}

	return nil
}

func (a *workloads) applyScopeRemoval(ctx context.Context, namespace string, wr runtimev1alpha1.TypedReference, s v1alpha2.WorkloadScope) error {
	scopeObject := unstructured.Unstructured{}
	scopeObject.SetAPIVersion(s.Reference.APIVersion)
	scopeObject.SetKind(s.Reference.Kind)
	scopeObjectRef := types.NamespacedName{Namespace: namespace, Name: s.Reference.Name}
	if err := a.rawClient.Get(ctx, scopeObjectRef, &scopeObject); err != nil {
		// if the scope is already deleted
		// treat it as removal done to avoid blocking AppConfig finalizer
		if apierrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, errFmtApplyScope, s.Reference.APIVersion, s.Reference.Kind, s.Reference.Name)
	}

	scopeDefinition, err := util.FetchScopeDefinition(ctx, a.rawClient, a.dm, &scopeObject)
	if err != nil {
		return errors.Wrapf(err, errFmtGetScopeDefinition, scopeObject.GetAPIVersion(), scopeObject.GetKind(), scopeObject.GetName())
	}

	workloadRefsPath := scopeDefinition.Spec.WorkloadRefsPath
	if workloadRefsPath == "" {
		// Scopes to be dereferenced MUST have workloadRefsPath
		return errors.Errorf(errFmtGetScopeWorkloadRefsPath, scopeObject.GetAPIVersion(), scopeObject.GetKind(), scopeObject.GetName())
	}

	if value, err := fieldpath.Pave(scopeObject.UnstructuredContent()).GetValue(workloadRefsPath); err == nil {
		refs := value.([]interface{})

		workloadRefIndex := -1
		for i, item := range refs {
			ref := item.(map[string]interface{})
			if (wr.APIVersion == ref["apiVersion"]) &&
				(wr.Kind == ref["kind"]) &&
				(wr.Name == ref["name"]) {
				workloadRefIndex = i
				break
			}
		}

		if workloadRefIndex >= 0 {
			// Remove the element at index i.
			refs[workloadRefIndex] = refs[len(refs)-1]
			refs = refs[:len(refs)-1]

			if err = fieldpath.Pave(scopeObject.UnstructuredContent()).SetValue(workloadRefsPath, refs); err != nil {
				return errors.Wrapf(err, errFmtSetScopeWorkloadRef, s.Reference.Name, wr.Name)
			}

			if err = a.rawClient.Update(ctx, &scopeObject); err != nil {
				return errors.Wrapf(err, errFmtApplyScope, s.Reference.APIVersion, s.Reference.Kind, s.Reference.Name)
			}
		}
	} else {
		return errors.Wrapf(err, errFmtGetScopeWorkloadRef,
			scopeObject.GetAPIVersion(), scopeObject.GetKind(), scopeObject.GetName(), workloadRefsPath)
	}
	return nil
}
