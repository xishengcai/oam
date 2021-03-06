package hpatrait

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/api/autoscaling/v2beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/scale/scheme/autoscalingv1"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/pkg/errors"
	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// hpa kind
	kindHPA        = "HorizontalPodAutoscaler"
	gvkDeployment  = "apps/v1, Kind=Deployment"
	gvkStatefulSet = "apps/v1, Kind=StatefulSet"
	labelKey       = "hpatrait.oam.crossplane.io"
)

var (
	groupVersionHPA = v2beta1.SchemeGroupVersion.String()
)

// renderHPA render hpaTrait to kubernetes resource hpa
func (r *Reconciler) renderHPA(t *oamv1alpha2.HorizontalPodAutoscalerTrait, resources []*unstructured.Unstructured) (
	[]*v2beta1.HorizontalPodAutoscaler, error) {
	hpas := make([]*v2beta1.HorizontalPodAutoscaler, 0)
	for _, res := range resources {
		scaleTargetRef, isValidResource, err := renderReference(res)
		if err != nil {
			return nil, err
		}
		if !isValidResource {
			continue
		}
		hpa := &v2beta1.HorizontalPodAutoscaler{
			TypeMeta: metav1.TypeMeta{
				Kind:       kindHPA,
				APIVersion: groupVersionHPA,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      t.GetName(), // use trait name as hpa name
				Namespace: t.GetNamespace(),
				Labels: map[string]string{
					labelKey: string(t.GetUID()),
				},
			},
			Spec: v2beta1.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: scaleTargetRef,
				MinReplicas:    t.Spec.MinReplicas,
				MaxReplicas:    t.Spec.MaxReplicas,
				Metrics:        t.Spec.Metrics,
			},
		}
		if err := ctrl.SetControllerReference(t, hpa, r.Scheme); err != nil {
			return nil, err
		}
		hpas = append(hpas, hpa)
	}
	return hpas, nil
}

func (r *Reconciler) cleanUpLegacyHPAs(ctx context.Context, hpaTrait *oamv1alpha2.HorizontalPodAutoscalerTrait, hpaUIDs []types.UID) error {
	var hpa v2beta1.HorizontalPodAutoscaler
	for _, res := range hpaTrait.Status.Resources {
		if res.Kind == kindHPA && res.APIVersion == autoscalingv1.SchemeGroupVersion.String() {
			isLegacy := true
			for _, i := range hpaUIDs {
				if i == res.UID {
					isLegacy = false
					break
				}
			}
			if isLegacy {
				err := r.Delete(ctx, &hpa)
				if err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}
		}
	}
	return nil
}

func renderReference(resource *unstructured.Unstructured) (r v2beta1.CrossVersionObjectReference, isValidResource bool, err error) {
	resGVK := resource.GetObjectKind().GroupVersionKind().String()
	isValidResource = false
	r = v2beta1.CrossVersionObjectReference{
		Kind:       resource.GetKind(),
		Name:       resource.GetName(),
		APIVersion: resource.GetAPIVersion(),
	}

	switch resGVK {
	case gvkDeployment:
		var deploy appsv1.Deployment
		bts, _ := json.Marshal(resource)
		if err := json.Unmarshal(bts, &deploy); err != nil {
			return r, isValidResource, errors.Wrap(err, "Failed to convert an unstructured obj to a appsv1.deployment")
		}

		// check spec.containers.resource
		// if missing, raise an error
		// for it's required by HPA
		containers := deploy.Spec.Template.Spec.Containers
		for _, container := range containers {
			if container.Resources.Requests == nil {
				return r, isValidResource, fmt.Errorf("cannot get container.resources.requests from deployment: %s", deploy.GetName())
			}
		}
	case gvkStatefulSet:
		var sts appsv1.StatefulSet
		bts, _ := json.Marshal(resource)
		if err := json.Unmarshal(bts, &sts); err != nil {
			return r, isValidResource, errors.Wrap(err, "Failed to convert an unstructured obj to a appsv1.statefulset")
		}
		containers := sts.Spec.Template.Spec.Containers
		for _, container := range containers {
			if container.Resources.Requests == nil {
				return r, isValidResource, fmt.Errorf("cannot get container.resources.requests from statefulset: %s", sts.GetName())
			}
		}
	}

	isValidResource = true
	return r, isValidResource, nil
}
