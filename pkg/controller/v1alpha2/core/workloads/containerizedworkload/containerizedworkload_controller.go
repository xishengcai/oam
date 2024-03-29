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

package containerizedworkload

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"k8s.io/klog/v2"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/util"
	"github.com/xishengcai/oam/util/apply"
)

// Reconcile error strings.
const (
	errRenderWorkload     = "cannot render workload"
	errApplyChildResource = "cannot apply the childResource"
	errRenderService      = "cannot render service"
	errApplyService       = "cannot apply the service"
	errGcService          = "cannot gc the service"
	errApplyConfigMap     = "cannot apply the configMap"
)

// Setup adds a controller that reconciles ContainerizedWorkload.
func Setup(mgr ctrl.Manager, args controller.Args) error {
	r := Reconciler{
		Client:     mgr.GetClient(),
		record:     event.NewAPIRecorder(mgr.GetEventRecorderFor("ContainerizedWorkload")),
		Scheme:     mgr.GetScheme(),
		applicator: apply.NewAPIApplicator(mgr.GetClient()),
	}
	return r.SetupWithManager(mgr)
}

// Reconciler reconciles a ContainerizedWorkload object
type Reconciler struct {
	client.Client
	record     event.Recorder
	Scheme     *runtime.Scheme
	applicator apply.Applicator
}

// Reconcile reconciles a ContainerizedWorkload object
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	var workload v1alpha2.ContainerizedWorkload
	if err := r.Get(ctx, req.NamespacedName, &workload); err != nil {
		if apierrors.IsNotFound(err) {
			klog.Info("Container workload is deleted")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	klog.Infof("Reconcile container namespace: %s, workload:%s", workload.Namespace, workload.Name)
	// log.Info("Get the workload", "apiVersion", workload.APIVersion, "kind", workload.Kind)
	// find the resource object to record the event to, default is the parent appConfig.
	eventObj, err := util.LocateParentAppConfig(ctx, r.Client, &workload)
	if eventObj == nil {
		klog.ErrorS(err, "LocateParentAppConfig failed", "workloadName", workload.Name)
		eventObj = &workload
	}
	err = r.checkWorkloadDependency(&workload)
	if err != nil {
		klog.ErrorS(err, "Failed to checkWorkloadDependency", "name", workload.Name)
		r.record.Event(eventObj, event.Warning(errRenderWorkload, err))
		return util.ReconcileWaitResult, err
	}

	// applicationConfiguration write label by workload child define
	if workload.Labels == nil {
		return ctrl.Result{}, errors.New("label is nil, default child resource deployment")
	}

	childObject, err := r.renderChildResource(&workload)
	if err != nil {
		klog.ErrorS(err, "Failed to render childObject", "name", workload.Name)
		r.record.Event(eventObj, event.Warning(errRenderWorkload, err))
		return util.ReconcileWaitResult,
			util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errRenderWorkload)))
	}

	err = r.checkLabelSelect(ctx, &workload, childObject)
	if client.IgnoreNotFound(err) != nil {
		klog.ErrorS(err, "Failed to render childObject", "name", workload.Name)
		r.record.Event(eventObj, event.Warning(errRenderWorkload, err))
		return util.ReconcileWaitResult,
			util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errRenderWorkload)))
	}

	workload.Status.Resources = nil
	// server side apply, only the fields we set are touched
	applyOpts := []apply.ApplyOption{apply.MustBeControllableBy(workload.GetUID())}
	if err = r.applicator.Apply(ctx, childObject, applyOpts...); err != nil {
		klog.ErrorS(err, "Failed to apply ", childObject.GetObjectKind())
		r.record.Event(eventObj, event.Warning(errApplyChildResource, err))
		return util.ReconcileWaitResult,
			util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errApplyChildResource)))
	}
	var uid types.UID
	childWorkloadKindString := workload.Spec.Type
	switch childWorkloadKindString {
	case v1alpha2.StatefulSetWorkloadType:
		uid = childObject.(*appsv1.StatefulSet).UID
	default:
		uid = childObject.(*appsv1.Deployment).UID
	}
	workload.Status.Resources = append(workload.Status.Resources,
		cpv1alpha1.TypedReference{
			APIVersion: childObject.GetObjectKind().GroupVersionKind().GroupVersion().String(),
			Kind:       childObject.GetObjectKind().GroupVersionKind().Kind,
			Name:       workload.Name,
			UID:        uid,
		},
	)
	r.record.Event(eventObj, event.Normal("childResource created",
		fmt.Sprintf("Workload `%s` successfully server side patched a childResource", workload.Name)))

	// garbage collect the deployment that we created but not needed
	if err = r.cleanupResources(ctx, &workload, string(childWorkloadKindString), uid); err != nil {
		klog.ErrorS(err, "Failed to clean up resources")
		r.record.Event(eventObj, event.Warning(errApplyChildResource, err))
	}
	// configMap
	configMapApplyOpts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(workload.GetUID())}
	configMaps, err := r.renderConfigMaps(ctx, &workload)
	if err != nil {
		klog.ErrorS(err, "Failed to render configMap")
		r.record.Event(eventObj, event.Warning(errRenderWorkload, err))
		return util.ReconcileWaitResult,
			util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errRenderWorkload)))
	}
	for _, cm := range configMaps {
		if err = r.Patch(ctx, cm, client.Apply, configMapApplyOpts...); err != nil {
			klog.ErrorS(err, "Failed to apply a configMap")
			r.record.Event(eventObj, event.Warning(errApplyConfigMap, err))
			return util.ReconcileWaitResult,
				util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errApplyConfigMap)))
		}
		r.record.Event(eventObj, event.Normal("ConfigMap created",
			fmt.Sprintf("Workload `%s` successfully server side patched a configmap `%s`",
				workload.Name, cm.Name)))
		// record the new deployment, new service
		workload.Status.Resources = append(workload.Status.Resources,
			cpv1alpha1.TypedReference{
				APIVersion: cm.GetObjectKind().GroupVersionKind().GroupVersion().String(),
				Kind:       cm.GetObjectKind().GroupVersionKind().Kind,
				Name:       cm.GetName(),
				UID:        cm.UID,
			},
		)
	}

	// service
	service, err := r.renderService(ctx, &workload, childObject)
	if err != nil {
		r.record.Event(eventObj, event.Warning(errRenderService, err))
		return util.ReconcileWaitResult,
			util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errRenderService)))
	}

	// PointToGrayName is gray workload
	if service != nil {
		// server side apply the service
		if err := r.applicator.Apply(ctx, service, applyOpts...); err != nil {
			klog.ErrorS(err, "Failed to apply a service")
			delErr := r.Client.Delete(ctx, service)
			if delErr != nil {
				klog.ErrorS(delErr, "Failed to delete a service")
			}
			r.record.Event(eventObj, event.Warning(errApplyService, err))
			return util.ReconcileWaitResult,
				util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errApplyService)))
		}
		r.record.Event(eventObj, event.Normal("Service created",
			fmt.Sprintf("Workload `%s` successfully server side patched a service `%s`",
				workload.Name, service.Name)))

		// garbage collect the service/deployments that we created but not needed
		if err := r.cleanupResources(ctx, &workload, serviceKind, service.UID); err != nil {
			klog.ErrorS(err, "Failed to clean up resources")
			r.record.Event(eventObj, event.Warning(errGcService, err))
		}

		// record the new deployment, new service
		workload.Status.Resources = append(workload.Status.Resources,
			cpv1alpha1.TypedReference{
				APIVersion: service.GetObjectKind().GroupVersionKind().GroupVersion().String(),
				Kind:       service.GetObjectKind().GroupVersionKind().Kind,
				Name:       service.GetName(),
				UID:        service.UID,
			},
		)
	}
	if err := r.Status().Update(ctx, &workload); err != nil {
		return util.ReconcileWaitResult, err
	}
	return ctrl.Result{}, util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileSuccess())
}

// checkLabelSelect compare the child resource label, if not same delete
// because use canary trait will modify pod select，so must delete old deployment
func (r *Reconciler) checkLabelSelect(ctx context.Context, workload *v1alpha2.ContainerizedWorkload, childObject runtime.Object) error {
	var currentLabels, renderLabels map[string]string
	objectKey := client.ObjectKey{
		Namespace: workload.Namespace,
		Name:      workload.Name,
	}
	switch workload.Spec.Type {
	case v1alpha2.StatefulSetWorkloadType:
		sts := childObject.(*appsv1.StatefulSet)
		emptyChild := &appsv1.StatefulSet{}
		err := r.Get(ctx, objectKey, emptyChild)
		if err != nil {
			return err
		}
		renderLabels = sts.Spec.Selector.MatchLabels
		currentLabels = emptyChild.Spec.Selector.MatchLabels
	default:
		dep := childObject.(*appsv1.Deployment)
		emptyChild := &appsv1.Deployment{}
		err := r.Get(ctx, objectKey, emptyChild)
		if err != nil {
			return err
		}
		renderLabels = dep.Spec.Selector.MatchLabels
		currentLabels = emptyChild.Spec.Selector.MatchLabels
	}

	appID, ok := currentLabels[util.LabelAppID]
	if !ok {
		return fmt.Errorf("conflict, %v this namespace already has the same workload but does not belong to oam", objectKey)
	}

	if appID != renderLabels[util.LabelAppID] {
		return fmt.Errorf("conflict, oam appIdD %s already the same name workload: %s", appID, workload.Name)
	}

	if !reflect.DeepEqual(currentLabels, renderLabels) {
		err := r.Client.Delete(ctx, childObject)
		if err != nil {
			return err
		}
	}
	return nil
}

// SetupWithManager setups up k8s controller.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	src := &v1alpha2.ContainerizedWorkload{}
	name := "oam/" + strings.ToLower(v1alpha2.ContainerizedWorkloadKind)
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(src).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.StatefulSet{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

func (r *Reconciler) checkWorkloadDependency(wl *v1alpha2.ContainerizedWorkload) error {
	deps := wl.Spec.Dependency
	ctx := context.Background()
	for _, dep := range deps {
		key := client.ObjectKey{Namespace: wl.Namespace, Name: dep.Name}
		switch dep.Kind {
		case v1alpha2.ContainerizedWorkloadKind:
			workload := &v1alpha2.ContainerizedWorkload{}
			if err := r.Get(ctx, key, workload); err != nil {
				return err
			}
			err := r.queryContainerizedWorkloadChildHealth(wl.Labels[util.LabelKeyChildResource], key)
			if err != nil {
				return err
			}
		case "HelmRelease":
			err := r.queryHelmReleaseWorkloadHealth(wl, dep.Name)
			if err != nil {
				return err
			}
		case "Third":
			klog.Infof("ContainerizedWorkload dependency third component, can't check health, we default pass")
			return nil

		default:
			klog.Infof("ignore Error.ContainerizedWorkload: %s find not support dependency kind: %s", wl.Name, dep.Name)
			return nil
		}
	}

	return nil
}

func (r *Reconciler) queryContainerizedWorkloadChildHealth(label string, key client.ObjectKey) error {
	ctx := context.Background()
	switch label {
	case util.KindDeployment:
		deploy := &appsv1.Deployment{}
		err := r.Get(ctx, key, deploy)
		if err != nil {
			return err
		}
		if deploy.Status.ReadyReplicas != deploy.Status.Replicas {
			return fmt.Errorf("deployment: %s is not ready, ReadyReplicas: %d, Replicas: %d",
				deploy.Name, deploy.Status.ReadyReplicas, deploy.Status.Replicas)
		}
	case util.KindStatefulSet:
		sts := &appsv1.StatefulSet{}
		err := r.Get(ctx, key, sts)
		if err != nil {
			return err
		}
		if sts.Status.ReadyReplicas != sts.Status.Replicas {
			return fmt.Errorf("statefulSet:%s is not ready, ReadyReplicas: %d, Replicas: %d",
				sts.Name, sts.Status.ReadyReplicas, sts.Status.Replicas)
		}
	}
	return nil
}

func (r *Reconciler) queryHelmReleaseWorkloadHealth(wl *v1alpha2.ContainerizedWorkload, depName string) error {
	ctx := context.Background()

	notFoundFlag := true

	// check deployment status
	labelSelect := client.MatchingLabels{
		util.LabelAppID:       wl.Labels[util.LabelAppID],
		util.LabelComponentID: depName,
	}
	deployList := &appsv1.DeploymentList{}
	err := r.List(ctx, deployList, labelSelect)
	if err != nil {
		return err
	}

	for _, deploy := range deployList.Items {
		notFoundFlag = false
		if deploy.Status.ReadyReplicas != deploy.Status.Replicas {
			return fmt.Errorf("deployment %s is not ready, ReadyReplicas: %d, Replicas: %d",
				deploy.Name, deploy.Status.ReadyReplicas, deploy.Status.Replicas)
		}
	}

	// check statefulSet status
	statefulSetList := &appsv1.StatefulSetList{}
	err = r.List(ctx, statefulSetList, labelSelect)
	if err != nil {
		return err
	}
	for _, sts := range statefulSetList.Items {
		notFoundFlag = false
		if sts.Status.ReadyReplicas != sts.Status.Replicas {
			return fmt.Errorf("statufulSet %s is not ready, ReadyReplicas: %d, Replicas: %d",
				sts.Name, sts.Status.ReadyReplicas, sts.Status.Replicas)
		}
	}

	if notFoundFlag {
		return fmt.Errorf("component %s not found dependency %s's workload", wl.Name, depName)
	}
	return nil
}
