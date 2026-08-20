package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// DebugTarget is a leased immutable debug compilation for one Session.
type DebugTarget struct {
	session SessionID
	source  workspace.SourceSnapshot
	text    string
	plan    *ferret.Plan
	release func()
	once    sync.Once
}

func newDebugTarget(
	session SessionID,
	source workspace.SourceSnapshot,
	text string,
	plan *ferret.Plan,
	release func(),
) *DebugTarget {
	return &DebugTarget{
		session: session,
		source:  source,
		text:    text,
		plan:    plan,
		release: release,
	}
}

// SessionID returns the parent executable Session identifier.
func (t *DebugTarget) SessionID() SessionID {
	return t.session
}

// Source returns the immutable source identity used for compilation.
func (t *DebugTarget) Source() workspace.SourceSnapshot {
	return t.source
}

// SourceText returns the immutable source contents used for compilation.
func (t *DebugTarget) SourceText() string {
	return t.text
}

// NewDebugSession creates one Ferret debugger session from the leased Plan.
func (t *DebugTarget) NewDebugSession(
	ctx context.Context,
	options ...ferret.SessionOption,
) (*ferret.DebugSession, error) {
	return t.plan.NewDebugSession(ctx, options...)
}

// Release idempotently releases the target's Plan lifetime lease.
func (t *DebugTarget) Release() {
	t.once.Do(t.release)
}

// AcquireDebugTarget lazily compiles and leases a Session's matching debug Plan.
func (m *Manager) AcquireDebugTarget(ctx context.Context, id SessionID) (*DebugTarget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	session, err := m.sessions.sessionForDebug(id)
	if err != nil {
		return nil, err
	}

	target, err := session.acquireDebugTarget(ctx)
	if err == nil {
		return target, nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrSessionClosed) || errors.Is(err, workspace.ErrClosed) ||
		errors.Is(err, workspace.ErrDocumentNotFound) {
		return nil, err
	}

	return nil, &CompilationError{
		Source:      session.source,
		Diagnostics: diagnostic.FromError(session.source.URI, session.text, err),
		Cause:       err,
	}
}
