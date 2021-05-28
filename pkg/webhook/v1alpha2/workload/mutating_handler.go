/*
Copyright 2019 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workload

import (
	"context"
	"encoding/json"
	"github.com/pkg/errors"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/xishengcai/oam/pkg/oam/util"
)

// MutatingHandler handles Deployment、StatefulSet
type MutatingHandler struct {
}

const (
	IstioLableKey       = "istio-injection"
	IstioLableValue     = "enabled"
	AppIdLableKey       = "oam.runtime.app.id"
	ComponentIdLableKey = "oam.runtime.component.id"
)

// log is for logging in this package.
var mutatelog = logf.Log.WithName("workloads mutate webhook")

var _ admission.Handler = &MutatingHandler{}

// Handle handles admission requests.
func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {

	var (
		deployment  *v1.Deployment
		statefulSet *v1.StatefulSet
		obj         runtime.Object
	)

	switch req.Kind.Kind {
	case "Deployment":
		if err := json.Unmarshal(req.Object.Raw, deployment); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		obj = deployment
	case "StatefulSet":
		if err := json.Unmarshal(req.Object.Raw, statefulSet); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		obj = statefulSet
	default:
		return admission.Errored(http.StatusBadRequest, errors.Errorf("unacceptable kind : %s", req.Kind.Kind))
	}

	// mutate the object
	if err := h.Mutate(obj); err != nil {
		mutatelog.Error(err, "failed to mutate the workload", "name", req.Name)
		return admission.Errored(http.StatusBadRequest, err)
	}

	mutated, err := json.Marshal(obj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	resp := admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, mutated)
	if len(resp.Patches) > 0 {
		mutatelog.Info("admit workloads",
			"namespace", req.Namespace, "name", req.Name, "patches", util.JSONMarshal(resp.Patches))
	}
	return resp
}

// Mutate sets all the default value for the Component
func (h *MutatingHandler) Mutate(obj runtime.Object) error {

	if deployment, ok := obj.(*v1.Deployment); ok {

		if value, ok := deployment.Labels[IstioLableKey]; ok && value == IstioLableValue {
			deployment.Spec.Selector.MatchLabels[IstioLableKey] = IstioLableValue
			deployment.Spec.Template.Labels[IstioLableKey] = IstioLableValue
		}

		if value, ok := deployment.Labels[AppIdLableKey]; ok && value != "" {
			deployment.Spec.Selector.MatchLabels[AppIdLableKey] = value
			deployment.Spec.Template.Labels[AppIdLableKey] = value
		}

		if value, ok := deployment.Labels[ComponentIdLableKey]; ok && value != "" {
			deployment.Spec.Selector.MatchLabels[ComponentIdLableKey] = value
			deployment.Spec.Template.Labels[ComponentIdLableKey] = value
		}
	}

	if statefulset, ok := obj.(*v1.StatefulSet); ok {

		if value, ok := statefulset.Labels[IstioLableKey]; ok && value == IstioLableValue {
			statefulset.Spec.Selector.MatchLabels[IstioLableKey] = IstioLableValue
			statefulset.Spec.Template.Labels[IstioLableKey] = IstioLableValue
		}

		if value, ok := statefulset.Labels[AppIdLableKey]; ok && value != "" {
			statefulset.Spec.Selector.MatchLabels[AppIdLableKey] = value
			statefulset.Spec.Template.Labels[AppIdLableKey] = value
		}

		if value, ok := statefulset.Labels[ComponentIdLableKey]; ok && value != "" {
			statefulset.Spec.Selector.MatchLabels[ComponentIdLableKey] = value
			statefulset.Spec.Template.Labels[ComponentIdLableKey] = value
		}
	}

	return nil
}

var _ inject.Client = &MutatingHandler{}

func (h *MutatingHandler) InjectClient(c client.Client) error {
	return nil
}

var _ admission.DecoderInjector = &MutatingHandler{}

func (h *MutatingHandler) InjectDecoder(d *admission.Decoder) error {
	return nil
}

func RegisterMutatingHandler(mgr manager.Manager) {
	server := mgr.GetWebhookServer()
	server.Register("/mutating-apps-v1-workloads", &webhook.Admission{Handler: &MutatingHandler{}})
}
