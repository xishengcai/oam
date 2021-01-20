package volumetrait

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	clientappv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	cpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam/util"
)

// Reconcile error strings.
const (
	errQueryOpenAPI = "failed to query openAPI"
	errMountVolume  = "cannot scale the resource"
	errApplyPVC     = "cannot apply the pvc"
)

// Setup adds a controller that reconciles ContainerizedWorkload.
func Setup(mgr ctrl.Manager, args controller.Args, log logging.Logger) error {
	dm, err := discoverymapper.New(mgr.GetConfig())
	if err != nil {
		return err
	}
	r := Reconcile{
		Client:          mgr.GetClient(),
		DiscoveryClient: *discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig()),
		log:             ctrl.Log.WithName("VolumeTrait"),
		record:          event.NewAPIRecorder(mgr.GetEventRecorderFor("volumeTrait")),
		Scheme:          mgr.GetScheme(),
		dm:              dm,
	}
	return r.SetupWithManager(mgr, log)

}

// Reconcile reconciles a VolumeTrait object
type Reconcile struct {
	client.Client
	discovery.DiscoveryClient
	dm     discoverymapper.DiscoveryMapper
	log    logr.Logger
	record event.Recorder
	Scheme *runtime.Scheme
}

//SetupWithManager to setup k8s controller.
func (r *Reconcile) SetupWithManager(mgr ctrl.Manager, log logging.Logger) error {
	name := "oam/" + strings.ToLower(oamv1alpha2.VolumeTraitKind)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&oamv1alpha2.VolumeTrait{}).
		Owns(&v1.PersistentVolumeClaim{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&source.Kind{Type: &oamv1alpha2.VolumeTrait{}}, &VolumeHandler{
			Client:     mgr.GetClient(),
			Logger:     log,
			AppsClient: clientappv1.NewForConfigOrDie(mgr.GetConfig()),
		}).
		Complete(r)
}

// Reconcile to reconcile volume trait.
// +kubebuilder:rbac:groups=core.oam.dev,resources=volumetraits,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.oam.dev,resources=volumetraits/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads,verbs=get;list;
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads/status,verbs=get;
// +kubebuilder:rbac:groups=core.oam.dev,resources=workloaddefinition,verbs=get;list;
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
func (r *Reconcile) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	mLog := r.log.WithValues("volume trait", req.NamespacedName)

	mLog.Info("Reconcile volume trait")

	var volumeTrait oamv1alpha2.VolumeTrait
	if err := r.Get(ctx, req.NamespacedName, &volumeTrait); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// find the resource object to record the event to, default is the parent appConfig.
	eventObj, err := util.LocateParentAppConfig(ctx, r.Client, &volumeTrait)
	if eventObj == nil {
		// fallback to workload itself
		mLog.Error(err, "Failed to find the parent resource", "volumeTrait", volumeTrait.Name)
		eventObj = &volumeTrait
	}

	// Fetch the workload instance this trait is referring to
	workload, err := util.FetchWorkload(ctx, r, mLog, &volumeTrait)
	if err != nil {
		r.record.Event(eventObj, event.Warning(util.ErrLocateWorkload, err))
		return util.ReconcileWaitResult, util.PatchCondition(
			ctx, r, &volumeTrait, cpv1alpha1.ReconcileError(errors.Wrap(err, util.ErrLocateWorkload)))
	}

	// Fetch the child resources list from the corresponding workload
	resources, err := util.FetchWorkloadChildResources(ctx, mLog, r, r.dm, workload)
	if err != nil {
		mLog.Error(err, "Error while fetching the workload child resources", "workload", workload.UnstructuredContent())
		r.record.Event(eventObj, event.Warning(util.ErrFetchChildResources, err))
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &volumeTrait,
			cpv1alpha1.ReconcileError(fmt.Errorf(util.ErrFetchChildResources)))
	}

	// include the workload itself if there is no child resources
	if len(resources) == 0 {
		resources = append(resources, workload)
	}

	// Scale the child resources that we know how to scale
	result, err := r.mountVolume(ctx, mLog, &volumeTrait, resources)
	if err != nil {
		r.record.Event(eventObj, event.Warning(errMountVolume, err))
		return result, err
	}

	r.record.Event(eventObj, event.Normal("Volume Trait applied",
		fmt.Sprintf("Trait `%s` successfully mount volume  to %v ",
			volumeTrait.Name, volumeTrait.Spec.VolumeList)))

	return ctrl.Result{}, util.PatchCondition(ctx, r, &volumeTrait, cpv1alpha1.ReconcileSuccess())
}

// identify child resources and add volume
func (r *Reconcile) mountVolume(ctx context.Context, mLog logr.Logger,
	volumeTrait *oamv1alpha2.VolumeTrait, resources []*unstructured.Unstructured) (ctrl.Result, error) {
	isController := false
	bod := true
	var statusResources []cpv1alpha1.TypedReference

	// find the resource object to record the event to, default is the parent appConfig.
	eventObj, err := util.LocateParentAppConfig(ctx, r.Client, volumeTrait)
	if eventObj == nil {
		// fallback to workload itself
		mLog.Error(err, "Failed to find the parent resource", "volumeTrait", volumeTrait.Name)
		eventObj = volumeTrait
	}

	// Update owner references
	ownerRef := metav1.OwnerReference{
		APIVersion:         volumeTrait.APIVersion,
		Kind:               volumeTrait.Kind,
		Name:               volumeTrait.Name,
		UID:                volumeTrait.UID,
		Controller:         &isController,
		BlockOwnerDeletion: &bod,
	}
	for _, res := range resources {

		if res.GetKind() != util.KindStatefulSet && res.GetKind() != util.KindDeployment {
			continue
		}
		resPatch := client.MergeFrom(res.DeepCopyObject())
		mLog.Info("Get the resource the trait is going to modify",
			"resource name", res.GetName(), "UID", res.GetUID())
		cpmeta.AddOwnerReference(res, ownerRef)

		spec, _, _ := unstructured.NestedFieldNoCopy(res.Object, "spec", "template", "spec")
		ovmInterface, _ := spec.(map[string]interface{})["volumes"].([]interface{})
		var oldVolumes []v1.Volume
		if len(ovmInterface) != 0 {
			b, _ := json.Marshal(ovmInterface)
			_ = json.Unmarshal(b, &oldVolumes)
		}

		var volumes []v1.Volume
		var pvcList []v1.PersistentVolumeClaim

		// volume 是列表， 因为可能有多个容器
		containers, _, _ := unstructured.NestedFieldNoCopy(res.Object, "spec", "template", "spec", "containers")

		for _, item := range volumeTrait.Spec.VolumeList {
			var volumeMounts []v1.VolumeMount
			for pathIndex, path := range item.Paths {
				pvcName := fmt.Sprintf("%s-%d-%s", res.GetName(), item.ContainerIndex, path.Name)
				volumes = append(volumes, v1.Volume{
					Name:         pvcName,
					VolumeSource: v1.VolumeSource{PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName}},
				})
				volumeMount := v1.VolumeMount{
					Name:      pvcName,
					MountPath: path.Path,
				}
				volumeMounts = append(volumeMounts, volumeMount)
				pvcList = append(pvcList, v1.PersistentVolumeClaim{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       util.KindPersistentVolumeClaim,
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      pvcName,
						Namespace: volumeTrait.Namespace,
					},
					Spec: v1.PersistentVolumeClaimSpec{
						// can't use &path.StorageClassName
						StorageClassName: &item.Paths[pathIndex].StorageClassName,
						AccessModes: []v1.PersistentVolumeAccessMode{
							v1.ReadWriteOnce,
						},
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceName(v1.ResourceStorage): resource.MustParse(path.Size),
							},
						},
					},
				})
			}
			c, _ := containers.([]interface{})[item.ContainerIndex].(map[string]interface{})
			vmInterface, _ := c["volumeMounts"].([]interface{})

			if len(vmInterface) != 0 {
				var vms []v1.VolumeMount
				b, _ := json.Marshal(vmInterface)
				_ = json.Unmarshal(b, &vms)
				for _, o := range vms {
					for _, v := range oldVolumes {
						if v.Name == o.Name && v.PersistentVolumeClaim == nil {
							volumeMounts = append(volumeMounts, o)
						}
					}
				}
			}
			c["volumeMounts"] = volumeMounts

		}
		for _, oldVm := range oldVolumes {
			if oldVm.PersistentVolumeClaim == nil {
				volumes = append(volumes, oldVm)
			}
		}
		spec.(map[string]interface{})["volumes"] = volumes

		// delete volumeMounts
		for _, c := range containers.([]interface{}) {
			c, _ := c.(map[string]interface{})
			vmInterface, _ := c["volumeMounts"].([]interface{})
			var volumeMounts []v1.VolumeMount
			if len(vmInterface) != 0 {
				var vms []v1.VolumeMount
				b, _ := json.Marshal(vmInterface)
				_ = json.Unmarshal(b, &vms)
				for _, o := range vms {
					if strings.Contains(o.Name, "oam-component-sts") {
						for _, i := range volumes {
							if i.Name == o.Name {
								volumeMounts = append(volumeMounts, o)
							}
						}
					} else {
						volumeMounts = append(volumeMounts, o)
					}

				}
				c["volumeMounts"] = volumeMounts
			}

		}

		// merge patch to modify the pvc
		var pvcUidList []types.UID

		applyOpts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(volumeTrait.GetUID())}
		for _, pvc := range pvcList {
			pvcExists := &v1.PersistentVolumeClaim{}
			if err := r.Client.Get(ctx, client.ObjectKey{Name: pvc.Name, Namespace: pvc.Namespace}, pvcExists); err == nil {
				mLog.Info("pvc has been created. Can't modify spec", "pvcName", pvc.Name)
				pvcUidList = append(pvcUidList, pvcExists.UID)
				statusResources = append(statusResources,
					cpv1alpha1.TypedReference{
						APIVersion: pvcExists.GetObjectKind().GroupVersionKind().GroupVersion().String(),
						Kind:       pvcExists.GetObjectKind().GroupVersionKind().Kind,
						Name:       pvcExists.GetName(),
						UID:        pvcExists.UID,
					},
				)
				continue
			}

			cpmeta.AddOwnerReference(&pvc, ownerRef)
			if err := r.Patch(ctx, &pvc, client.Apply, applyOpts...); err != nil {
				mLog.Error(err, "Failed to create a pvc")
				return util.ReconcileWaitResult,
					util.PatchCondition(ctx, r, volumeTrait, cpv1alpha1.ReconcileError(errors.Wrap(err, errMountVolume)))
			}
			r.record.Event(eventObj, event.Normal("PVC created",
				fmt.Sprintf("VolumeTrait `%s` successfully server side patched a pvc `%s`",
					volumeTrait.Name, pvc.Name)))

			pvcUidList = append(pvcUidList, pvc.UID)

			statusResources = append(statusResources,
				cpv1alpha1.TypedReference{
					APIVersion: pvc.GetObjectKind().GroupVersionKind().GroupVersion().String(),
					Kind:       pvc.GetObjectKind().GroupVersionKind().Kind,
					Name:       pvc.GetName(),
					UID:        pvc.UID,
				},
			)

		}

		// merge patch to modify the resource
		if err := r.Patch(ctx, res, resPatch, client.FieldOwner(volumeTrait.GetUID())); err != nil {
			mLog.Error(err, "Failed to mount volume a resource")
			return util.ReconcileWaitResult,
				util.PatchCondition(ctx, r, volumeTrait, cpv1alpha1.ReconcileError(errors.Wrap(err, errMountVolume)))
		}

		if err := r.cleanupResources(ctx, volumeTrait, pvcUidList); err != nil {
			mLog.Error(err, "Failed to clean up resources")
			r.record.Event(eventObj, event.Warning(errApplyPVC, err))
		}
		mLog.Info("Successfully patch a resource", "resource GVK", res.GroupVersionKind().String(),
			"res UID", res.GetUID(), "target volumeClaimTemplates", volumeTrait.Spec.VolumeList)

	}

	volumeTrait.Status.Resources = statusResources
	if err := r.Status().Update(ctx, volumeTrait); err != nil {
		return util.ReconcileWaitResult, err
	}

	return ctrl.Result{}, nil
}
