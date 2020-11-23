package volumetrait

import (
	"context"
	"fmt"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/xishengcai/oam/pkg/oam/util"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientappv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// VolumeHandler will watch volume change and delete pvc automatically.
type VolumeHandler struct {
	Client     client.Client
	AppsClient clientappv1.AppsV1Interface
	Logger     logging.Logger
}

func (c *VolumeHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	return
}

func (c *VolumeHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	return
}

// Delete implements EventHandler
func (c *VolumeHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	if err := c.removeVolumes(evt.Meta); err != nil {
		klog.Error("remove volumes failed, ", err)
	}
}

// Generic implements EventHandler
func (c *VolumeHandler) Generic(_ event.GenericEvent, _ workqueue.RateLimitingInterface) {
	// Generic is called in response to an event of an unknown type or a synthetic event triggered as a cron or
	// external trigger request - e.g. reconcile Autoscaling, or a Webhook.
	// so we need to do nothing here.
}

func (c *VolumeHandler) removeVolumes(object metav1.Object) error {
	var pvc v1.PersistentVolumeClaimList
	err := c.Client.List(context.Background(), &pvc)
	if err != nil {
		c.Logger.Info(fmt.Sprintf("error list all  %v", err))
		return nil
	}
	labels := object.GetLabels()
	if labels == nil {
		return fmt.Errorf("volumeTrait not found labels")
	}
	kind := labels[util.LabelKeyChildResource]
	name := labels[util.LabelKeyChildResourceName]

	if kind == util.KindDeployment {
		res := &appsv1.Deployment{}
		if err = c.Client.Get(context.Background(), client.ObjectKey{Name: name, Namespace: object.GetNamespace()}, res); err != nil {
			return err
		}
		volumes := res.Spec.Template.Spec.Volumes
		var wantDeletePathName []string
		for i := len(volumes) - 1; i >= 0; i-- {
			if volumes[i].PersistentVolumeClaim != nil {
				wantDeletePathName = append(wantDeletePathName, volumes[i].Name)
				volumes = append(volumes[:i], volumes[i+1:]...)

			}
		}
		res.Spec.Template.Spec.Volumes = volumes

		for index, c := range res.Spec.Template.Spec.Containers {
			volumeMounts := c.VolumeMounts
			for i := len(volumeMounts) - 1; i >= 0; i-- {
				for _, wd := range wantDeletePathName {
					if volumeMounts[i].Name == wd {
						volumeMounts = append(volumeMounts[:i], volumeMounts[i+1:]...)
						break
					}
				}
			}
			res.Spec.Template.Spec.Containers[index].VolumeMounts = volumeMounts
		}

		res.ResourceVersion = ""
		if err := c.Client.Update(context.Background(), res); err != nil {
			return err
		}

	} else if kind == util.KindStatefulSet {
		// TODO: 需要优化，代码重复
		res := &appsv1.StatefulSet{}
		if err = c.Client.Get(context.Background(), client.ObjectKey{Name: name, Namespace: object.GetNamespace()}, res); err != nil {
			return err
		}
		volumes := res.Spec.Template.Spec.Volumes
		var wantDeletePathName []string
		for i := len(volumes) - 1; i >= 0; i-- {
			if volumes[i].PersistentVolumeClaim != nil {
				wantDeletePathName = append(wantDeletePathName, volumes[i].Name)
				volumes = append(volumes[:i], volumes[i+1:]...)

			}
		}
		res.Spec.Template.Spec.Volumes = volumes

		for index, c := range res.Spec.Template.Spec.Containers {
			volumeMounts := c.VolumeMounts
			for i := len(volumeMounts) - 1; i >= 0; i-- {
				for _, wd := range wantDeletePathName {
					if volumeMounts[i].Name == wd {
						volumeMounts = append(volumeMounts[:i], volumeMounts[i+1:]...)
						break
					}
				}
			}
			res.Spec.Template.Spec.Containers[index].VolumeMounts = volumeMounts
		}

		res.ResourceVersion = ""
		if err := c.Client.Update(context.Background(), res); err != nil {
			return err
		}
	}

	return nil
}
