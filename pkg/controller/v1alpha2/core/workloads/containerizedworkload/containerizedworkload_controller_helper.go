package containerizedworkload

import (
	"context"
	"fmt"
	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// create a corresponding deployment
func (r *Reconciler) renderDeployment(ctx context.Context,
	workload *v1alpha2.ContainerizedWorkload) (*appsv1.Deployment, error) {

	resources, err := TranslateContainerWorkload(ctx, workload)
	if err != nil {
		return nil, err
	}
	deploy, ok := resources[0].(*appsv1.Deployment)
	if !ok {
		return nil, fmt.Errorf("internal error, deployment is not rendered correctly")
	}
	deploy.SetLabels(map[string]string{util.LabelAppId:workload.Labels[util.LabelAppId]})
	// make sure we don't have opinion on the replica count
	deploy.Spec.Replicas = nil
	// k8s server-side patch complains if the protocol is not set
	for i := 0; i < len(deploy.Spec.Template.Spec.Containers); i++ {
		for j := 0; j < len(deploy.Spec.Template.Spec.Containers[i].Ports); j++ {
			if len(deploy.Spec.Template.Spec.Containers[i].Ports[j].Protocol) == 0 {
				deploy.Spec.Template.Spec.Containers[i].Ports[j].Protocol = corev1.ProtocolTCP
			}
		}
	}
	r.log.Info(" rendered a deployment", "deploy", deploy.Spec.Template.Spec)

	// set the controller reference so that we can watch this deployment and it will be deleted automatically
	if err := ctrl.SetControllerReference(workload, deploy, r.Scheme); err != nil {
		return nil, err
	}

	return deploy, nil
}

// create a corresponding statefulset
func (r *Reconciler) renderStatefulSet(ctx context.Context,
	workload *v1alpha2.ContainerizedWorkload) (*appsv1.StatefulSet, error) {

	resources, err := TranslateStatefulSetWorkload(ctx, workload)
	if err != nil {
		return nil, err
	}
	sts, ok := resources[0].(*appsv1.StatefulSet)
	if !ok {
		return nil, fmt.Errorf("internal error, statefulSet is not rendered correctly")
	}
	// make sure we don't have opinion on the replica count
	sts.SetLabels(map[string]string{util.LabelAppId:workload.Labels[util.LabelAppId]})
	sts.Spec.Replicas = nil
	// k8s server-side patch complains if the protocol is not set
	for i := 0; i < len(sts.Spec.Template.Spec.Containers); i++ {
		for j := 0; j < len(sts.Spec.Template.Spec.Containers[i].Ports); j++ {
			if len(sts.Spec.Template.Spec.Containers[i].Ports[j].Protocol) == 0 {
				sts.Spec.Template.Spec.Containers[i].Ports[j].Protocol = corev1.ProtocolTCP
			}
		}
	}
	r.log.Info(" rendered a statefulSet", "statefulSet", sts.Spec)

	// set the controller reference so that we can watch this statefulSet and it will be deleted automatically
	if err := ctrl.SetControllerReference(workload, sts, r.Scheme); err != nil {
		return nil, err
	}

	return sts, nil
}

// delete childResource that are not the same as the existing
// nolint:gocyclo
func (r *Reconciler) cleanupResources(ctx context.Context,
	workload *v1alpha2.ContainerizedWorkload,
	childResourceKind string,
	childResourceUID types.UID) error {

	log := r.log.WithValues("gc childResource", workload.Name)
	for _, res := range workload.Status.Resources {
		//if res.Kind == childResourceKind && res.APIVersion == appsv1.SchemeGroupVersion.String() {
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
				log.Info("gc containerizedWorkload childResource, Removed an orphaned: ", res.Kind, ",orphaned UID: ", res.UID)
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
	}
	return nil
}

// create a service for the deployment
func (r *Reconciler) renderService(ctx context.Context,
	workload *v1alpha2.ContainerizedWorkload, objs runtime.Object) (*corev1.Service, error) {
	// create a service for the workload
	service, err := ServiceInjector(ctx, workload, objs)
	if err != nil {
		return nil, err
	}
	if service == nil || len(service.Spec.Ports)==0 {
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
