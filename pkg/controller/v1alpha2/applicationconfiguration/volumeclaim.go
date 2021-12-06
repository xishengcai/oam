package applicationconfiguration

import (
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type VolumeHandler struct {
	Client    client.Client
	dm        discoverymapper.DiscoveryMapper
	clientSet *kubernetes.Clientset
}
