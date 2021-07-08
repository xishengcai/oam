package volumetrait

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"github.com/xishengcai/oam/pkg/oam/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientappv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// VolumeHandler will watch volume change and delete pvc automatically.
type VolumeHandler struct {
	ClientSet  *kubernetes.Clientset
	Client     client.Client
	AppsClient clientappv1.AppsV1Interface
	Logger     logr.Logger
	dm         discoverymapper.DiscoveryMapper
}

// Create  implements EventHandler
func (c *VolumeHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {}

// Update  implements EventHandler
func (c *VolumeHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {}

// Delete implements EventHandler
func (c *VolumeHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	if err := c.removeVolumes(evt.Meta); err != nil {
		c.Logger.Info("remove pvc failed", "volumeTrait", evt.Meta.GetName())
	}
}

// Generic implements EventHandler
func (c *VolumeHandler) Generic(_ event.GenericEvent, _ workqueue.RateLimitingInterface) {
	// Generic is called in response to an event of an unknown type or a synthetic event triggered as a cron or
	// external trigger request - e.g. reconcile Autoscaling, or a Webhook.
	// so we need to do nothing here.
}

func (c *VolumeHandler) removeVolumes(object metav1.Object) error {
	volumeTrait, ok := object.(*v1alpha2.VolumeTrait)
	if !ok {
		return nil
	}
	ctx := context.Background()
	// Fetch the workload instance this trait is referring to
	workload, err := util.FetchWorkload(ctx, c.Client, c.Logger, volumeTrait)
	if err != nil {
		return err
	}

	// Fetch the child resources list from the corresponding workload
	resources, err := util.FetchWorkloadChildResources(ctx, c.Logger, c.Client, c.dm, workload)
	if err != nil {
		return err
	}

	for _, resource := range resources {
		if resource.GetKind() == util.KindStatefulSet || resource.GetKind() == util.KindDeployment {
			err := c.Client.Delete(ctx, resource)
			if err != nil {
				return err
			}
			c.Logger.Info("volumeTrait deleted, and delete mountResource",
				"kind", resource.GetKind(),
				"name", resource.GetName(),
				"namespace", resource.GetNamespace())
		}
	}
	// delete pvc that create by volumeTrait
	for _, resource := range volumeTrait.Status.Resources {
		err := c.ClientSet.CoreV1().PersistentVolumeClaims(volumeTrait.Namespace).
			Delete(ctx, resource.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
		c.Logger.Info("remove pvc success",
			"volumeTrait", object.GetName(),
			"pvcName", resource.Name)
	}
	return nil
}
