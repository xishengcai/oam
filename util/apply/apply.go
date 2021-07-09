package apply

import (
	"context"

	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/oam-dev/kubevela/pkg/oam"
)

// Applicator applies new state to an object or create it if not exist.
// It uses the same mechanism as `kubectl apply`, that is, for each resource being applied,
// computing a three-way diff merge in client side based on its current state, modified stated,
// and last-applied-state which is tracked through an specific annotation.
// If the resource doesn't exist before, Apply will create it.
type Applicator interface {
	Apply(context.Context, runtime.Object, ...ApplyOption) error
}

// ApplyOption is called before applying state to the object.
// ApplyOption is still called even if the object does NOT exist.
// If the object does not exist, `existing` will be assigned as `nil`.
// nolint: golint
type ApplyOption func(ctx context.Context, existing, desired runtime.Object) error

// NewAPIApplicator creates an Applicator that applies state to an
// object or creates the object if not exist.
func NewAPIApplicator(c client.Client) *APIApplicator {
	return &APIApplicator{
		creator: creatorFn(createOrGetExisting),
		patcher: patcherFn(threeWayMergePatch),
		c:       c,
	}
}

type creator interface {
	createOrGetExisting(context.Context, client.Client, runtime.Object, ...ApplyOption) (runtime.Object, error)
}

type creatorFn func(context.Context, client.Client, runtime.Object, ...ApplyOption) (runtime.Object, error)

func (fn creatorFn) createOrGetExisting(ctx context.Context, c client.Client, o runtime.Object, ao ...ApplyOption) (runtime.Object, error) {
	return fn(ctx, c, o, ao...)
}

type patcher interface {
	patch(c, m runtime.Object) (client.Patch, error)
}

type patcherFn func(c, m runtime.Object) (client.Patch, error)

func (fn patcherFn) patch(c, m runtime.Object) (client.Patch, error) {
	return fn(c, m)
}

// APIApplicator implements Applicator
type APIApplicator struct {
	creator
	patcher
	c client.Client
}

// Apply applies new state to an object or create it if not exist
func (a *APIApplicator) Apply(ctx context.Context, desired runtime.Object, ao ...ApplyOption) error {
	existing, err := a.createOrGetExisting(ctx, a.c, desired, ao...)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}

	// the object already exists, apply new state
	if err = executeApplyOptions(ctx, existing, desired, ao); err != nil {
		return err
	}
	//loggingApply("patching object", desired)
	patch, err := a.patcher.patch(existing, desired)
	if err != nil {
		return errors.Wrap(err, "cannot calculate patch by computing a three way diff")
	}
	return errors.Wrapf(a.c.Patch(ctx, desired, patch), "cannot patch object")
}

// createOrGetExisting will create the object if it does not exist
// or get and return the existing object
func createOrGetExisting(ctx context.Context, c client.Client, desired runtime.Object, ao ...ApplyOption) (runtime.Object, error) {
	m, ok := desired.(oam.Object)
	if !ok {
		return nil, errors.New("cannot access object metadata")
	}

	var create = func() (runtime.Object, error) {
		// execute ApplyOptions even the object doesn't exist
		if err := executeApplyOptions(ctx, nil, desired, ao); err != nil {
			return nil, err
		}
		if err := addLastAppliedConfigAnnotation(desired); err != nil {
			return nil, err
		}
		//loggingApply("creating object", desired)
		return nil, errors.Wrap(c.Create(ctx, desired), "cannot create object")
	}

	// allow to create object with only generateName
	if m.GetName() == "" && m.GetGenerateName() != "" {
		return create()
	}

	existing := &unstructured.Unstructured{}
	existing.GetObjectKind().SetGroupVersionKind(desired.GetObjectKind().GroupVersionKind())
	err := c.Get(ctx, types.NamespacedName{Name: m.GetName(), Namespace: m.GetNamespace()}, existing)
	if kerrors.IsNotFound(err) {
		return create()
	}
	if err != nil {
		return nil, errors.Wrap(err, "cannot get object")
	}
	return existing, nil
}

func executeApplyOptions(ctx context.Context, existing, desired runtime.Object, aos []ApplyOption) error {
	// if existing is nil, it means the object is going to be created.
	// ApplyOption function should handle this situation carefully by itself.
	for _, fn := range aos {
		if err := fn(ctx, existing, desired); err != nil {
			return errors.Wrap(err, "cannot apply ApplyOption")
		}
	}
	return nil
}

// MustBeControllableBy requires that the existing object is controllable by an
// object with the supplied UID. An object is controllable if its controller
// reference matches the supplied UID, or it has no controller reference. An
// error will be returned if the current object cannot be controlled by the
// supplied UID.
// ACKNOWLEDGMENTS: The code was based in part on the source code of
// - github.com/crossplane/crossplane-runtime/pkg/resource/resource.go#L274
func MustBeControllableBy(u types.UID) ApplyOption {
	return func(_ context.Context, existing, _ runtime.Object) error {
		if existing == nil {
			return nil
		}
		c := metav1.GetControllerOf(existing.(metav1.Object))
		if c == nil {
			return nil
		}

		if c.UID != u {
			return errors.Errorf("existing object is not controlled by UID %q", u)
		}
		return nil
	}
}
