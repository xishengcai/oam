/*
Copyright 2020 The Crossplane Authors.

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

package applicationconfiguration

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
	"github.com/xishengcai/oam/apis/core/v1alpha2"
	"github.com/xishengcai/oam/pkg/oam"
	"github.com/xishengcai/oam/pkg/oam/util"
)

var (
	// ErrDataOutputNotExist is an error indicating the DataOutput specified doesn't not exist
	ErrDataOutputNotExist = errors.New("DataOutput does not exist")
)

const (
	instanceNamePath = "metadata.name"
)

func setTraitProperties(t *unstructured.Unstructured, traitName, namespace string, ref *metav1.OwnerReference) {
	// Set metadata name for `Trait` if the metadata name is NOT set.
	if t.GetName() == "" {
		t.SetName(traitName)
	}

	t.SetOwnerReferences([]metav1.OwnerReference{*ref})
	t.SetNamespace(namespace)
}

// SetWorkloadInstanceName will set metadata.name for workload CR according to createRevision flag in traitDefinition
func SetWorkloadInstanceName(traitDefs []v1alpha2.TraitDefinition, w *unstructured.Unstructured, c *v1alpha2.Component,
	existingWorkload *unstructured.Unstructured) error {
	// Don't override the specified name
	if w.GetName() != "" {
		return nil
	}
	pv := fieldpath.Pave(w.UnstructuredContent())
	if isRevisionEnabled(traitDefs) {
		if c.Status.LatestRevision == nil {
			return fmt.Errorf(errFmtCompRevision, c.Name)
		}

		componentLastRevision := c.Status.LatestRevision.Name
		// if workload exists, check the revision label, we will not change the name if workload exists and no revision changed
		if existingWorkload != nil && existingWorkload.GetLabels()[oam.LabelAppComponentRevision] == componentLastRevision {
			return nil
		}

		// if revisionEnabled and the running workload's revision isn't equal to the component's latest reversion,
		// use revisionName as the workload name
		if err := pv.SetString(instanceNamePath, componentLastRevision); err != nil {
			return errors.Wrapf(err, errSetValueForField, instanceNamePath, c.Status.LatestRevision)
		}

		return nil
	}
	// use component name as workload name, which means we will always use one workload for different revisions
	if err := pv.SetString(instanceNamePath, c.GetName()); err != nil {
		return errors.Wrapf(err, errSetValueForField, instanceNamePath, c.GetName())
	}
	w.Object = pv.UnstructuredContent()
	return nil
}

// isRevisionEnabled will check if any of the traitDefinitions has a createRevision flag
func isRevisionEnabled(traitDefs []v1alpha2.TraitDefinition) bool {
	for _, td := range traitDefs {
		if td.Spec.RevisionEnabled {
			return true
		}
	}
	return false
}

// A ResourceRenderer renders a Kubernetes-compliant YAML resource into an
// Unstructured object, optionally setting the supplied parameters.
type ResourceRenderer interface {
	Render(data []byte, p ...Parameter) (*unstructured.Unstructured, error)
}

// A ResourceRenderFn renders a Kubernetes-compliant YAML resource into an
// Unstructured object, optionally setting the supplied parameters.
type ResourceRenderFn func(data []byte, p ...Parameter) (*unstructured.Unstructured, error)

// Render the supplied Kubernetes YAML resource.
func (fn ResourceRenderFn) Render(data []byte, p ...Parameter) (*unstructured.Unstructured, error) {
	return fn(data, p...)
}

func renderWorkload(data []byte, p ...Parameter) (*unstructured.Unstructured, error) {
	// TODO(negz): Is there a better decoder to use here?
	w := &fieldpath.Paved{}
	if err := json.Unmarshal(data, w); err != nil {
		return nil, errors.Wrap(err, errUnmarshalWorkload)
	}

	for _, param := range p {
		for _, path := range param.FieldPaths {
			// TODO(negz): Infer parameter type from workload OpenAPI schema.
			switch param.Value.Type {
			case intstr.String:
				if err := w.SetString(path, param.Value.StrVal); err != nil {
					return nil, errors.Wrapf(err, errFmtSetParam, param.Name)
				}
			case intstr.Int:
				if err := w.SetNumber(path, float64(param.Value.IntVal)); err != nil {
					return nil, errors.Wrapf(err, errFmtSetParam, param.Name)
				}
			}
		}
	}

	return &unstructured.Unstructured{Object: w.UnstructuredContent()}, nil
}

func renderTrait(data []byte, _ ...Parameter) (*unstructured.Unstructured, error) {
	// TODO(negz): Is there a better decoder to use here?
	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, u); err != nil {
		return nil, errors.Wrap(err, errUnmarshalTrait)
	}
	return u, nil
}

// A Parameter may be used to set the supplied paths to the supplied value.
type Parameter struct {
	// Name of this parameter.
	Name string

	// Value of this parameter.
	Value intstr.IntOrString

	// FieldPaths that should be set to this parameter's value.
	FieldPaths []string
}

// A ParameterResolver resolves the parameters accepted by a component and the
// parameter values supplied to a component into configured parameters.
type ParameterResolver interface {
	Resolve([]v1alpha2.ComponentParameter, []v1alpha2.ComponentParameterValue) ([]Parameter, error)
}

// A ParameterResolveFn resolves the parameters accepted by a component and the
// parameter values supplied to a component into configured parameters.
type ParameterResolveFn func([]v1alpha2.ComponentParameter, []v1alpha2.ComponentParameterValue) ([]Parameter, error)

// Resolve the supplied parameters.
func (fn ParameterResolveFn) Resolve(cp []v1alpha2.ComponentParameter, cpv []v1alpha2.ComponentParameterValue) ([]Parameter, error) {
	return fn(cp, cpv)
}

func resolve(cp []v1alpha2.ComponentParameter, cpv []v1alpha2.ComponentParameterValue) ([]Parameter, error) {
	supported := make(map[string]bool)
	for _, v := range cp {
		supported[v.Name] = true
	}

	set := make(map[string]*Parameter)
	for _, v := range cpv {
		if !supported[v.Name] {
			return nil, errors.Errorf(errFmtUnsupportedParam, v.Name)
		}
		set[v.Name] = &Parameter{Name: v.Name, Value: v.Value}
	}

	for _, p := range cp {
		_, ok := set[p.Name]
		if !ok && p.Required != nil && *p.Required {
			// This parameter is required, but not set.
			return nil, errors.Errorf(errFmtRequiredParam, p.Name)
		}
		if !ok {
			// This parameter is not required, and not set.
			continue
		}

		set[p.Name].FieldPaths = p.FieldPaths
	}

	params := make([]Parameter, 0, len(set))
	for _, p := range set {
		params = append(params, *p)
	}

	return params, nil
}

func addDataOutputsToDAG(dag *dag, outs []v1alpha2.DataOutput, obj *unstructured.Unstructured) {
	for _, out := range outs {
		r := &corev1.ObjectReference{
			APIVersion: obj.GetAPIVersion(),
			Kind:       obj.GetKind(),
			Name:       obj.GetName(),
			Namespace:  obj.GetNamespace(),
			FieldPath:  out.FieldPath,
		}
		dag.AddSource(out.Name, r, out.Conditions)
	}
}

func makeUnsatisfiedDependency(obj *unstructured.Unstructured, s *dagSource, in v1alpha2.DataInput,
	reason string) v1alpha2.UnstaifiedDependency {
	return v1alpha2.UnstaifiedDependency{
		Reason: reason,
		From: v1alpha2.DependencyFromObject{
			TypedReference: runtimev1alpha1.TypedReference{
				APIVersion: s.ObjectRef.APIVersion,
				Kind:       s.ObjectRef.Kind,
				Name:       s.ObjectRef.Name,
			},
			FieldPath: s.ObjectRef.FieldPath,
		},
		To: v1alpha2.DependencyToObject{
			TypedReference: runtimev1alpha1.TypedReference{
				APIVersion: obj.GetAPIVersion(),
				Kind:       obj.GetKind(),
				Name:       obj.GetName(),
			},
			FieldPaths: in.ToFieldPaths,
		},
	}
}

func (r *components) handleDataInput(ctx context.Context, ins []v1alpha2.DataInput, dag *dag, obj,
	ac *unstructured.Unstructured) ([]v1alpha2.UnstaifiedDependency, error) {
	uds := make([]v1alpha2.UnstaifiedDependency, 0)
	for _, in := range ins {
		s, ok := dag.Sources[in.ValueFrom.DataOutputName]
		if !ok {
			return nil, errors.Wrapf(ErrDataOutputNotExist, "DataOutputName (%s)", in.ValueFrom.DataOutputName)
		}
		val, ready, reason, err := r.getDataInput(ctx, s, ac)
		if err != nil {
			return nil, errors.Wrap(err, "getDataInput failed")
		}
		if !ready {
			uds = append(uds, makeUnsatisfiedDependency(obj, s, in, reason))
			return uds, nil
		}

		err = fillValue(obj, in.ToFieldPaths, val)
		if err != nil {
			return nil, errors.Wrap(err, "fillValue failed")
		}
	}
	return uds, nil
}

func fillValue(obj *unstructured.Unstructured, fs []string, val interface{}) error {
	paved := fieldpath.Pave(obj.Object)
	for _, fp := range fs {
		toSet := val

		// Special case for slcie because we will append instead of rewriting.
		if reflect.TypeOf(val).Kind() == reflect.Slice {
			raw, err := paved.GetValue(fp)
			if err != nil {
				if fieldpath.IsNotFound(err) {
					raw = make([]interface{}, 0)
				} else {
					return err
				}
			}
			l := raw.([]interface{})
			l = append(l, val.([]interface{})...)
			toSet = l
		}

		err := paved.SetValue(fp, toSet)
		if err != nil {
			return errors.Wrap(err, "paved.SetValue() failed")
		}
	}
	return nil
}

func matchValue(conds []v1alpha2.ConditionRequirement, val string, paved, ac *fieldpath.Paved) (bool, string) {
	// If no condition is specified, it is by default to check value not empty.
	if len(conds) == 0 {
		if val == "" {
			return false, errValueEmpty
		}
		return true, ""
	}

	return checkConditions(conds, paved, &val, ac)
}

func getCheckVal(m v1alpha2.ConditionRequirement, paved *fieldpath.Paved, val *string) (string, error) {
	var checkVal string
	switch {
	case m.FieldPath != "":
		return paved.GetString(m.FieldPath)
	case val != nil:
		checkVal = *val
	default:
		return "", errors.New("FieldPath not specified")
	}
	return checkVal, nil
}

func getExpectVal(m v1alpha2.ConditionRequirement, ac *fieldpath.Paved) (string, error) {
	if m.Value != "" {
		return m.Value, nil
	}
	if m.ValueFrom.FieldPath == "" || ac == nil {
		return "", nil
	}
	var err error
	value, err := ac.GetString(m.ValueFrom.FieldPath)
	if err != nil {
		return "", fmt.Errorf("get valueFrom.fieldPath fail: %w", err)
	}
	return value, nil
}

func checkConditions(conds []v1alpha2.ConditionRequirement, paved *fieldpath.Paved, val *string, ac *fieldpath.Paved) (bool, string) {
	for _, m := range conds {
		checkVal, err := getCheckVal(m, paved, val)
		if err != nil {
			return false, fmt.Sprintf("can't get value to check %v", err)
		}
		m.Value, err = getExpectVal(m, ac)
		if err != nil {
			return false, err.Error()
		}

		switch m.Operator {
		case v1alpha2.ConditionEqual:
			if m.Value != checkVal {
				return false, fmt.Sprintf("got(%v) expected to be %v", checkVal, m.Value)
			}
		case v1alpha2.ConditionNotEqual:
			if m.Value == checkVal {
				return false, fmt.Sprintf("got(%v) expected not to be %v", checkVal, m.Value)
			}
		case v1alpha2.ConditionNotEmpty:
			if checkVal == "" {
				return false, errValueEmpty
			}
		}
	}
	return true, ""
}

// GetTraitName return trait name
func getTraitName(ac *v1alpha2.ApplicationConfiguration, componentName string,
	ct *v1alpha2.ComponentTrait, t *unstructured.Unstructured, traitDef *v1alpha2.TraitDefinition) string {
	var (
		traitName  string
		apiVersion string
		kind       string
	)

	if len(t.GetName()) > 0 {
		return t.GetName()
	}

	apiVersion = t.GetAPIVersion()
	kind = t.GetKind()

	traitType := traitDef.Name
	if strings.Contains(traitType, ".") {
		traitType = strings.Split(traitType, ".")[0]
	}

	for _, w := range ac.Status.Workloads {
		if w.ComponentName != componentName {
			continue
		}
		for _, trait := range w.Traits {
			if trait.Reference.APIVersion == apiVersion && trait.Reference.Kind == kind {
				traitName = trait.Reference.Name
			}
		}
	}

	if len(traitName) == 0 {
		traitName = util.GenTraitName(componentName, ct.DeepCopy(), traitType)
	}

	return traitName
}

// getExistingWorkload tries to retrieve the currently running workload
func (r *components) getExistingWorkload(ctx context.Context, ac *v1alpha2.ApplicationConfiguration,
	c *v1alpha2.Component, w *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	var workloadName string
	existingWorkload := &unstructured.Unstructured{}
	for _, component := range ac.Status.Workloads {
		if component.ComponentName != c.GetName() {
			continue
		}
		workloadName = component.Reference.Name
	}
	if workloadName != "" {
		objectKey := client.ObjectKey{Namespace: ac.GetNamespace(), Name: workloadName}
		existingWorkload.SetAPIVersion(w.GetAPIVersion())
		existingWorkload.SetKind(w.GetKind())
		err := r.client.Get(ctx, objectKey, existingWorkload)
		if err != nil {
			return nil, client.IgnoreNotFound(err)
		}
	}
	return existingWorkload, nil
}
