package canarytrait

import (
	"context"
	"strings"

	"k8s.io/klog/v2"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"github.com/xishengcai/oam/pkg/oam/util"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	istioclient "istio.io/client-go/pkg/clientset/versioned"
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
		record:          event.NewAPIRecorder(mgr.GetEventRecorderFor("CanaryTrait")),
		Scheme:          mgr.GetScheme(),
		dm:              dm,
		IstioClient:     istioclient.NewForConfigOrDie(mgr.GetConfig()),
	}
	return r.SetupWithManager(mgr)
}

// Reconciler reconciles a ManualScalarTrait object
type Reconciler struct {
	client.Client
	discovery.DiscoveryClient
	dm          discoverymapper.DiscoveryMapper
	IstioClient *istioclient.Clientset
	record      event.Recorder
	Scheme      *runtime.Scheme
}

// SetupWithManager to setup k8s controller.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	name := "oam/" + strings.ToLower(oamv1alpha2.HorizontalPodAutoscalerTraitKind)
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&oamv1alpha2.CanaryTrait{}).
		Complete(r)
}

// Reconcile reconcile canary traits
// +kubebuilder:rbac:groups=core.oam.dev,resources=canarytraits,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.oam.dev,resources=canarytraits/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.oam.dev,resources=workloaddefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling,resources=canarytraits,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	klog.Info("Reconcile canaryLogTrait")

	var canaryTrait oamv1alpha2.CanaryTrait
	if err := r.Get(ctx, req.NamespacedName, &canaryTrait); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	klog.InfoS("Get the canary trait", "Spec", canaryTrait.Spec)
	eventObj, err := util.LocateParentAppConfig(ctx, r.Client, &canaryTrait)
	if eventObj == nil {
		klog.ErrorS(err, "Failed to find the parent resource", "canaryTrait", canaryTrait.Name)
		return ctrl.Result{}, nil
	}

	_, err = r.renderDestinationRule(canaryTrait)
	if err != nil {
		klog.ErrorS(err, "Error renderDestinationRule", "canary", err)
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &canaryTrait,
			cpv1alpha1.ReconcileError(err))
	}

	_, err = r.renderVirtualService(canaryTrait)
	if err != nil {
		klog.ErrorS(err, "Error renderVirtualService", "canary", err)
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &canaryTrait,
			cpv1alpha1.ReconcileError(err))
	}
	return ctrl.Result{}, util.PatchCondition(ctx, r, &canaryTrait, cpv1alpha1.ReconcileSuccess())
}
