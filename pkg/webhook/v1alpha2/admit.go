package v1alpha2

import (
	"github.com/xishengcai/oam/pkg/webhook/v1alpha2/applicationconfiguration"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Add will be called in main and register all validation handlers
func Add(mgr manager.Manager) {
	server := mgr.GetWebhookServer()
	applicationconfiguration.Register(server)
}
