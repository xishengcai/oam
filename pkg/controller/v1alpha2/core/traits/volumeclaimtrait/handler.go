package volumeclaimtrait

import (
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"k8s.io/client-go/kubernetes"
	clientappv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// VolumeClaimHandler will watch volume claim change and delete pvc automatically.
type VolumeClaimHandler struct {
	ClientSet  *kubernetes.Clientset
	Client     client.Client
	AppsClient clientappv1.AppsV1Interface
	dm         discoverymapper.DiscoveryMapper
}

func (v VolumeClaimHandler) Create(event event.CreateEvent, limitingInterface workqueue.RateLimitingInterface) {

}

func (v VolumeClaimHandler) Update(event event.UpdateEvent, limitingInterface workqueue.RateLimitingInterface) {

}

func (v VolumeClaimHandler) Delete(event event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {

}

func (v VolumeClaimHandler) Generic(event event.GenericEvent, limitingInterface workqueue.RateLimitingInterface) {
	// Generic is called in response to an event of an unknown type or a synthetic event triggered as a cron or
	// external trigger request - e.g. reconcile Autoscaling, or a Webhook.
	// so we need to do nothing here.
}
