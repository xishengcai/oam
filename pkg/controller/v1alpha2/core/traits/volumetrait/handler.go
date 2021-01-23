package volumetrait

import (
	"context"
	"fmt"
	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/xishengcai/oam/apis/core/v1alpha2"
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
	Logger     logging.Logger
}

func (c *VolumeHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	return
}

func (c *VolumeHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	//newV := evt.ObjectNew.(*v1alpha2.VolumeTrait)
	//oldV := evt.ObjectOld.(*v1alpha2.VolumeTrait)
	//
	//newPvcList := newV.Status.Resources
	//oldPvcList := oldV.Status.Resources
	//
	//for _, o := range oldPvcList{
	//	if ok := findElem(newPvcList,o);!ok{
	//		err := c.ClientSet.CoreV1().PersistentVolumeClaims(newV.Namespace).
	//			Delete(context.Background(), o.Name, metav1.DeleteOptions{})
	//		if err != nil {
	//			klog.Errorf("delete pvc: %s, err %v", o.Name, err)
	//		}
	//		c.Logger.Info("remove old pvc", "pvcName", o.Name)
	//	}
	//}
	return
}

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

	// delete pvc that create by volumeTrait
	for _, resource := range volumeTrait.Status.Resources {
		err := c.ClientSet.CoreV1().PersistentVolumeClaims(volumeTrait.Namespace).
			Delete(context.Background(), resource.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("pvc namespace: %s, name: %s delete fialed, %v",
				volumeTrait.Namespace, resource.Name, err)
		}
		c.Logger.Info("remove pvc success", "volumeTrait", object.GetName(), "pvcName", resource.Name)
	}
	return nil
}

func findElem(array []v1alpha1.TypedReference,elem v1alpha1.TypedReference) bool{
	for _, a := range array{
		if a.UID == elem.UID{
			return true
		}
	}
	return false
}
