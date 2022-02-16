package applicationconfiguration

import (
	"context"
	"fmt"
	"strconv"

	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"github.com/xishengcai/oam/pkg/oam"
	"github.com/xishengcai/oam/pkg/oam/discoverymapper"
	"github.com/xishengcai/oam/pkg/oam/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/xishengcai/oam/apis/core/v1alpha2"
)

// A ComponentRenderer renders an ApplicationConfiguration's Components into
// workloads and traits.
type ComponentRenderer interface {
	Render(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) ([]*Workload, *v1alpha2.DependencyStatus, error)
}

// A ComponentRenderFn renders an ApplicationConfiguration's Components into
// workloads and traits.
type ComponentRenderFn func(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) ([]Workload, *v1alpha2.DependencyStatus, error)

// Render an ApplicationConfiguration's Components into workloads and traits.
func (fn ComponentRenderFn) Render(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) (
	[]Workload, *v1alpha2.DependencyStatus, error) {
	return fn(ctx, ac)
}

var _ ComponentRenderer = &components{}

type components struct {
	client   client.Reader
	dm       discoverymapper.DiscoveryMapper
	params   ParameterResolver
	workload ResourceRenderer
	trait    ResourceRenderer
}

func (r *components) Render(ctx context.Context, ac *v1alpha2.ApplicationConfiguration) ([]*Workload, *v1alpha2.DependencyStatus, error) {
	workloads := make([]*Workload, 0, len(ac.Spec.Components))
	dag := newDAG()

	for _, acc := range ac.Spec.Components {
		w, err := r.renderComponent(ctx, acc, ac, dag)
		if err != nil {
			return nil, nil, err
		}

		workloads = append(workloads, w)
	}

	ds := &v1alpha2.DependencyStatus{}
	res := make([]*Workload, 0, len(ac.Spec.Components))
	for i, acc := range ac.Spec.Components {
		unsatisfied, err := r.handleDependency(ctx, workloads[i], acc, dag, ac)
		if err != nil {
			return nil, nil, err
		}
		ds.Unsatisfied = append(ds.Unsatisfied, unsatisfied...)
		res = append(res, workloads[i])
	}

	return res, ds, nil
}

func (r *components) renderComponent(ctx context.Context, acc v1alpha2.ApplicationConfigurationComponent, ac *v1alpha2.ApplicationConfiguration, dag *dag) (*Workload, error) {
	if acc.RevisionName != "" {
		acc.ComponentName = ExtractComponentName(acc.RevisionName)
	}
	c, componentRevisionName, err := util.GetComponent(ctx, r.client, acc, ac.GetNamespace())
	if err != nil {
		return nil, err
	}
	p, err := r.params.Resolve(c.Spec.Parameters, acc.ParameterValues)
	if err != nil {
		return nil, errors.Wrapf(err, errFmtResolveParams, acc.ComponentName)
	}

	// 每次都是重新生成workload
	w, err := r.workload.Render(c.Spec.Workload.Raw, p...)
	if err != nil {
		return nil, errors.Wrapf(err, errFmtRenderWorkload, acc.ComponentName)
	}
	compInfoLabels := map[string]string{
		oam.LabelAppName:              ac.Name,
		oam.LabelAppComponent:         acc.ComponentName,
		oam.LabelAppComponentRevision: componentRevisionName,
		oam.LabelOAMResourceType:      oam.ResourceTypeWorkload,
	}
	util.AddLabels(w, compInfoLabels)

	compInfoAnnotations := map[string]string{
		oam.AnnotationAppGeneration: strconv.Itoa(int(ac.Generation)),
	}
	util.AddAnnotations(w, compInfoAnnotations)

	// pass through labels and annotation from app-config to workload
	util.PassLabelAndAnnotation(ac, w)

	ref := metav1.NewControllerRef(ac, v1alpha2.ApplicationConfigurationGroupVersionKind)
	w.SetOwnerReferences([]metav1.OwnerReference{*ref})
	w.SetNamespace(ac.GetNamespace())

	traits := make([]*Trait, 0, len(acc.Traits))
	traitDefs := make([]v1alpha2.TraitDefinition, 0, len(acc.Traits))
	compInfoLabels[oam.LabelOAMResourceType] = oam.ResourceTypeTrait

	for _, ct := range acc.Traits {
		t, traitDef, errIn := r.renderTrait(ctx, ct, ac, acc.ComponentName, ref, dag)
		if errIn != nil {
			return nil, errIn
		}
		util.AddLabels(t, compInfoLabels)
		util.AddAnnotations(t, compInfoAnnotations)
		// pass through labels and annotation from app-config to trait
		util.PassLabelAndAnnotation(ac, t)
		traits = append(traits, &Trait{Object: *t, Definition: *traitDef})
		traitDefs = append(traitDefs, *traitDef)
	}

	util.AddLabels(w, map[string]string{util.LabelAppID: ac.Name})

	existingWorkload, err := r.getExistingWorkload(ctx, ac, c, w)
	if err != nil {
		return nil, err
	}

	if existingWorkload != nil && existingWorkload.Object == nil {
		//if acc.WorkloadType == "StatefulSet" {
		//	util.AddLabels(w, map[string]string{util.LabelKeyChildResource: util.KindStatefulSet})
		//}
		util.AddLabels(w, map[string]string{util.LabelKeyChildResource: util.KindDeployment})
	} else if existingWorkload != nil {
		util.AddLabels(w, existingWorkload.GetLabels())
	}

	if err := SetWorkloadInstanceName(traitDefs, w, c, existingWorkload); err != nil {
		return nil, err
	}
	// create the ref after the workload name is set
	workloadRef := runtimev1alpha1.TypedReference{
		APIVersion: w.GetAPIVersion(),
		Kind:       w.GetKind(),
		Name:       w.GetName(),
	}
	//  We only patch a TypedReference object to the trait if it asks for it
	for i := range acc.Traits {
		traitDef := traitDefs[i]
		trait := traits[i]
		workloadRefPath := traitDef.Spec.WorkloadRefPath
		if len(workloadRefPath) != 0 {
			if err := fieldpath.Pave(trait.Object.UnstructuredContent()).SetValue(workloadRefPath, workloadRef); err != nil {
				return nil, errors.Wrapf(err, errFmtSetWorkloadRef, trait.Object.GetName(), w.GetName())
			}
		}
	}
	scopes := make([]unstructured.Unstructured, 0, len(acc.Scopes))
	for _, cs := range acc.Scopes {
		scopeObject, err := r.renderScope(ctx, cs, ac.GetNamespace())
		if err != nil {
			return nil, err
		}

		scopes = append(scopes, *scopeObject)
	}

	addDataOutputsToDAG(dag, acc.DataOutputs, w)

	return &Workload{ComponentName: acc.ComponentName, ComponentRevisionName: componentRevisionName,
		Workload: w, Traits: traits, RevisionEnabled: isRevisionEnabled(traitDefs), Scopes: scopes}, nil
}

func (r *components) renderTrait(ctx context.Context, ct v1alpha2.ComponentTrait, ac *v1alpha2.ApplicationConfiguration,
	componentName string, ref *metav1.OwnerReference, dag *dag) (*unstructured.Unstructured, *v1alpha2.TraitDefinition, error) {
	t, err := r.trait.Render(ct.Trait.Raw)
	if err != nil {
		return nil, nil, errors.Wrapf(err, errFmtRenderTrait, componentName)
	}
	traitDef, err := util.FetchTraitDefinition(ctx, r.client, r.dm, t)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return t, util.GetDummyTraitDefinition(t), nil
		}
		return nil, nil, errors.Wrapf(err, errFmtGetTraitDefinition, t.GetAPIVersion(), t.GetKind(), t.GetName())
	}
	traitName := getTraitName(ac, componentName, &ct, t, traitDef)

	setTraitProperties(t, traitName, ac.GetNamespace(), ref)

	addDataOutputsToDAG(dag, ct.DataOutputs, t)

	return t, traitDef, nil
}

func (r *components) renderScope(ctx context.Context, cs v1alpha2.ComponentScope, ns string) (*unstructured.Unstructured, error) {
	// Get Scope instance from k8s, since it is global and not a child resource of workflow.
	scopeObject := &unstructured.Unstructured{}
	scopeObject.SetAPIVersion(cs.ScopeReference.APIVersion)
	scopeObject.SetKind(cs.ScopeReference.Kind)
	scopeObjectRef := types.NamespacedName{Namespace: ns, Name: cs.ScopeReference.Name}
	if err := r.client.Get(ctx, scopeObjectRef, scopeObject); err != nil {
		return nil, errors.Wrapf(err, errFmtGetScope, cs.ScopeReference.Name)
	}
	return scopeObject, nil
}

func (r *components) handleDependency(ctx context.Context, w *Workload, acc v1alpha2.ApplicationConfigurationComponent,
	dag *dag, ac *v1alpha2.ApplicationConfiguration) ([]v1alpha2.UnstaifiedDependency, error) {
	uds := make([]v1alpha2.UnstaifiedDependency, 0)
	unstructuredAC, err := util.Object2Unstructured(ac)
	if err != nil {
		return nil, errors.Wrapf(err, "handleDataInput by convert AppConfig (%s) to unstructured object failed", ac.Name)
	}
	unsatisfied, err := r.handleDataInput(ctx, acc.DataInputs, dag, w.Workload, unstructuredAC)
	if err != nil {
		return nil, errors.Wrapf(err, "handleDataInput for workload (%s/%s) failed", w.Workload.GetNamespace(), w.Workload.GetName())
	}
	if len(unsatisfied) != 0 {
		uds = append(uds, unsatisfied...)
		w.HasDep = true
	}

	for i, ct := range acc.Traits {
		trait := w.Traits[i]
		unsatisfied, err := r.handleDataInput(ctx, ct.DataInputs, dag, &trait.Object, unstructuredAC)
		if err != nil {
			return nil, errors.Wrapf(err, "handleDataInput for trait (%s/%s) failed", trait.Object.GetNamespace(), trait.Object.GetName())
		}
		if len(unsatisfied) != 0 {
			uds = append(uds, unsatisfied...)
			trait.HasDep = true
		}
	}
	return uds, nil
}

func (r *components) getDataInput(ctx context.Context, s *dagSource, ac *unstructured.Unstructured) (interface{}, bool, string, error) {
	obj := s.ObjectRef
	key := types.NamespacedName{
		Namespace: obj.Namespace,
		Name:      obj.Name,
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(obj.GroupVersionKind())
	err := r.client.Get(ctx, key, u)
	if err != nil {
		reason := fmt.Sprintf("failed to get object (%s)", key.String())
		return nil, false, reason, errors.Wrap(resource.IgnoreNotFound(err), reason)
	}
	paved := fieldpath.Pave(u.UnstructuredContent())
	pavedAC := fieldpath.Pave(ac.UnstructuredContent())

	rawval, err := paved.GetValue(obj.FieldPath)
	if err != nil {
		if fieldpath.IsNotFound(err) {
			return "", false, fmt.Sprintf("%s not found in object", obj.FieldPath), nil
		}
		err = fmt.Errorf("failed to get field value (%s) in object (%s): %w", obj.FieldPath, key.String(), err)
		return nil, false, err.Error(), err
	}

	var ok bool
	var reason string
	switch val := rawval.(type) {
	case string:
		// For string input we will:
		// - check its value not empty if no condition is given.
		// - check its value against conditions if no field path is specified.
		ok, reason = matchValue(s.Conditions, val, paved, pavedAC)
	default:
		ok, reason = checkConditions(s.Conditions, paved, nil, pavedAC)
	}
	if !ok {
		return nil, false, reason, nil
	}

	return rawval, true, "", nil
}
