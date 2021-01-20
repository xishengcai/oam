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
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"reflect"
	"strings"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/util"
)

// Reconcile error strings.
const (
	errRenderWorkload   = "cannot render workload"
	errApplyChildResource= "cannot apply the childResource"
	errRenderService    = "cannot render service"
	errApplyService     = "cannot apply the service"
	errGcService        = "cannot gc the service"
	errApplyConfigMap   = "cannot apply the configMap"
)

// Setup adds a controller that reconciles ContainerizedWorkload.
func Setup(mgr ctrl.Manager, args controller.Args, log logging.Logger) error {
	r := Reconciler{
		Client: mgr.GetClient(),
		log:    ctrl.Log.WithName("ContainerizedWorkload"),
		record: event.NewAPIRecorder(mgr.GetEventRecorderFor("ContainerizedWorkload")),
		Scheme: mgr.GetScheme(),
	}
	return r.SetupWithManager(mgr)
}

// Reconciler reconciles a ContainerizedWorkload object
type Reconciler struct {
	client.Client
	log    logr.Logger
	record event.Recorder
	Scheme *runtime.Scheme
	childResource []string
}

// Reconcile reconciles a ContainerizedWorkload object
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.log.WithValues("containerizedworkload", req.NamespacedName)
	log.Info("Reconcile container workload")

	var workload v1alpha2.ContainerizedWorkload
	if err := r.Get(ctx, req.NamespacedName, &workload); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Container workload is deleted")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	//log.Info("Get the workload", "apiVersion", workload.APIVersion, "kind", workload.Kind)
	// find the resource object to record the event to, default is the parent appConfig.
	eventObj, err := util.LocateParentAppConfig(ctx, r.Client, &workload)
	if eventObj == nil {
		log.Error(err, "workload", workload.Name)
		eventObj = &workload
	}

	// applicationConfiguration write label by workload child define
	if workload.Labels == nil {
		return ctrl.Result{}, errors.New("label is nil, default child resource deployment")
	}

	childObject, err := r.renderChildResource(&workload)
	if err != nil {
		log.Error(err, "Failed to render a deployment")
		r.record.Event(eventObj, event.Warning(errRenderWorkload, err))
		return util.ReconcileWaitResult,
			util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errRenderWorkload)))
	}
	r.checkLabelSelect(ctx,&workload,childObject)
	//r.childResource = childResourceValue
	workload.Status.Resources = nil
	applyOpts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(workload.GetUID())}



	//server side apply, only the fields we set are touched
	if err := r.Patch(ctx, childObject, client.Apply, applyOpts...); err != nil {
		log.Error(err, "Failed to apply to a deployment")
		r.record.Event(eventObj, event.Warning(errApplyChildResource, err))
		return util.ReconcileWaitResult,
			util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errApplyChildResource)))
	}
	var uid types.UID
	switch workload.Labels[util.LabelKeyChildResource] {
	case util.KindDeployment:
		uid = childObject.(*appsv1.Deployment).UID
	case util.KindStatefulSet:
		uid = childObject.(*appsv1.StatefulSet).UID
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
	if err := r.cleanupResources(ctx, &workload, util.KindDeployment, uid); err != nil {
		log.Error(err, "Failed to clean up resources")
		r.record.Event(eventObj, event.Warning(errApplyChildResource, err))
	}
	// configMap
	configMapApplyOpts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(workload.GetUID())}
	configMaps, err := r.renderConfigMaps(ctx, &workload)
	if err != nil {
		log.Error(err, "Failed to render configMap")
		r.record.Event(eventObj, event.Warning(errRenderWorkload, err))
		return util.ReconcileWaitResult,
			util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errRenderWorkload)))
	}
	for _, cm := range configMaps {
		if err := r.Patch(ctx, cm, client.Apply, configMapApplyOpts...); err != nil {
			log.Error(err, "Failed to apply a configMap")
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
	if service != nil{
		// server side apply the service
		if err := r.Patch(ctx, service, client.Apply, applyOpts...); err != nil {
			log.Error(err, "Failed to apply a service")
			r.record.Event(eventObj, event.Warning(errApplyService, err))
			return util.ReconcileWaitResult,
				util.PatchCondition(ctx, r, &workload, cpv1alpha1.ReconcileError(errors.Wrap(err, errApplyService)))
		}
		//r.record.Event(eventObj, event.Normal("Service created",
		//	fmt.Sprintf("Workload `%s` successfully server side patched a service `%s`",
		//		workload.Name, service.Name)))
		// garbage collect the service/deployments that we created but not needed
		if err := r.cleanupResources(ctx, &workload, serviceKind, service.UID); err != nil {
			log.Error(err, "Failed to clean up resources")
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

func (r *Reconciler)checkLabelSelect(ctx context.Context,workload *v1alpha2.ContainerizedWorkload, childObject runtime.Object){
	switch workload.Labels[util.LabelKeyChildResource] {
	case util.KindDeployment:
		dep := childObject.(*appsv1.Deployment)
		emptyChild := &appsv1.Deployment{}
		err := r.Get(ctx,client.ObjectKey{Namespace: workload.Namespace,Name:workload.Name}, emptyChild)
		if err == nil {
			if !reflect.DeepEqual(dep.Spec.Selector.MatchLabels,emptyChild.Spec.Selector.MatchLabels){
				err = r.Client.Delete(ctx, emptyChild)
				if err != nil{
					klog.Error("failed delete statefulSet ", emptyChild.Name)
				}else{
					klog.Infof("success delete deployment ", emptyChild.Name)
				}
			}
		}
	case util.KindStatefulSet:
		dep := childObject.(*appsv1.StatefulSet)
		emptyChild := &appsv1.StatefulSet{}
		err := r.Get(ctx,client.ObjectKey{Namespace: workload.Namespace,Name:workload.Name}, emptyChild)
		if err == nil {
			if !reflect.DeepEqual(dep.Spec.Selector.MatchLabels,emptyChild.Spec.Selector.MatchLabels){
				err = r.Client.Delete(ctx, emptyChild)
				if err != nil{
					klog.Errorf("failed delete statefulSet ", emptyChild.Name)
				}else{
					klog.Infof("success delete deployment ", emptyChild.Name)
				}
			}
		}
	}

}
