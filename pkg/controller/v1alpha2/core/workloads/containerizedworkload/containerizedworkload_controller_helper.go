package containerizedworkload

import (
	"context"

	"k8s.io/klog/v2"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam/util"
)

// create a corresponding deployment
func (r *Reconciler) renderChildResource(workload *v1alpha2.ContainerizedWorkload) (runtime.Object, error) {
	resource, err := TranslateContainerWorkload(workload)
	if err != nil {
		return nil, err
	}

	if workload.Labels[util.LabelKeyChildResource] == util.KindStatefulSet {
		resource = transDepToSts(resource.(*appsv1.Deployment))
	}
	if err := ctrl.SetControllerReference(workload, resource, r.Scheme); err != nil {
		return nil, err
	}
	return resource, nil
}

// delete childResource that are not the same as the existing
func (r *Reconciler) cleanupResources(ctx context.Context, workload *v1alpha2.ContainerizedWorkload,
	childResourceKind string, childResourceUID types.UID) error {
	for _, res := range workload.Status.Resources {
		// if res.Kind == childResourceKind && res.APIVersion == appsv1.SchemeGroupVersion.String() {
		if res.Kind == childResourceKind {
			if res.UID != childResourceUID {
				dn := client.ObjectKey{Name: res.Name, Namespace: workload.Namespace}

				obj := generateChildResourceObj(res.Kind)

				if err := r.Get(ctx, dn, obj); err != nil {
					if apierrors.IsNotFound(err) {
						continue
					}
					return err
				}
				if err := r.Delete(ctx, obj); err != nil {
					return err
				}
				klog.InfoS("gc containerizedWorkload childResource, Removed an orphaned: ", res.Kind, ",orphaned UID: ", res.UID)
			}
		}
	}
	return nil
}

// TODO: generate obj by kind
func generateChildResourceObj(objType string) runtime.Object {
	switch objType {
	case util.KindDeployment:
		return &appsv1.Deployment{}
	case util.KindStatefulSet:
		return &appsv1.StatefulSet{}
	case util.KindConfigMap:
		return &corev1.ConfigMap{}
	}
	return nil
}

// create a service for the deployment
func (r *Reconciler) renderService(ctx context.Context,
	workload *v1alpha2.ContainerizedWorkload, obj runtime.Object) (*corev1.Service, error) {
	// create a service for the workload
	service, err := ServiceInjector(ctx, workload, obj)
	if err != nil {
		return nil, err
	}
	if service == nil || len(service.Spec.Ports) == 0 {
		return nil, nil
	}
	// the service injector lib doesn't set the namespace and serviceType
	service.Namespace = workload.Namespace
	service.Spec.Type = corev1.ServiceTypeClusterIP
	// k8s server-side patch complains if the protocol is not set

	for index, i := range service.Spec.Ports {
		if i.Protocol == "" {
			service.Spec.Ports[index].Protocol = corev1.ProtocolTCP
		}
	}
	// always set the controller reference so that we can watch this service and
	if err := ctrl.SetControllerReference(workload, service, r.Scheme); err != nil {
		return nil, err
	}
	return service, nil
}

// create ConfigMaps for ContainerConfigFiles
func (r *Reconciler) renderConfigMaps(ctx context.Context, workload *v1alpha2.ContainerizedWorkload) (map[string]*corev1.ConfigMap, error) {
	configMaps, err := TranslateConfigMaps(ctx, workload)
	if err != nil {
		return nil, err
	}
	for _, cm := range configMaps {
		// always set the controller reference so that we can watch this configmap and it will be deleted automatically
		if err := ctrl.SetControllerReference(workload, cm, r.Scheme); err != nil {
			return nil, err
		}
	}
	return configMaps, nil
}
