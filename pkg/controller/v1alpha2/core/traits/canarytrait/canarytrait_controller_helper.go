package canarytrait

import (
	"context"
	oamv1alpha2 "github.com/xishengcai/oam/apis/core/v1alpha2"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *Reconciler) renderDestinationRule(trait oamv1alpha2.CanaryTrait) (*v1alpha3.DestinationRule, error) {
	// create destinationRule
	destinationRule := v1alpha3.DestinationRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       destinationRuleKind,
			APIVersion: destinationAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      trait.Name,
			Namespace: trait.Namespace,
			Labels:    trait.GetLabels(),
		},
		Spec: networkingv1alpha3.DestinationRule{
			Host: trait.Name,
			Subsets: []*networkingv1alpha3.Subset{
				{
					Name:   "v1",
					Labels: map[string]string{"version": "v1"},
				},
				{
					Name:   "v2",
					Labels: map[string]string{"version": "v2"},
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(&trait, &destinationRule, r.Scheme); err != nil {
		return nil, err
	}
	dr, err := r.IstioClient.NetworkingV1alpha3().DestinationRules(trait.Namespace).Get(context.TODO(), trait.Name, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		dr, err := r.IstioClient.NetworkingV1alpha3().DestinationRules(trait.Namespace).Create(context.TODO(), &destinationRule, metav1.CreateOptions{})
		return dr, err
	}

	if err != nil {
		return nil, err
	}
	dr.Spec = destinationRule.Spec
	dr, err = r.IstioClient.NetworkingV1alpha3().DestinationRules(trait.Namespace).Update(context.TODO(), dr, metav1.UpdateOptions{})
	return dr, err

}

func (r *Reconciler) renderVirtualService(trait oamv1alpha2.CanaryTrait) (*v1alpha3.VirtualService, error) {
	// create virtualService
	virtualService := v1alpha3.VirtualService{
		TypeMeta: metav1.TypeMeta{
			Kind:       virtualServiceKind,
			APIVersion: virtualServiceAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      trait.GetName(),
			Namespace: trait.GetNamespace(),
			Labels:    trait.GetLabels(),
		},
		Spec: networkingv1alpha3.VirtualService{
			Hosts: []string{trait.GetName()},
		},
	}
	if trait.Spec.Type == canaryTypeTraffic {
		virtualService.Spec.Http = []*networkingv1alpha3.HTTPRoute{
			{
				Route: []*networkingv1alpha3.HTTPRouteDestination{
					{
						// TODO 在定义的时候设置范围大小0-100
						Weight: trait.Spec.Proportion,
						Destination: &networkingv1alpha3.Destination{
							Host:   trait.GetName(),
							Subset: "v1",
						},
					},
					{
						Weight: 100 - trait.Spec.Proportion,
						Destination: &networkingv1alpha3.Destination{
							Host:   trait.GetName(),
							Subset: "v2",
						},
					},
				},
			},
		}
	}

	// TODO header 暂时不做
	if err := ctrl.SetControllerReference(&trait, &virtualService, r.Scheme); err != nil {
		return nil, err
	}
	vs, err := r.IstioClient.NetworkingV1alpha3().VirtualServices(trait.Namespace).Get(context.TODO(), trait.Name, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		vs, err := r.IstioClient.NetworkingV1alpha3().VirtualServices(trait.Namespace).Create(context.TODO(), &virtualService, metav1.CreateOptions{})
		return vs, err
	}

	if err != nil {
		return nil, err
	}

	vs.Spec = virtualService.Spec
	vs, err = r.IstioClient.NetworkingV1alpha3().VirtualServices(trait.Namespace).Update(context.TODO(), vs, metav1.UpdateOptions{})
	return vs, err
}
