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

package manualscalertrait

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/klog/v2"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	cpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/kube-openapi/pkg/util/proto"
	"k8s.io/kubectl/pkg/explain"
	"k8s.io/kubectl/pkg/util/openapi"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"github.com/xishengcai/oam/pkg/oam/util"
)

// Reconcile error strings.
const (
	errQueryOpenAPI            = "failed to query openAPI"
	errPatchTobeScaledResource = "cannot patch the resource for scale"
	errScaleResource           = "cannot scale the resource"
)

// Setup adds a controller that reconciles ContainerizedWorkload.
func Setup(mgr ctrl.Manager, args controller.Args) error {
	dm, err := discoverymapper.New(mgr.GetConfig())
	if err != nil {
		return err
	}
	r := Reconciler{
		Client:          mgr.GetClient(),
		DiscoveryClient: *discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig()),
		dm:              dm,
		record:          event.NewAPIRecorder(mgr.GetEventRecorderFor("ManualScalarTrait")),
		Scheme:          mgr.GetScheme(),
	}
	return r.SetupWithManager(mgr)
}

// Reconciler reconciles a ManualScalarTrait object
type Reconciler struct {
	client.Client
	discovery.DiscoveryClient
	dm     discoverymapper.DiscoveryMapper
	record event.Recorder
	Scheme *runtime.Scheme
}

// Reconcile to reconcile manual trait.
// +kubebuilder:rbac:groups=core.oam.dev,resources=manualscalertraits,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.oam.dev,resources=manualscalertraits/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads,verbs=get;list;
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads/status,verbs=get;
// +kubebuilder:rbac:groups=core.oam.dev,resources=workloaddefinition,verbs=get;list;
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch;delete
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	var manualScalar oamv1alpha2.ManualScalerTrait
	if err := r.Get(ctx, req.NamespacedName, &manualScalar); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	klog.InfoS("Get the manualscalar trait", "ReplicaCount", manualScalar.Spec.ReplicaCount)
	// find the resource object to record the event to, default is the parent appConfig.
	eventObj, err := util.LocateParentAppConfig(ctx, r.Client, &manualScalar)
	if eventObj == nil {
		// fallback to workload itself
		klog.ErrorS(err, "Failed to find the parent resource", "manualScalar", manualScalar.Name)
		eventObj = &manualScalar
	}

	// Fetch the workload instance this trait is referring to
	workload, err := util.FetchWorkload(ctx, r, &manualScalar)
	if err != nil {
		r.record.Event(eventObj, event.Warning(util.ErrLocateWorkload, err))
		return util.ReconcileWaitResult, util.PatchCondition(
			ctx, r, &manualScalar, cpv1alpha1.ReconcileError(errors.Wrap(err, util.ErrLocateWorkload)))
	}

	// Fetch the child resources list from the corresponding workload
	resources, err := util.FetchWorkloadChildResources(ctx, r, r.dm, workload)
	if err != nil {
		klog.ErrorS(err, "Error while fetching the workload child resources", "workload", workload.UnstructuredContent())
		r.record.Event(eventObj, event.Warning(util.ErrFetchChildResources, err))
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &manualScalar, cpv1alpha1.ReconcileError(fmt.Errorf(util.ErrFetchChildResources)))
	}

	// Scale the child resources that we know how to scale
	err = r.scaleResources(ctx, manualScalar, resources)
	if err != nil {
		klog.ErrorS(err, "scale Resources")
		r.record.Event(eventObj, event.Warning(errScaleResource, err))
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &manualScalar, cpv1alpha1.ReconcileError(err))
	}
	r.record.Event(eventObj, event.Normal("Manual scalar applied",
		fmt.Sprintf("Trait `%s` successfully scaled a resource to %d instances",
			manualScalar.Name, manualScalar.Spec.ReplicaCount)))
	return ctrl.Result{}, util.PatchCondition(ctx, r, &manualScalar, cpv1alpha1.ReconcileSuccess())
}

// identify child resources and scale them
func (r *Reconciler) scaleResources(ctx context.Context, manualScalar oamv1alpha2.ManualScalerTrait, resources []*unstructured.Unstructured) error {
	// scale all the resources that we can scale
	isController := false
	bod := true
	found := false
	// Update owner references
	ownerRef := metav1.OwnerReference{
		APIVersion:         manualScalar.APIVersion,
		Kind:               manualScalar.Kind,
		Name:               manualScalar.Name,
		UID:                manualScalar.UID,
		Controller:         &isController,
		BlockOwnerDeletion: &bod,
	}
	// prepare for openApi schema check
	schemaDoc, err := r.DiscoveryClient.OpenAPISchema()
	if err != nil {
		return errors.Wrap(err, errQueryOpenAPI)
	}

	document, err := openapi.NewOpenAPIData(schemaDoc)
	if err != nil {
		return errors.Wrap(err, errQueryOpenAPI)
	}

	for _, res := range resources {
		if locateReplicaField(document, res) {
			found = true
			resPatch := client.MergeFrom(res.DeepCopyObject())
			klog.InfoS("Get the resource the trait is going to modify",
				"resource name", res.GetName(), "UID", res.GetUID())
			cpmeta.AddOwnerReference(res, ownerRef)
			err := unstructured.SetNestedField(res.Object, int64(manualScalar.Spec.ReplicaCount), "spec", "replicas")
			if err != nil {
				klog.ErrorS(err, "Failed to patch a resource for scaling")
				return errors.Wrap(err, errPatchTobeScaledResource)
			}
			// merge patch to scale the resource
			if err := r.Patch(ctx, res, resPatch, client.FieldOwner(manualScalar.GetUID())); err != nil {
				klog.ErrorS(err, "Failed to scale a resource")
				return errors.Wrap(err, errScaleResource)
			}
			klog.InfoS("Successfully scaled a resource", "resource GVK", res.GroupVersionKind().String(),
				"res UID", res.GetUID(), "target replica", manualScalar.Spec.ReplicaCount)
		}
	}
	if !found {
		return errors.New("Cannot locate any resource")
	}
	return nil
}

// locateReplicaField call openapi RESTFUL end point to fetch the schema of a given resource and try to see
// 	if it has a spec.replicas filed that is of type integer. We will apply duck typing to modify the fields there
//  assuming that the fields is used to control the number of instances of this resource
//  NOTE: This only works if the resource CRD has a structural schema, all `apiextensions.k8s.io/v1` CRDs do
// https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#specifying-a-structural-schema
func locateReplicaField(document openapi.Resources, res *unstructured.Unstructured) bool {
	// this is the most common path for replicas fields
	replicaFieldPath := []string{"spec", "replicas"}
	gv, err := schema.ParseGroupVersion(res.GetAPIVersion())
	if err != nil {
		return false
	}
	// we look up the resource schema definition by its GVK
	schemaR := document.LookupResource(schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    res.GetKind(),
	})
	// we try to see if there is a spec.replicas fields in its definition
	field, err := explain.LookupSchemaForField(schemaR, replicaFieldPath)
	if err != nil || field == nil {
		return false
	}
	// we also verify that it is of type integer to further narrow down the candidates
	replicaField, ok := field.(*proto.Primitive)
	if !ok || replicaField.Type != "integer" {
		return false
	}
	return true
}

// SetupWithManager to setup k8s controller.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	name := "oam/" + strings.ToLower(oamv1alpha2.ManualScalerTraitKind)
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&oamv1alpha2.ManualScalerTrait{}).
		Complete(r)
}
