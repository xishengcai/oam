package applicationconfiguration

import (
	"strings"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// A Workload produced by an OAM ApplicationConfiguration.
type Workload struct {
	// ComponentName that produced this workload.
	ComponentName string

	// ComponentRevisionName of current component
	ComponentRevisionName string

	// A Workload object.
	Workload *unstructured.Unstructured

	// HasDep indicates whether this resource has dependencies and unready to be applied.
	HasDep bool

	// Traits associated with this workload.
	Traits []*Trait

	// RevisionEnabled means multiple workloads of same component will possibly be alive.
	RevisionEnabled bool

	// Scopes associated with this workload.
	Scopes []unstructured.Unstructured
}

// A Trait produced by an OAM ApplicationConfiguration.
type Trait struct {
	Object unstructured.Unstructured

	// HasDep indicates whether this resource has dependencies and unready to be applied.
	HasDep bool

	// Definition indicates the trait's definition
	Definition v1alpha2.TraitDefinition
}

type VolumeClaim struct {
}

// Status produces the status of this workload and its traits, suitable for use
// in the status of an ApplicationConfiguration.
func (w Workload) Status() v1alpha2.WorkloadStatus {
	acw := v1alpha2.WorkloadStatus{
		ComponentName:         w.ComponentName,
		ComponentRevisionName: w.ComponentRevisionName,
		Reference: v1alpha1.TypedReference{
			APIVersion: w.Workload.GetAPIVersion(),
			Kind:       w.Workload.GetKind(),
			Name:       w.Workload.GetName(),
		},
		Traits: make([]v1alpha2.WorkloadTrait, len(w.Traits)),
		Scopes: make([]v1alpha2.WorkloadScope, len(w.Scopes)),
	}
	for i, tr := range w.Traits {
		if tr.Definition.Name == util.Dummy && tr.Definition.Spec.Reference.Name == util.Dummy {
			acw.Traits[i].Message = util.DummyTraitMessage
		}
		acw.Traits[i].Reference = v1alpha1.TypedReference{
			APIVersion: w.Traits[i].Object.GetAPIVersion(),
			Kind:       w.Traits[i].Object.GetKind(),
			Name:       w.Traits[i].Object.GetName(),
		}
	}
	for i, s := range w.Scopes {
		acw.Scopes[i].Reference = v1alpha1.TypedReference{
			APIVersion: s.GetAPIVersion(),
			Kind:       s.GetKind(),
			Name:       s.GetName(),
		}
	}
	return acw
}

// IsRevisionWorkload check is a workload is an old revision Workload which shouldn't be garbage collected.
// TODO(wonderflow): Do we have a better way to recognize it's a revisionWorkload which can't be garbage collected by AppConfig?
func IsRevisionWorkload(status v1alpha2.WorkloadStatus) bool {
	return strings.HasPrefix(status.Reference.Name, status.ComponentName+"-")
}

func eligible(namespace string, ws []v1alpha2.WorkloadStatus, w []*Workload) []unstructured.Unstructured {
	applied := make(map[v1alpha1.TypedReference]bool)
	for _, wl := range w {
		r := v1alpha1.TypedReference{
			APIVersion: wl.Workload.GetAPIVersion(),
			Kind:       wl.Workload.GetKind(),
			Name:       wl.Workload.GetName(),
		}
		applied[r] = true
		for _, t := range wl.Traits {
			r := v1alpha1.TypedReference{
				APIVersion: t.Object.GetAPIVersion(),
				Kind:       t.Object.GetKind(),
				Name:       t.Object.GetName(),
			}
			applied[r] = true
		}
	}
	eligible := make([]unstructured.Unstructured, 0)
	for _, s := range ws {
		if !applied[s.Reference] && !IsRevisionWorkload(s) {
			w := &unstructured.Unstructured{}
			w.SetAPIVersion(s.Reference.APIVersion)
			w.SetKind(s.Reference.Kind)
			w.SetNamespace(namespace)
			w.SetName(s.Reference.Name)
			eligible = append(eligible, *w)
		}

		for _, ts := range s.Traits {
			if !applied[ts.Reference] {
				t := &unstructured.Unstructured{}
				t.SetAPIVersion(ts.Reference.APIVersion)
				t.SetKind(ts.Reference.Kind)
				t.SetNamespace(namespace)
				t.SetName(ts.Reference.Name)
				eligible = append(eligible, *t)
			}
		}
	}

	return eligible
}
