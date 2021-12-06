package applicationconfiguration

import (
	"context"

	"github.com/xishengcai/oam/util/apply"

	"github.com/xishengcai/oam/apis/core/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VolumeClaimRenderer interface {
	Render(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) error
}

type VolumeClaimRenderFn func(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) error

func (fn VolumeClaimRenderFn) Render(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) error {
	return fn(ctx, ac)
}

var _ VolumeClaimRenderer = &volumeClaims{}

type volumeClaims struct {
	applicator apply.Applicator
}

func (r *volumeClaims) Render(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) error {
	for _, vc := range ac.Spec.VolumeClaims {
		if err := r.renderVolumeClaims(ctx, ac, vc); err != nil {
			return err
		}
	}
	return nil
}

// renderVolumeClaims create VolumeClaim
// 需要校验，如果已经存在则不允许创建
func (r *volumeClaims) renderVolumeClaims(ctx context.Context, ac *v1alpha2.ApplicationConfiguration, vcc v1alpha2.VolumeClaimConfig) error {

	//name := ac.Name + "-" + vcc.Name
	name := vcc.Name
	vc := &v1alpha2.VolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha2.Group + "/" + v1alpha2.Version,
			Kind:       v1alpha2.VolumeClaimKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ac.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: ac.APIVersion,
					Kind:       ac.Kind,
					Name:       ac.Name,
					UID:        ac.UID,
				},
			},
		},
		Spec: v1alpha2.VolumeClaimSpec{
			Type:             vcc.Type,
			HostPath:         vcc.HostPath,
			StorageClassName: vcc.StorageClassName,
			Size:             vcc.Size,
		},
	}
	applyOpts := []apply.ApplyOption{apply.MustBeControllableBy(ac.GetUID())}
	if err := r.applicator.Apply(ctx, vc, applyOpts...); err != nil {
		return err
	}
	return nil
}
