package volumeclaimtrait

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"github.com/xishengcai/oam/pkg/oam/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientappv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	waitTime         = time.Second * 60
	reconcileTimeout = time.Second * 60
	HostPath         = "HostPath"
	StorageClass     = "StorageClass"
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
		For(&oamv1alpha2.VolumeClaim{}).
		Owns(&v1.PersistentVolumeClaim{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&source.Kind{
				Type: &oamv1alpha2.VolumeClaim{},
			},
			&VolumeClaimHandler{
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
	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()

	var volumeClaim oamv1alpha2.VolumeClaim
	if err := r.Get(ctx, req.NamespacedName, &volumeClaim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var pvc *v1.PersistentVolumeClaim
	var pv *v1.PersistentVolume

	objectMeta := metav1.ObjectMeta{
		Name:      volumeClaim.Name,
		Namespace: volumeClaim.Namespace,
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion: volumeClaim.APIVersion,
				Kind:       volumeClaim.Kind,
				Name:       volumeClaim.Name,
				UID:        volumeClaim.UID,
			},
		},
	}
	// generate pvc
	switch volumeClaim.Spec.Type {
	case HostPath:
		pvc = &v1.PersistentVolumeClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       util.KindPersistentVolumeClaim,
			},
			ObjectMeta: objectMeta,
			Spec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{
					v1.ReadWriteOnce,
				},
				VolumeName: volumeClaim.Name,
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}

		hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate
		pv = &v1.PersistentVolume{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       util.KindPersistentVolume,
			},
			ObjectMeta: objectMeta,
			Spec: v1.PersistentVolumeSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{
					v1.ReadWriteOnce,
				},
				PersistentVolumeSource: v1.PersistentVolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: volumeClaim.Spec.HostPath,
						Type: &hostPathDirectoryOrCreate,
					},
				},
				Capacity: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		}
	case StorageClass:
		pvc = &v1.PersistentVolumeClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       util.KindPersistentVolumeClaim,
			},
			ObjectMeta: objectMeta,
			Spec: v1.PersistentVolumeClaimSpec{
				// can't use &path.StorageClassName
				StorageClassName: &volumeClaim.Spec.StorageClassName,
				AccessModes: []v1.PersistentVolumeAccessMode{
					v1.ReadWriteOnce,
				},
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: resource.MustParse(volumeClaim.Spec.Size),
					},
				},
			},
		}
	default:
		return ctrl.Result{RequeueAfter: waitTime}, fmt.Errorf("volumeClaim type notSupport %s", volumeClaim.Spec.Type)
	}

	// apply pvc
	_, err := r.clientSet.CoreV1().PersistentVolumeClaims(volumeClaim.Namespace).Get(ctx, volumeClaim.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = r.clientSet.CoreV1().PersistentVolumeClaims(volumeClaim.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
			if err != nil {
				return ctrl.Result{RequeueAfter: waitTime}, err
			}
		}
	}

	// if type == hostPath, apply
	_, err = r.clientSet.CoreV1().PersistentVolumes().Get(ctx, volumeClaim.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = r.clientSet.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{})
			if err != nil {
				return ctrl.Result{RequeueAfter: waitTime}, err
			}
		}
	}

	return ctrl.Result{RequeueAfter: waitTime}, util.PatchCondition(ctx, r, &volumeClaim, cpv1alpha1.ReconcileSuccess())
}
