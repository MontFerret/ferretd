package client

type (
	// SessionID is an opaque daemon execution-session identifier.
	SessionID string

	// ExecutionID is an opaque daemon execution identifier.
	ExecutionID string

	// SourceSnapshot identifies the immutable workspace source compiled into a Session.
	SourceSnapshot struct {
		WorkspaceID  string
		RelativePath string
		URI          string
		Revision     uint64
	}

	// Session is an immutable daemon execution-session snapshot.
	Session struct {
		ID         SessionID
		Source     SourceSnapshot
		Parameters []string
	}

	// CreateSessionRequest selects one eligible workspace document to discover or
	// refresh from disk and compile.
	CreateSessionRequest struct {
		WorkspaceID  string
		RelativePath string
	}

	// ExecutionState identifies an Execution lifecycle state.
	ExecutionState uint8

	// ExecutionOptions contains one-shot invocation settings.
	ExecutionOptions struct {
		OutputContentType string
	}

	// CreateExecutionRequest creates one isolated invocation from a compiled Session.
	CreateExecutionRequest struct {
		SessionID  SessionID
		Parameters map[string]any
		Options    ExecutionOptions
	}

	// Execution is an immutable daemon Execution snapshot.
	Execution struct {
		ID         ExecutionID
		SessionID  SessionID
		State      ExecutionState
		Parameters map[string]any
		Options    ExecutionOptions
		Output     *ExecutionOutput
		Failure    *ExecutionFailure
	}

	// ExecutionOutput is an encoded Ferret result.
	ExecutionOutput struct {
		ContentType string
		Data        []byte
	}

	// FailureCategory identifies the phase that failed.
	FailureCategory uint8

	// ExecutionFailure describes a terminal failed Execution.
	ExecutionFailure struct {
		Category    FailureCategory
		Message     string
		Diagnostics []Diagnostic
	}

	// DiagnosticSeverity identifies the importance of a diagnostic.
	DiagnosticSeverity uint8

	// Position is a zero-based UTF-16 source position.
	Position struct {
		Line      uint32
		Character uint32
	}

	// Range is a half-open UTF-16 source range.
	Range struct {
		Start Position
		End   Position
	}

	// RelatedInformation identifies a source location related to a Diagnostic.
	RelatedInformation struct {
		URI     string
		Range   Range
		Message string
	}

	// Diagnostic is a protocol-neutral Ferret diagnostic projection.
	Diagnostic struct {
		URI                string
		Range              Range
		Severity           DiagnosticSeverity
		Code               string
		Source             string
		Message            string
		RelatedInformation []RelatedInformation
	}

	// ExecutionEventKind identifies a typed lifecycle event payload.
	ExecutionEventKind uint8

	// ExecutionEvent is one ordered observation for an Execution.
	ExecutionEvent struct {
		ExecutionID ExecutionID
		Sequence    uint64
		Kind        ExecutionEventKind
		Execution   Execution
	}
)

const (
	// ExecutionStateCreated identifies an Execution that has not started.
	ExecutionStateCreated ExecutionState = iota + 1
	// ExecutionStateRunning identifies an active Execution.
	ExecutionStateRunning
	// ExecutionStateCompleted identifies a successful Execution.
	ExecutionStateCompleted
	// ExecutionStateFailed identifies a failed Execution.
	ExecutionStateFailed
	// ExecutionStateCancelled identifies an Execution stopped through cancellation.
	ExecutionStateCancelled
)

const (
	// FailureCategorySessionCreation identifies runtime Session construction failure.
	FailureCategorySessionCreation FailureCategory = iota + 1
	// FailureCategoryRuntime identifies Ferret execution failure.
	FailureCategoryRuntime
	// FailureCategoryCleanup identifies runtime Session cleanup failure.
	FailureCategoryCleanup
)

const (
	// DiagnosticSeverityError identifies an error diagnostic.
	DiagnosticSeverityError DiagnosticSeverity = iota + 1
)

const (
	// ExecutionEventCreated reports creation of an Execution resource.
	ExecutionEventCreated ExecutionEventKind = iota + 1
	// ExecutionEventStarted reports the start of an invocation.
	ExecutionEventStarted
	// ExecutionEventCompleted reports successful termination.
	ExecutionEventCompleted
	// ExecutionEventFailed reports failed termination.
	ExecutionEventFailed
	// ExecutionEventCancelled reports cancellation termination.
	ExecutionEventCancelled
)

// Terminal reports whether state can no longer transition.
func (s ExecutionState) Terminal() bool {
	return s == ExecutionStateCompleted || s == ExecutionStateFailed || s == ExecutionStateCancelled
}
