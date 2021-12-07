package volumetrait

import (
	"context"

	"k8s.io/klog/v2"

	"github.com/xishengcai/oam/apis/core/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *Reconcile) cleanupResources(ctx context.Context, volumeTrait *v1alpha2.VolumeTrait, pvcName []string) error {
	for _, res := range volumeTrait.Status.Resources {
		if findStringElem(pvcName, res.Name) {
			continue
		}
		if err := r.clientSet.CoreV1().PersistentVolumeClaims(volumeTrait.Namespace).
			Delete(ctx, res.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
		klog.InfoS("gc volumeTrait pvc resources", res.Kind, res.UID)
	}
	return nil
}

func findStringElem(array []string, elem string) bool {
	for _, a := range array {
		if a == elem {
			return true
		}
	}
	return false
}
