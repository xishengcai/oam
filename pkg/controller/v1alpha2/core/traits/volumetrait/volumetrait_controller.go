package volumetrait

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	cpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientappv1 "k8s.io/client-go/kubernetes/typed/apps/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam/util"
)

// Reconcile error strings.
const (
	errMountVolume = "cannot scale the resource"
	errApplyPVC    = "cannot apply the pvc"
	waitTime       = time.Second * 60
)

// Setup adds a controller that reconciles ContainerizedWorkload.
func Setup(mgr ctrl.Manager, _ controller.Args, _ logging.Logger) error {
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
		log:             ctrl.Log.WithName("VolumeTrait"),
		record:          event.NewAPIRecorder(mgr.GetEventRecorderFor("volumeTrait")),
		Scheme:          mgr.GetScheme(),
		dm:              dm,
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&oamv1alpha2.VolumeTrait{}).
		Owns(&v1.PersistentVolumeClaim{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&source.Kind{Type: &oamv1alpha2.VolumeTrait{}}, &VolumeHandler{
			ClientSet:  r.clientSet,
			Client:     mgr.GetClient(),
			Logger:     ctrl.Log.WithName("VolumeHandler"),
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
	log    logr.Logger
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
	mLog := r.log.WithValues("namespacedName", req.NamespacedName)

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

	mLog.V(4).Info("workload-name", workload.GetName(), "workload-uid", workload.GetUID())
	// Fetch the child resources list from the corresponding workload
	resources, err := util.FetchWorkloadChildResources(ctx, mLog, r, r.dm, workload)
	if err != nil {
		mLog.Error(err, "Error while fetching the workload child resources", "workload", workload.UnstructuredContent())
		r.record.Event(eventObj, event.Warning(util.ErrFetchChildResources, err))
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &volumeTrait,
			cpv1alpha1.ReconcileError(fmt.Errorf(util.ErrFetchChildResources)))
	}
	result, err := r.mountVolume(ctx, mLog, &volumeTrait, resources)
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
func (r *Reconcile) mountVolume(ctx context.Context, mLog logr.Logger,
	volumeTrait *oamv1alpha2.VolumeTrait, resources []*unstructured.Unstructured) (ctrl.Result, error) {
	isController := false
	bod := true
	var statusResources []cpv1alpha1.TypedReference

	// find the resource object to record the event to, default is the parent appConfig.
	appConfig, err := util.LocateParentAppConfig(ctx, r.Client, volumeTrait)
	if appConfig == nil {
		mLog.Error(err, "Failed to find the parent resource", "volumeTrait", volumeTrait.Name)
		appConfig = volumeTrait
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
		cpmeta.AddOwnerReference(res, ownerRef)
		spec, _, _ := unstructured.NestedFieldNoCopy(res.Object, "spec", "template", "spec")
		oldVolumes := getVolumesFromSpec(spec)

		var volumes []v1.Volume                 // 重新构建 volumes
		var pvcList []*v1.PersistentVolumeClaim // 重新构建 pvc

		// volume 是列表， 因为可能有多个容器
		// 从 sts or deploy中找出容器
		containers, _, _ := unstructured.NestedFieldNoCopy(res.Object, "spec", "template", "spec", "containers")

		// 遍历挂载特性中的VolumeList字段
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
				pvcList = append(pvcList, &v1.PersistentVolumeClaim{
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
								v1.ResourceStorage: resource.MustParse(path.Size),
							},
						},
					},
				})
			}
			if item.ContainerIndex > len(containers.([]interface{}))-1 {
				return ctrl.Result{}, fmt.Errorf("container Index out of range")
			}
			c, _ := containers.([]interface{})[item.ContainerIndex].(map[string]interface{})
			oldVolumeMounts := getVolumeMountsFromContainer(c)

			// 找出非pvc的volumeMounts
			volumeMounts = append(volumeMounts, getHasPvcVolumeMounts(oldVolumes, oldVolumeMounts)...)

			c["volumeMounts"] = volumeMounts
		}
		// 继续构建volumes， 遍历old volumes， 找到pvc == nil的，追加到数组中
		volumes = mergeVolumes(oldVolumes, volumes)
		spec.(map[string]interface{})["volumes"] = volumes

		// 多容器时， 对每个容器遍历，删除之前有pvc，但是现在没有的
		for _, ci := range containers.([]interface{}) {
			c := ci.(map[string]interface{})
			vms := getVolumeMountsFromContainer(c)
			newVms := filterVolumeMounts(volumes, vms)
			c["volumeMounts"] = newVms
		}

		var pvcNameList []string
		for _, pvc := range pvcList {
			pvcTemp, err := r.clientSet.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(ctx, pvc.Name, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					pvcTemp, err = r.clientSet.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
					if err != nil {
						mLog.Error(err, "Create pvc failed", "pvcName", pvc.Name)
						return util.ReconcileWaitResult, err
					}
				}
			}
			r.record.Event(appConfig, event.Normal("PVC created",
				fmt.Sprintf("VolumeTrait `%s` successfully server side create a pvc `%s`",
					volumeTrait.Name, pvc.Name)))
			pvcNameList = append(pvcNameList, pvc.Name)
			statusResources = append(statusResources,
				cpv1alpha1.TypedReference{
					APIVersion: pvcTemp.GetObjectKind().GroupVersionKind().GroupVersion().String(),
					Kind:       pvcTemp.GetObjectKind().GroupVersionKind().Kind,
					Name:       pvcTemp.GetName(),
					UID:        pvcTemp.UID,
				},
			)
		}

		// merge patch to modify the resource
		if err := r.Patch(ctx, res, resPatch, client.FieldOwner(volumeTrait.GetUID())); err != nil {
			mLog.Error(err, "Failed to mount volume a resource")
			return util.ReconcileWaitResult, err
		}

		if err := r.cleanupResources(ctx, volumeTrait, pvcNameList); err != nil {
			mLog.Error(err, "Failed to clean up resources")
			r.record.Event(appConfig, event.Warning(errApplyPVC, err))
			return util.ReconcileWaitResult, err
		}
		mLog.Info("Successfully patch a resource", "resource GVK", res.GroupVersionKind().String(),
			"res UID", res.GetUID(), "target volumeClaimTemplates", volumeTrait.Spec.VolumeList)
	}

	volumeTrait.Status.Resources = statusResources
	volumeTraitPatch := client.MergeFrom(volumeTrait.DeepCopyObject())
	if err := r.Status().Patch(ctx, volumeTrait, volumeTraitPatch); err != nil {
		mLog.Error(err, "failed to update volumeTrait")
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

func getHasPvcVolumeMounts(vls []v1.Volume, vms []v1.VolumeMount) (noPvcVolumeMounts []v1.VolumeMount) {
	for _, x := range vls {
		if x.PersistentVolumeClaim != nil {
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

func mergeVolumes(olds, news []v1.Volume) []v1.Volume {
	for _, x := range olds {
		if x.PersistentVolumeClaim != nil {
			continue
		}
		news = append(news, x)
	}
	return news
}

func filterVolumeMounts(vlms []v1.Volume, vms []v1.VolumeMount) (newVms []v1.VolumeMount) {
	for _, x := range vms {
		for _, j := range vlms {
			if x.Name == j.Name {
				newVms = append(newVms, x)
			}
		}
	}
	return
}
