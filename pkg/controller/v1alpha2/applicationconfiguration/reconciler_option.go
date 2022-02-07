package applicationconfiguration

import (
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/event"
)

// A ReconcilerOption configures a Reconciler.
type ReconcilerOption func(*OAMApplicationReconciler)

// WithRecorder specifies how the Reconciler should record events.
func WithRecorder(er event.Recorder) ReconcilerOption {
	return func(r *OAMApplicationReconciler) {
		r.record = er
	}
}

// WithApplyOnceOnly indicates whether workloads and traits should be
// affected if no spec change is made in the ApplicationConfiguration.
func WithApplyOnceOnly(applyOnceOnly bool) ReconcilerOption {
	return func(r *OAMApplicationReconciler) {
		r.applyOnceOnly = applyOnceOnly
	}
}

// WithSetSync set Reconciler exec interval
func WithSetSync(syncTime time.Duration) ReconcilerOption {
	return func(r *OAMApplicationReconciler) {
		r.syncTime = syncTime
	}
}
