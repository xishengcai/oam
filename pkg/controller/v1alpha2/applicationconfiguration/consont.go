package applicationconfiguration

import "time"

const (
	reconcileTimeout = 1 * time.Minute
	dependCheckWait  = 10 * time.Second
	shortWait        = 30 * time.Second
)

// Reconcile error strings.
const (
	errGetAppConfig          = "cannot get application configuration"
	errUpdateAppConfigStatus = "cannot update application configuration status"
	errExecutePrehooks       = "failed to execute pre-hooks"
	errExecutePosthooks      = "failed to execute post-hooks"
	errRenderComponents      = "cannot render components"
	errApplyComponents       = "cannot apply components"
	errGCComponent           = "cannot garbage collect components"
	errFinalizeWorkloads     = "failed to finalize workloads"
)

// Reconcile event reasons.
const (
	reasonRenderComponents        = "RenderedComponents"
	reasonExecutePrehook          = "ExecutePrehook"
	reasonExecutePosthook         = "ExecutePosthook"
	reasonApplyComponents         = "AppliedComponents"
	reasonGGComponent             = "GarbageCollectedComponent"
	reasonCannotExecutePrehooks   = "CannotExecutePrehooks"
	reasonCannotExecutePosthooks  = "CannotExecutePosthooks"
	reasonCannotRenderComponents  = "CannotRenderComponents"
	reasonCannotApplyComponents   = "CannotApplyComponents"
	reasonCannotGGComponents      = "CannotGarbageCollectComponents"
	reasonCannotFinalizeWorkloads = "CannotFinalizeWorkloads"
)

// Render error strings.
const (
	errUnmarshalWorkload = "cannot unmarshal workload"
	errUnmarshalTrait    = "cannot unmarshal trait"
	errValueEmpty        = "value should not be empty"
)

// Render error format strings.
const (
	errFmtGetScope         = "cannot get scope %q"
	errFmtResolveParams    = "cannot resolve parameter values for component %q"
	errFmtRenderWorkload   = "cannot render workload for component %q"
	errFmtRenderTrait      = "cannot render trait for component %q"
	errFmtSetParam         = "cannot set parameter %q"
	errFmtUnsupportedParam = "unsupported parameter %q"
	errFmtRequiredParam    = "required parameter %q not specified"
	errFmtCompRevision     = "cannot get latest revision for component %q while revision is enabled"
	errSetValueForField    = "can not set value %q for fieldPath %q"
)
