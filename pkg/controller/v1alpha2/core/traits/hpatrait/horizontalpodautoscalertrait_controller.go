package hpatrait

import (
	"context"
	"fmt"
	"strings"

	cpv1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/controller"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"github.com/xishengcai/oam/pkg/oam/util"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconcile error strings.
const (
	errLocateAvailableResouces = "cannot find available resources"
	errApplyHPA                = "cannot apply the HPA"
	errGCHPA                   = "cannot clean up HPA"
)

// Setup adds a controller that reconciles ContainerizedWorkload.
func Setup(mgr ctrl.Manager, args controller.Args, log logging.Logger) error {
	dm, err := discoverymapper.New(mgr.GetConfig())
	if err != nil {
		return err
	}
	r := Reconciler{
		Client:          mgr.GetClient(),
		DiscoveryClient: *discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig()),
		log:             ctrl.Log.WithName("HpaTrait"),
		record:          event.NewAPIRecorder(mgr.GetEventRecorderFor("HpaTrait")),
		Scheme:          mgr.GetScheme(),
		dm:              dm,
	}
	return r.SetupWithManager(mgr)
}

// Reconciler reconciles a ManualScalarTrait object
type Reconciler struct {
	client.Client
	discovery.DiscoveryClient
	dm     discoverymapper.DiscoveryMapper
	log    logr.Logger
	record event.Recorder
	Scheme *runtime.Scheme
}

// SetupWithManager to setup k8s controller.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	name := "oam/" + strings.ToLower(oamv1alpha2.HorizontalPodAutoscalerTraitKind)
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&oamv1alpha2.HorizontalPodAutoscalerTrait{}).
		Complete(r)
}

// Reconcile  reconcile trait hpa
// +kubebuilder:rbac:groups=core.oam.dev,resources=horizontalpodautoscalertraits,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.oam.dev,resources=horizontalpodautoscalertraits/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.oam.dev,resources=workloaddefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads/status,verbs=get;
// +kubebuilder:rbac:groups=core.oam.dev,resources=containerizedworkloads,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	hpaLog := r.log.WithValues("namespace", req.NamespacedName)
	hpaLog.Info("Reconcile HorizontalPodAutoscalerTrait")

	var hpaTrait oamv1alpha2.HorizontalPodAutoscalerTrait
	if err := r.Get(ctx, req.NamespacedName, &hpaTrait); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	hpaLog.Info("Get the HPA trait", "Spec: ", hpaTrait.Spec)

	// Fetch the workload instance this trait is referring to
	workload, err := util.FetchWorkload(ctx, r, r.log, &hpaTrait)
	if err != nil {
		return util.ReconcileWaitResult, err
	}

	resources, err := util.FetchWorkloadChildResources(ctx, hpaLog, r, r.dm, workload)
	if err != nil {
		hpaLog.Error(err, "Error while fetching the workload child resources", "workload", workload.UnstructuredContent())
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &hpaTrait,
			cpv1alpha1.ReconcileError(fmt.Errorf(util.ErrFetchChildResources)))
	}

	// render HPAs
	// it's possible that one component contains more than one deployment or statefulset.
	// then each deployment or statefulset deserves one HPA.
	hpas, err := r.renderHPA(&hpaTrait, resources)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(hpas) == 0 {
		r.log.Info("Cannot get any HPA-applicable resources")
		return util.ReconcileWaitResult, fmt.Errorf(errLocateAvailableResouces)
	}

	// to record UID of newly created HPAs
	hpaUIDs := make([]types.UID, 0)
	hpaTrait.Status.Resources = nil

	// server side apply HPAs
	for _, hpa := range hpas {
		applyOpts := []client.PatchOption{client.ForceOwnership, client.FieldOwner(hpa.Name)}
		if err := r.Patch(ctx, hpa, client.Apply, applyOpts...); err != nil {
			r.log.Error(err, "Failed to apply a HPA", "Target HPA spec", hpa, "Total HPA count", len(hpas))
			return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &hpaTrait, cpv1alpha1.ReconcileError(errors.Wrap(err, errApplyHPA)))
		}
		r.log.Info("Successfully applied a HPA", "UID", hpa.UID)

		// record the status of newly created HPA
		hpaTrait.Status.Resources = append(hpaTrait.Status.Resources, cpv1alpha1.TypedReference{
			APIVersion: hpa.GetObjectKind().GroupVersionKind().GroupVersion().String(),
			Kind:       hpa.GetObjectKind().GroupVersionKind().Kind,
			Name:       hpa.GetName(),
			UID:        hpa.GetUID(),
		})
		hpaUIDs = append(hpaUIDs, hpa.GetUID())
		if err := r.Status().Update(ctx, &hpaTrait); err != nil {
			r.log.Error(err, "Failed update HPA_trait status")
			return util.ReconcileWaitResult, err
		}
		r.log.Info("Successfully update HPA_trait status", "UID", hpaTrait.GetUID())
	}

	// delete existing HPAs referred to this HPAtrait
	if err := r.cleanUpLegacyHPAs(ctx, &hpaTrait, hpaUIDs); err != nil {
		return util.ReconcileWaitResult, util.PatchCondition(ctx, r, &hpaTrait, cpv1alpha1.ReconcileError(errors.Wrap(err, errGCHPA)))
	}

	return ctrl.Result{}, util.PatchCondition(ctx, r, &hpaTrait, cpv1alpha1.ReconcileSuccess())
}
