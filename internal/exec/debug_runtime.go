package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	debugPlanLease struct {
		parent *session
		once   sync.Once
	}

	// DebugRuntime augments the common execution runtime with Ferret's debugger
	// capability and owns the matching debug-Plan lease.
	DebugRuntime struct {
		runtime  *executionRuntime
		debugger *ferret.DebugSession
		lease    debugPlanLease
	}
)

func newDebugRuntime(
	runtime *executionRuntime,
	debugger *ferret.DebugSession,
	parent *session,
) *DebugRuntime {
	return &DebugRuntime{
		runtime:  runtime,
		debugger: debugger,
		lease:    debugPlanLease{parent: parent},
	}
}

// SessionID returns the parent executable Session identifier.
func (r *DebugRuntime) SessionID() SessionID {
	return r.runtime.target.sessionID
}

// Context returns the manager-owned context for debugger execution commands.
func (r *DebugRuntime) Context() context.Context {
	return r.runtime.ctx
}

// Debugger returns the Ferret debugger capability for debugger-specific
// commands. The common execution runtime retains ownership of the underlying
// Ferret session.
func (r *DebugRuntime) Debugger() *ferret.DebugSession {
	return r.debugger
}

// Parameters returns an independent copy of the retained caller parameters.
func (r *DebugRuntime) Parameters() Parameters {
	return r.runtime.parameters()
}

// Options returns the normalized runtime options.
func (r *DebugRuntime) Options() RuntimeOptions {
	return r.runtime.options()
}

// MaterializeOutput copies one Ferret result into daemon-owned runtime output.
func (r *DebugRuntime) MaterializeOutput(output *ferret.Output) *RuntimeOutput {
	return r.runtime.materializeOutput(output)
}

// MaterializeFailure converts an error to durable source-aware runtime failure details.
func (r *DebugRuntime) MaterializeFailure(err error) *RuntimeFailure {
	return r.runtime.materializeFailure(err)
}

// Close cancels the common execution runtime, closes the Ferret debugger session
// owned by that runtime, and releases the debug-Plan lease. Concurrent callers
// observe the same session close result.
func (r *DebugRuntime) Close() error {
	r.runtime.cancel(errExecutionCanceled)
	err := r.runtime.closeSession()
	r.lease.release()

	return err
}

func (l *debugPlanLease) release() {
	l.once.Do(l.parent.releaseDebugRuntime)
}

// CreateDebugRuntime prepares and creates one debugger-capable runtime for an
// executable Session.
func (m *Manager) CreateDebugRuntime(
	ctx context.Context,
	id SessionID,
	parameters Parameters,
	options RuntimeOptions,
) (*DebugRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	input, err := newRuntimeInput(parameters, options)
	if err != nil {
		return nil, err
	}

	creation, err := m.sessions.beginRuntimeCreate(id)
	if err != nil {
		return nil, err
	}
	defer creation.finish()

	parent := creation.session()
	target, err := parent.acquireDebugRuntimeTarget(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, ErrSessionClosed) || errors.Is(err, workspace.ErrClosed) ||
			errors.Is(err, workspace.ErrDocumentNotFound) {

			return nil, err
		}

		return nil, &CompilationError{
			Source:      parent.source,
			Diagnostics: diagnostic.FromError(parent.source.URI, parent.text, err),
			Cause:       err,
		}
	}

	runtime := newExecutionRuntime(target, input)
	debugger, err := target.plan.NewDebugSession(ctx, runtime.sessionOptions()...)
	if err != nil {
		runtime.cancel(errExecutionCanceled)
		parent.releaseDebugRuntime()

		return nil, err
	}
	runtime.ferretSession = debugger

	return newDebugRuntime(runtime, debugger, parent), nil
}
