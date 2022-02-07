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
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/klog/v2"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"github.com/xishengcai/oam/util/apply"
)

// Setup adds a controller that reconciles ApplicationConfigurations.
func Setup(mgr ctrl.Manager, args controller.Args) error {
	dm, err := discoverymapper.New(mgr.GetConfig())
	if err != nil {
		return err
	}
	name := "oam/" + strings.ToLower(v1alpha2.ApplicationConfigurationGroupKind)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha2.ApplicationConfiguration{}).
		Watches(&source.Kind{Type: &v1alpha2.Component{}}, &ComponentHandler{
			Client:        mgr.GetClient(),
			RevisionLimit: args.RevisionLimit,
		}).
		Complete(NewReconciler(mgr, dm,
			WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
			WithApplyOnceOnly(args.ApplyOnceOnly),
			WithSetSync(args.SyncTime)))
}

// An OAMApplicationReconciler reconciles OAM ApplicationConfigurations by rendering and
// instantiating their Components and Traits.
type OAMApplicationReconciler struct {
	client        client.Client
	components    ComponentRenderer
	volumeClaims  VolumeClaimRenderer
	workloads     WorkloadApplicator
	gc            GarbageCollector
	scheme        *runtime.Scheme
	record        event.Recorder
	applyOnceOnly bool
	syncTime      time.Duration
	preHooks      map[string]ControllerHooks
	postHooks     map[string]ControllerHooks
}

// NewReconciler returns an OAMApplicationReconciler that reconciles ApplicationConfigurations
// by rendering and instantiating their Components and Traits.
func NewReconciler(m ctrl.Manager, dm discoverymapper.DiscoveryMapper, o ...ReconcilerOption) *OAMApplicationReconciler {
	r := &OAMApplicationReconciler{
		client: m.GetClient(),
		scheme: m.GetScheme(),
		components: &components{
			client:   m.GetClient(),
			dm:       dm,
			params:   ParameterResolveFn(resolve),
			workload: ResourceRenderFn(renderWorkload),
			trait:    ResourceRenderFn(renderTrait),
		},
		volumeClaims: &volumeClaims{
			applicator: apply.NewAPIApplicator(m.GetClient()),
		},

		gc:        GarbageCollectorFn(eligible),
		record:    event.NewNopRecorder(),
		preHooks:  make(map[string]ControllerHooks),
		postHooks: make(map[string]ControllerHooks),
		workloads: &workloads{
			applicator: apply.NewAPIApplicator(m.GetClient()),
			rawClient:  m.GetClient(),
			dm:         dm,
		},
	}

	for _, ro := range o {
		ro(r)
	}

	return r
}

// Reconcile an OAM ApplicationConfigurations by rendering and instantiating its
// Components and Traits.
// NOTE(negz): We don't validate anything against their definitions at the
// controller level. We assume this will be done by validating admission
// webhooks.
func (r *OAMApplicationReconciler) Reconcile(req reconcile.Request) (result reconcile.Result, returnErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()

	ac := &v1alpha2.ApplicationConfiguration{}
	if err := r.client.Get(ctx, req.NamespacedName, ac); err != nil {
		return reconcile.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetAppConfig)
	}
	acPatch := ac.DeepCopy()
	if err := r.handleDeleteTimeStamp(ctx, ac); err != nil {
		return reconcile.Result{}, err
	}
	// execute the posthooks at the end no matter what
	defer func() {
		updateObservedGeneration(ac)
		for name, hook := range r.postHooks {
			exeResult, err := hook.Exec(ctx, ac)
			if err != nil {
				klog.ErrorS(err, "Failed to execute post-hooks", "hook name", name, "requeue-after", result.RequeueAfter)
				r.record.Event(ac, event.Warning(reasonCannotExecutePosthooks, err))
				ac.SetConditions(v1alpha1.ReconcileError(errors.Wrap(err, errExecutePosthooks)))
				result = exeResult
				returnErr = errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
				return
			}
			r.record.Event(ac, event.Normal(reasonExecutePosthook, "Successfully executed a posthook", "posthook name", name))
		}
		returnErr = errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
	}()

	// execute the prehooks
	for name, hook := range r.preHooks {
		result, err := hook.Exec(ctx, ac)
		if err != nil {
			klog.ErrorS(err, "Failed to execute pre-hooks", "hook name", name)
			r.record.Event(ac, event.Warning(reasonCannotExecutePrehooks, err))
			ac.SetConditions(v1alpha1.ReconcileError(errors.Wrap(err, errExecutePrehooks)))
			return result, errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
		}
		r.record.Event(ac, event.Normal(reasonExecutePrehook, "Successfully executed a prehook", "prehook name ", name))
	}

	workloads, depStatus, err := r.components.Render(ctx, ac)
	if err != nil {
		klog.ErrorS(err, "Cannot render components", "requeue-after", time.Now().Add(shortWait))
		r.record.Event(ac, event.Warning(reasonCannotRenderComponents, err))
		ac.SetConditions(v1alpha1.ReconcileError(errors.Wrap(err, errRenderComponents)))
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
	}
	klog.V(1).InfoS("Successfully rendered components", "workloads", len(workloads))
	r.record.Event(ac, event.Normal(reasonRenderComponents, "Successfully rendered components", "workloads", strconv.Itoa(len(workloads))))

	applyOpts := []apply.ApplyOption{apply.MustBeControllableBy(ac.GetUID())}
	if r.applyOnceOnly {
		applyOpts = append(applyOpts, applyOnceOnly())
	}
	if err := r.workloads.Apply(ctx, ac.Status.Workloads, workloads, applyOpts...); err != nil {
		klog.ErrorS(err, "Cannot apply components", "requeue-after", time.Now().Add(shortWait))
		r.record.Event(ac, event.Warning(reasonCannotApplyComponents, err))
		ac.SetConditions(v1alpha1.ReconcileError(errors.Wrap(err, errApplyComponents)))
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
	}
	klog.InfoS("Successfully applied components", "workloads", len(workloads))
	r.record.Event(ac, event.Normal(reasonApplyComponents, "Successfully applied components", "workloads", strconv.Itoa(len(workloads))))

	// Kubernetes garbage collection will (by default) reap workloads and traits
	// when the appconfig that controls them (in the controller reference sense)
	// is deleted. Here we cover the case in which a component or one of its
	// traits is removed from an extant appconfig.
	for _, e := range r.gc.Eligible(ac.GetNamespace(), ac.Status.Workloads, workloads) {
		// https://github.com/golang/go/wiki/CommonMistakes#using-reference-to-loop-iterator-variable
		e := e

		//log = log.WithValues("kind", e.GetKind(), "name", e.GetName())
		record := r.record.WithAnnotations("kind", e.GetKind(), "name", e.GetName())

		if err := r.client.Delete(ctx, &e); resource.IgnoreNotFound(err) != nil {
			klog.ErrorS(err, "Cannot garbage collect component", "requeue-after", time.Now().Add(shortWait))
			record.Event(ac, event.Warning(reasonCannotGGComponents, err))
			ac.SetConditions(v1alpha1.ReconcileError(errors.Wrap(err, errGCComponent)))
			return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
		}
		klog.InfoS("Garbage collected resource")
		record.Event(ac, event.Normal(reasonGGComponent, "Successfully garbage collected component"))
	}

	// create volumeClaims
	if err := r.volumeClaims.Render(ctx, ac); err != nil {
		klog.ErrorS(err, "Cannot apply volumeClaims", "requeue-after", time.Now().Add(shortWait))
		r.record.Event(ac, event.Warning(reasonCannotApplyComponents, err))
		ac.SetConditions(v1alpha1.ReconcileError(errors.Wrap(err, errApplyComponents)))
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
	}
	// patch the final status on the client side, k8s sever can't merge them
	r.updateStatus(ctx, ac, acPatch, workloads)

	if err := r.cleanUp(ctx, ac); err != nil {
		return reconcile.Result{RequeueAfter: shortWait}, errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
	}
	ac.Status.Dependency = v1alpha2.DependencyStatus{}
	if len(depStatus.Unsatisfied) != 0 {
		r.syncTime = dependCheckWait
		ac.Status.Dependency = *depStatus
	}

	// the post hook function will do the final status update
	return reconcile.Result{RequeueAfter: r.syncTime}, nil
}

func (r *OAMApplicationReconciler) updateStatus(ctx context.Context, ac, acPatch *v1alpha2.ApplicationConfiguration,
	workloads []*Workload) {
	ac.Status.Workloads = make([]v1alpha2.WorkloadStatus, len(workloads))
	historyWorkloads := make([]v1alpha2.HistoryWorkload, 0)
	for i, w := range workloads {
		ac.Status.Workloads[i] = workloads[i].Status()
		if !w.RevisionEnabled {
			continue
		}
		var ul unstructured.UnstructuredList
		ul.SetKind(w.Workload.GetKind())
		ul.SetAPIVersion(w.Workload.GetAPIVersion())
		if err := r.client.List(ctx, &ul, client.MatchingLabels{
			oam.LabelAppName:         ac.Name,
			oam.LabelAppComponent:    w.ComponentName,
			oam.LabelOAMResourceType: oam.ResourceTypeWorkload}); err != nil {
			continue
		}
		for _, v := range ul.Items {
			if v.GetName() == w.ComponentRevisionName {
				continue
			}
			// These workload exists means the component is under progress of rollout
			// Trait will not work for these remaining workload
			historyWorkloads = append(historyWorkloads, v1alpha2.HistoryWorkload{
				Revision: v.GetName(),
				Reference: v1alpha1.TypedReference{
					APIVersion: v.GetAPIVersion(),
					Kind:       v.GetKind(),
					Name:       v.GetName(),
					UID:        v.GetUID(),
				},
			})
		}
	}
	ac.Status.HistoryWorkloads = historyWorkloads
	// patch the extra fields in the status that is wiped by the Status() function
	patchExtraStatusField(&ac.Status, &acPatch.Status)
	ac.SetConditions(v1alpha1.ReconcileSuccess())
}

func updateObservedGeneration(ac *v1alpha2.ApplicationConfiguration) {
	if ac.Status.ObservedGeneration != ac.Generation {
		ac.Status.ObservedGeneration = ac.Generation
	}
}

func patchExtraStatusField(acStatus, acPatchStatus *v1alpha2.ApplicationConfigurationStatus) {
	// patch the extra status back
	for i := range acStatus.Workloads {
		for _, w := range acPatchStatus.Workloads {
			// find the workload in the old status
			if acStatus.Workloads[i].ComponentRevisionName == w.ComponentRevisionName {
				if len(w.Status) > 0 {
					acStatus.Workloads[i].Status = w.Status
				}
				// find the trait
				for j := range acStatus.Workloads[i].Traits {
					for _, t := range w.Traits {
						tr := acStatus.Workloads[i].Traits[j].Reference
						if t.Reference.APIVersion == tr.APIVersion && t.Reference.Kind == tr.Kind && t.Reference.Name == tr.Name {
							if len(t.Status) > 0 {
								acStatus.Workloads[i].Traits[j].Status = t.Status
							}
						}
					}
				}
			}
		}
	}
}

// if any finalizers newly registered, return true
func registerFinalizers(ac *v1alpha2.ApplicationConfiguration) bool {
	newFinalizer := false
	if !meta.FinalizerExists(&ac.ObjectMeta, workloadScopeFinalizer) && hasScope(ac) {
		meta.AddFinalizer(&ac.ObjectMeta, workloadScopeFinalizer)
		newFinalizer = true
	}
	return newFinalizer
}

func hasScope(ac *v1alpha2.ApplicationConfiguration) bool {
	for _, c := range ac.Spec.Components {
		if len(c.Scopes) > 0 {
			return true
		}
	}
	return false
}

func (r *OAMApplicationReconciler) handleDeleteTimeStamp(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) error {
	if ac.ObjectMeta.DeletionTimestamp.IsZero() {
		if registerFinalizers(ac) {
			klog.Info("Register new finalizers", "finalizers", ac.ObjectMeta.Finalizers)
			return errors.Wrap(r.client.Update(context.Background(), ac), errUpdateAppConfigStatus)
		}
	} else {
		if err := r.workloads.Finalize(context.Background(), ac); err != nil {
			klog.ErrorS(err, "Failed to finalize workloads", "workloads status", ac.Status.Workloads)
			r.record.Event(ac, event.Warning(reasonCannotFinalizeWorkloads, err))
			ac.SetConditions(v1alpha1.ReconcileError(errors.Wrap(err, errFinalizeWorkloads)))
			return errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
		}
		return errors.Wrap(r.client.Update(ctx, ac), errUpdateAppConfigStatus)
	}
	return nil
}

func (r *OAMApplicationReconciler) cleanUp(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) error {
	labels := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			oam.LabelAppName: ac.Name,
		},
	}
	selector, _ := metav1.LabelSelectorAsSelector(labels)
	vcs := v1alpha2.VolumeClaimList{}
	if err := r.client.List(ctx, &vcs, &client.ListOptions{LabelSelector: selector}); err != nil {
		return err
	}

	for _, v := range vcs.Items {
		needDelete := true
		for _, x := range ac.Spec.VolumeClaims {
			if v.Name == x.Name {
				needDelete = false
				break
			}
		}
		if needDelete {
			if err := r.client.Delete(ctx, &v); err != nil {
				return err
			}
		}
	}
	return nil
}
