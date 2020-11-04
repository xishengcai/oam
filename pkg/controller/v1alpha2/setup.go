/*
Copyright 2019 The Crossplane Authors.

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

package v1alpha2

import (
	"github.com/xishengcai/oam/pkg/controller/v1alpha2/core/scopes/healthscope"
	"github.com/xishengcai/oam/pkg/controller/v1alpha2/core/traits/hpa"
	"github.com/xishengcai/oam/pkg/controller/v1alpha2/core/traits/manualscalertrait"
	"github.com/xishengcai/oam/pkg/controller/v1alpha2/core/traits/volumetrait"
	"github.com/xishengcai/oam/pkg/controller/v1alpha2/core/workloads/containerizedworkload"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/xishengcai/oam/pkg/controller/v1alpha2/applicationconfiguration"
)

// Setup workload controllers.
func Setup(mgr ctrl.Manager, l logging.Logger) error {
	for _, setup := range []func(ctrl.Manager, logging.Logger) error{
		applicationconfiguration.Setup,
		containerizedworkload.Setup,
		volumetrait.Setup,
		manualscalertrait.Setup,
		healthscope.Setup,
		hpa.Setup,
	} {
		if err := setup(mgr, l); err != nil {
			return err
		}
	}
	return nil
}
