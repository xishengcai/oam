package volumetrait

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/klog/v2"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	cpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/pkg/errors"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientappv1 "k8s.io/client-go/kubernetes/typed/apps/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/source"

	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam/util"
)

// Reconcile error strings.
const (
	errMountVolume = "cannot mount volume"
	waitTime       = time.Second * 60
	HostPath       = "HostPath"
	StorageClass   = "StorageClass"
)

// Setup adds a controller that reconciles ContainerizedWorkload.
func Setup(mgr ctrl.Manager, _ controller.Args) error {
	name := "oam/" + strings.ToLower(oamv1alpha2.VolumeTraitKind)
	dm, err := discoverymapper.New(mgr.GetConfig())
	if err != nil {
		return err
	}

	clientSet := kubernetes.NewForConfigOrDie(mgr.GetConfig())
	r := &Reconcile{
		clientSet:       clientSet,
		Client:          mgr.GetClient(),
		DiscoveryClient: *discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig()),
		record:          event.NewAPIRecorder(mgr.GetEventRecorderFor("volumeTrait")),
		Scheme:          mgr.GetScheme(),
		dm:              dm,
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&oamv1alpha2.VolumeTrait{}).
		Watches(
			&source.Kind{
				Type: &oamv1alpha2.VolumeTrait{},
			},
			&VolumeHandler{
				ClientSet:  r.clientSet,
				Client:     mgr.GetClient(),
				dm:         r.dm,
				AppsClient: clientappv1.NewForConfigOrDie(mgr.GetConfig()),
			}).
		Complete(r)
}

// Reconcile reconciles a VolumeTrait object
type Reconcile struct {
	clientSet *kubernetes.Clientset
	client.Client
	discovery.DiscoveryClient
	dm     discoverymapper.DiscoveryMapper
	record event.Recorder
	Scheme *runtime.Scheme
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

	klog.Info("Reconcile volume trait")

	var volumeTrait oamv1alpha2.VolumeTrait
	if err := r.Get(ctx, req.NamespacedName, &volumeTrait); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// find the resource object to record the event to, default is the parent appConfig.
	eventObj, err := util.LocateParentAppConfig(ctx, r.Client, &volumeTrait)
	if eventObj == nil {
		// fallback to workload itself
		klog.ErrorS(err, "Failed to find the parent resource", "volumeTrait", volumeTrait.Name)
		eventObj = &volumeTrait
	}

	// Fetch the workload instance this trait is referring to
	workload, err := util.FetchWorkload(ctx, r, &volumeTrait)
	if err != nil {
		r.record.Event(eventObj, event.Warning(util.ErrLocateWorkload, err))
		return util.ReconcileWaitResult, util.PatchCondition(
			ctx, r, &volumeTrait, cpv1alpha1.ReconcileError(errors.Wrap(err, util.ErrLocateWorkload)))
	}

	klog.V(1).Info("workload-name", workload.GetName(), "workload-uid", workload.GetUID())
	// Fetch the child resources list from the corresponding workload
	resources, err := util.FetchWorkloadChildResources(ctx, r, r.dm, workload)
	if err != nil {
		klog.ErrorS(err, "Error while fetching the workload child resources", "workload", workload.UnstructuredContent())
		r.record.Event(eventObj, event.Warning(util.ErrFetchChildResources, err))
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &volumeTrait,
			cpv1alpha1.ReconcileError(fmt.Errorf(util.ErrFetchChildResources)))
	}
	result, err := r.mountVolume(ctx, &volumeTrait, resources)
	if err != nil {
		r.record.Event(eventObj, event.Warning(errMountVolume, err))
		return result, err
	}

	r.record.Event(eventObj, event.Normal("Volume Trait applied",
		fmt.Sprintf("Trait `%s` successfully mount volume  to %v ",
			volumeTrait.Name, volumeTrait.Spec.VolumeList)))

	return ctrl.Result{RequeueAfter: waitTime}, util.PatchCondition(ctx, r, &volumeTrait, cpv1alpha1.ReconcileSuccess())
}

// mountVolume find child resources and add mount volume
func (r *Reconcile) mountVolume(ctx context.Context, volumeTrait *oamv1alpha2.VolumeTrait, resources []*unstructured.Unstructured) (ctrl.Result, error) {
	var statusResources []cpv1alpha1.TypedReference

	// find the resource object to record the event to, default is the parent appConfig.
	appConfig, err := util.LocateParentAppConfig(ctx, r.Client, volumeTrait)
	if appConfig == nil {
		klog.ErrorS(err, "Failed to find the parent resource", "volumeTrait", volumeTrait.Name)
		appConfig = volumeTrait
	}

	// Update owner references
	ownerRef := metav1.OwnerReference{
		APIVersion:         volumeTrait.APIVersion,
		Kind:               volumeTrait.Kind,
		Name:               volumeTrait.Name,
		UID:                volumeTrait.UID,
		Controller:         newTrue(false),
		BlockOwnerDeletion: newTrue(true),
	}

	for _, res := range resources {
		if res.GetKind() != util.KindStatefulSet && res.GetKind() != util.KindDeployment {
			continue
		}
		resPatch := client.MergeFrom(res.DeepCopyObject())
		cpmeta.AddOwnerReference(res, ownerRef)
		spec, _, _ := unstructured.NestedFieldNoCopy(res.Object, "spec", "template", "spec")
		oldVolumes := getVolumesFromSpec(spec)
		volumes := make(map[string]v1.Volume) // 重新构建 volumes

		// volume 是列表， 因为可能有多个容器
		// 从 sts or deploy中找出容器
		containers, _, _ := unstructured.NestedFieldNoCopy(res.Object, "spec", "template", "spec", "containers")
		initContainers, _, _ := unstructured.NestedFieldNoCopy(res.Object, "spec", "template", "spec", "initContainers")

		// 遍历挂载特性中的VolumeList字段
		for _, item := range volumeTrait.Spec.VolumeList {
			var volumeMounts []v1.VolumeMount
			for _, path := range item.Paths {
				volumeMount := v1.VolumeMount{
					Name:      path.PersistentVolumeClaim,
					MountPath: path.Path,
				}
				volumeMounts = append(volumeMounts, volumeMount)
				var volumeClaim oamv1alpha2.VolumeClaim
				if err := r.Get(ctx, client.ObjectKey{Name: path.PersistentVolumeClaim, Namespace: appConfig.GetNamespace()}, &volumeClaim); err != nil {
					return ctrl.Result{}, client.IgnoreNotFound(err)
				}

				switch volumeClaim.Spec.Type {
				case HostPath:
					volumes[path.PersistentVolumeClaim] = v1.Volume{
						Name: path.PersistentVolumeClaim,
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: volumeClaim.Spec.HostPath,
							},
						},
					}
				case StorageClass:
					volumes[path.PersistentVolumeClaim] = v1.Volume{
						Name:         path.PersistentVolumeClaim,
						VolumeSource: v1.VolumeSource{PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{ClaimName: path.PersistentVolumeClaim}},
					}
				}
			}
			if item.ContainerIndex > len(containers.([]interface{}))-1 {
				return ctrl.Result{}, fmt.Errorf("container Index out of range")
			}
			container, _ := containers.([]interface{})[item.ContainerIndex].(map[string]interface{})
			if item.IsInitContainer {
				initContainer, _ := initContainers.([]interface{})[item.ContainerIndex].(map[string]interface{})
				oldVolumeMounts := getVolumeMountsFromContainer(initContainer)
				// 找出非pvc,hostPath 的volumeMounts
				volumeMounts = append(volumeMounts, findConfigVolumes(oldVolumes, oldVolumeMounts)...)
				initContainer["volumeMounts"] = volumeMounts
			} else {
				oldVolumeMounts := getVolumeMountsFromContainer(container)
				// 找出非pvc,hostPath 的volumeMounts
				volumeMounts = append(volumeMounts, findConfigVolumes(oldVolumes, oldVolumeMounts)...)
				container["volumeMounts"] = volumeMounts
			}
		}
		// 继续构建volumes， 遍历old volumes， 找到pvc == nil的，追加到数组中
		volumeArray := mergeVolumes(oldVolumes, volumes)
		spec.(map[string]interface{})["volumes"] = volumeArray

		// merge patch to modify the resource
		if err := r.Patch(ctx, res, resPatch, client.FieldOwner(volumeTrait.GetUID())); err != nil {
			klog.ErrorS(err, "Failed to mount volume a resource")
			return util.ReconcileWaitResult, err
		}

		klog.InfoS("Successfully patch a resource", "resource GVK", res.GroupVersionKind().String(),
			"res UID", res.GetUID(), "target volumeClaimTemplates", volumeTrait.Spec.VolumeList)
	}

	volumeTrait.Status.Resources = statusResources
	volumeTraitPatch := client.MergeFrom(volumeTrait.DeepCopyObject())
	if err := r.Status().Patch(ctx, volumeTrait, volumeTraitPatch); err != nil {
		klog.ErrorS(err, "failed to update volumeTrait")
		return util.ReconcileWaitResult, err
	}

	return ctrl.Result{}, nil
}

func getVolumeMountsFromContainer(container map[string]interface{}) (vms []v1.VolumeMount) {
	vmInterface, ok := container["volumeMounts"]
	if !ok {
		return
	}
	b, err := json.Marshal(vmInterface)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &vms)
	return
}

func getVolumesFromSpec(spec interface{}) (vls []v1.Volume) {
	vlsInterface, ok := spec.(map[string]interface{})["volumes"].([]interface{})
	if !ok {
		return
	}
	b, err := json.Marshal(vlsInterface)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &vls)
	return
}

// findConfigVolumes 找出非pvc,hostPath 的volumeMounts
func findConfigVolumes(vls []v1.Volume, vms []v1.VolumeMount) (noPvcVolumeMounts []v1.VolumeMount) {
	for _, x := range vls {
		if x.PersistentVolumeClaim != nil || x.HostPath != nil {
			continue
		}
		for _, j := range vms {
			if j.Name == x.Name {
				noPvcVolumeMounts = append(noPvcVolumeMounts, j)
			}
		}
	}
	return
}

// mergeVolumes merge configMap volumes to new volumes
func mergeVolumes(olds []v1.Volume, news map[string]v1.Volume) []v1.Volume {
	for _, x := range olds {
		if x.PersistentVolumeClaim != nil || x.HostPath != nil {
			continue
		}
		news[x.Name] = x
	}
	result := make([]v1.Volume, 0)
	for _, v := range news {
		result = append(result, v)
	}
	return result
}

func newTrue(b bool) *bool {
	return &b
}
