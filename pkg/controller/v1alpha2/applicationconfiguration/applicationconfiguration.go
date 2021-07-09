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
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/xishengcai/oam/pkg/oam/util"
	"github.com/xishengcai/oam/util/apply"
)

const (
	reconcileTimeout = 1 * time.Minute
	dependCheckWait  = 10 * time.Second
	shortWait        = 30 * time.Second
)

// Reconcile error strings.
const (
	errGetAppConfig          = "cannot get application configuration"
	errUpdateAppConfigStatus = "cannot update application configuration status"
	errExecutePrehooks       = "failed to execute pre-hooks"
	errExecutePosthooks      = "failed to execute post-hooks"
	errRenderComponents      = "cannot render components"
	errApplyComponents       = "cannot apply components"
	errGCComponent           = "cannot garbage collect components"
	errFinalizeWorkloads     = "failed to finalize workloads"
)

// Reconcile event reasons.
const (
	reasonRenderComponents        = "RenderedComponents"
	reasonExecutePrehook          = "ExecutePrehook"
	reasonExecutePosthook         = "ExecutePosthook"
	reasonApplyComponents         = "AppliedComponents"
	reasonGGComponent             = "GarbageCollectedComponent"
	reasonCannotExecutePrehooks   = "CannotExecutePrehooks"
	reasonCannotExecutePosthooks  = "CannotExecutePosthooks"
	reasonCannotRenderComponents  = "CannotRenderComponents"
	reasonCannotApplyComponents   = "CannotApplyComponents"
	reasonCannotGGComponents      = "CannotGarbageCollectComponents"
	reasonCannotFinalizeWorkloads = "CannotFinalizeWorkloads"
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
	workloads     WorkloadApplicator
	gc            GarbageCollector
	scheme        *runtime.Scheme
	record        event.Recorder
	preHooks      map[string]ControllerHooks
	postHooks     map[string]ControllerHooks
	applyOnceOnly bool
	syncTime      time.Duration
}

// A ReconcilerOption configures a Reconciler.
type ReconcilerOption func(*OAMApplicationReconciler)

// WithRecorder specifies how the Reconciler should record events.
func WithRecorder(er event.Recorder) ReconcilerOption {
	return func(r *OAMApplicationReconciler) {
		r.record = er
	}
}

// WithApplyOnceOnly indicates whether workloads and traits should be
// affected if no spec change is made in the ApplicationConfiguration.
func WithApplyOnceOnly(applyOnceOnly bool) ReconcilerOption {
	return func(r *OAMApplicationReconciler) {
		r.applyOnceOnly = applyOnceOnly
	}
}

// WithSetSync set Reconciler exec interval
func WithSetSync(syncTime time.Duration) ReconcilerOption {
	return func(r *OAMApplicationReconciler) {
		r.syncTime = syncTime
	}
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

	if ac.ObjectMeta.DeletionTimestamp.IsZero() {
		if registerFinalizers(ac) {
			klog.Info("Register new finalizers", "finalizers", ac.ObjectMeta.Finalizers)
			return reconcile.Result{}, errors.Wrap(r.client.Update(ctx, ac), errUpdateAppConfigStatus)
		}
	} else {
		if err := r.workloads.Finalize(ctx, ac); err != nil {
			klog.ErrorS(err, "Failed to finalize workloads", "workloads status", ac.Status.Workloads, "requeue-after", result.RequeueAfter)
			r.record.Event(ac, event.Warning(reasonCannotFinalizeWorkloads, err))
			ac.SetConditions(v1alpha1.ReconcileError(errors.Wrap(err, errFinalizeWorkloads)))
			return reconcile.Result{}, errors.Wrap(r.client.Status().Update(ctx, ac), errUpdateAppConfigStatus)
		}
		return reconcile.Result{}, errors.Wrap(r.client.Update(ctx, ac), errUpdateAppConfigStatus)
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

	//log = log.WithValues("uid", ac.GetUID(), "version", ac.GetResourceVersion())

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

	// patch the final status on the client side, k8s sever can't merge them
	r.updateStatus(ctx, ac, acPatch, workloads)

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

// A Workload produced by an OAM ApplicationConfiguration.
type Workload struct {
	// ComponentName that produced this workload.
	ComponentName string

	// ComponentRevisionName of current component
	ComponentRevisionName string

	// A Workload object.
	Workload *unstructured.Unstructured

	// HasDep indicates whether this resource has dependencies and unready to be applied.
	HasDep bool

	// Traits associated with this workload.
	Traits []*Trait

	// RevisionEnabled means multiple workloads of same component will possibly be alive.
	RevisionEnabled bool

	// Scopes associated with this workload.
	Scopes []unstructured.Unstructured
}

// A Trait produced by an OAM ApplicationConfiguration.
type Trait struct {
	Object unstructured.Unstructured

	// HasDep indicates whether this resource has dependencies and unready to be applied.
	HasDep bool

	// Definition indicates the trait's definition
	Definition v1alpha2.TraitDefinition
}

// Status produces the status of this workload and its traits, suitable for use
// in the status of an ApplicationConfiguration.
func (w Workload) Status() v1alpha2.WorkloadStatus {
	acw := v1alpha2.WorkloadStatus{
		ComponentName:         w.ComponentName,
		ComponentRevisionName: w.ComponentRevisionName,
		Reference: v1alpha1.TypedReference{
			APIVersion: w.Workload.GetAPIVersion(),
			Kind:       w.Workload.GetKind(),
			Name:       w.Workload.GetName(),
		},
		Traits: make([]v1alpha2.WorkloadTrait, len(w.Traits)),
		Scopes: make([]v1alpha2.WorkloadScope, len(w.Scopes)),
	}
	for i, tr := range w.Traits {
		if tr.Definition.Name == util.Dummy && tr.Definition.Spec.Reference.Name == util.Dummy {
			acw.Traits[i].Message = util.DummyTraitMessage
		}
		acw.Traits[i].Reference = v1alpha1.TypedReference{
			APIVersion: w.Traits[i].Object.GetAPIVersion(),
			Kind:       w.Traits[i].Object.GetKind(),
			Name:       w.Traits[i].Object.GetName(),
		}
	}
	for i, s := range w.Scopes {
		acw.Scopes[i].Reference = v1alpha1.TypedReference{
			APIVersion: s.GetAPIVersion(),
			Kind:       s.GetKind(),
			Name:       s.GetName(),
		}
	}
	return acw
}

// A GarbageCollector returns resource eligible for garbage collection. A
// resource is considered eligible if a reference exists in the supplied slice
// of workload statuses, but not in the supplied slice of workloads.
type GarbageCollector interface {
	Eligible(namespace string, ws []v1alpha2.WorkloadStatus, w []*Workload) []unstructured.Unstructured
}

// A GarbageCollectorFn returns resource eligible for garbage collection.
type GarbageCollectorFn func(namespace string, ws []v1alpha2.WorkloadStatus, w []*Workload) []unstructured.Unstructured

// Eligible resources.
func (fn GarbageCollectorFn) Eligible(namespace string, ws []v1alpha2.WorkloadStatus, w []*Workload) []unstructured.Unstructured {
	return fn(namespace, ws, w)
}

// IsRevisionWorkload check is a workload is an old revision Workload which shouldn't be garbage collected.
// TODO(wonderflow): Do we have a better way to recognize it's a revisionWorkload which can't be garbage collected by AppConfig?
func IsRevisionWorkload(status v1alpha2.WorkloadStatus) bool {
	return strings.HasPrefix(status.Reference.Name, status.ComponentName+"-")
}

func eligible(namespace string, ws []v1alpha2.WorkloadStatus, w []*Workload) []unstructured.Unstructured {
	applied := make(map[v1alpha1.TypedReference]bool)
	for _, wl := range w {
		r := v1alpha1.TypedReference{
			APIVersion: wl.Workload.GetAPIVersion(),
			Kind:       wl.Workload.GetKind(),
			Name:       wl.Workload.GetName(),
		}
		applied[r] = true
		for _, t := range wl.Traits {
			r := v1alpha1.TypedReference{
				APIVersion: t.Object.GetAPIVersion(),
				Kind:       t.Object.GetKind(),
				Name:       t.Object.GetName(),
			}
			applied[r] = true
		}
	}
	eligible := make([]unstructured.Unstructured, 0)
	for _, s := range ws {
		if !applied[s.Reference] && !IsRevisionWorkload(s) {
			w := &unstructured.Unstructured{}
			w.SetAPIVersion(s.Reference.APIVersion)
			w.SetKind(s.Reference.Kind)
			w.SetNamespace(namespace)
			w.SetName(s.Reference.Name)
			eligible = append(eligible, *w)
		}

		for _, ts := range s.Traits {
			if !applied[ts.Reference] {
				t := &unstructured.Unstructured{}
				t.SetAPIVersion(ts.Reference.APIVersion)
				t.SetKind(ts.Reference.Kind)
				t.SetNamespace(namespace)
				t.SetName(ts.Reference.Name)
				eligible = append(eligible, *t)
			}
		}
	}

	return eligible
}

// GenerationUnchanged indicates the resource being applied has no generation changed
// comparing to the existing one.
type GenerationUnchanged struct{}

func (e *GenerationUnchanged) Error() string {
	return fmt.Sprint("apply-only-once enabled,",
		"and detect generation in the annotation unchanged, will not apply.",
		"Please ignore this error in other logic.")
}

func applyOnceOnly() apply.ApplyOption {
	return func(ctx context.Context, current, desired runtime.Object) error {
		// ApplyOption only works for update/patch operation and will be ignored
		// if the object doesn't exist before.
		c, _ := current.(metav1.Object)
		d, _ := desired.(metav1.Object)
		if c == nil || d == nil {
			return errors.Errorf("invalid object being applied: %q ",
				desired.GetObjectKind().GroupVersionKind())
		}
		cLabels, dLabels := c.GetLabels(), d.GetLabels()
		if dLabels[oam.LabelOAMResourceType] == oam.ResourceTypeWorkload ||
			dLabels[oam.LabelOAMResourceType] == oam.ResourceTypeTrait {
			// check whether spec changes occur on the workload or trait,
			// according to annotations and lables
			if c.GetAnnotations()[oam.AnnotationAppGeneration] !=
				d.GetAnnotations()[oam.AnnotationAppGeneration] {
				return nil
			}
			if cLabels[oam.LabelAppComponentRevision] != dLabels[oam.LabelAppComponentRevision] ||
				cLabels[oam.LabelAppComponent] != dLabels[oam.LabelAppComponent] ||
				cLabels[oam.LabelAppName] != dLabels[oam.LabelAppName] {
				return nil
			}
			// return an error to abort current apply
			return &GenerationUnchanged{}
		}
		return nil
	}
}
