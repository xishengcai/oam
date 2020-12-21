package volumetrait

import (
	"context"
	"github.com/crossplane/oam-kubernetes-runtime2/apis/core/v1alpha2"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *Reconcile) cleanupResources(ctx context.Context,
	volumeTrait *v1alpha2.VolumeTrait,
	pvcUID []types.UID) error {

	log := r.log.WithValues("gc volumeTrait pvc resources", volumeTrait.Name)
	for _, res := range volumeTrait.Status.Resources {
		found := false
		for _, uid := range pvcUID {
			if uid == res.UID {
				found = true
				break
			}
		}

		if found {
			continue
		}
		dn := client.ObjectKey{Name: res.Name, Namespace: volumeTrait.Namespace}
		pvc := &v1.PersistentVolumeClaim{}
		if err := r.Get(ctx, dn, pvc); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if err := r.Delete(ctx, pvc); err != nil {
			return err
		}
		log.Info("gc volumeTrait pvc resources", res.Kind, res.UID)
	}

	return nil
}
