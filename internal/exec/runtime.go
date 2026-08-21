package exec

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MontFerret/ferret/v2"
	ferretruntime "github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferretd/internal/diagnostic"
	daemonparams "github.com/MontFerret/ferretd/internal/params"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	runtimeTarget struct {
		session SessionID
		source  workspace.SourceSnapshot
		text    string
		plan    *ferret.Plan
	}

	runtimeInput struct {
		values     ferretruntime.Params
		parameters map[string]any
		options    RuntimeOptions
	}

	// executionRuntime owns the daemon-level state and resources shared by
	// ordinary and debugger execution. Observable lifecycle state remains with
	// Execution or debug.Session.
	executionRuntime struct {
		target runtimeTarget
		input  runtimeInput

		ctx           context.Context
		cancel        context.CancelCauseFunc
		ferretSession ferretSession
		closeOnce     sync.Once
		closeErr      error
	}

	// ferretSession is the common owned-resource contract implemented by
	// Ferret's ordinary and debugger sessions.
	ferretSession interface {
		Close() error
	}

	runtimeRunResult struct {
		output   *RuntimeOutput
		err      error
		category FailureCategory
	}
)

func newRuntimeInput(parameters map[string]any, options RuntimeOptions) (runtimeInput, error) {
	values, retained, err := daemonparams.Prepare(parameters)
	if err != nil {
		return runtimeInput{}, fmt.Errorf("%w: %v", ErrInvalidParameters, err)
	}

	return runtimeInput{
		values:     values,
		parameters: retained,
		options:    options.normalized(),
	}, nil
}

func newExecutionRuntime(target runtimeTarget, input runtimeInput) *executionRuntime {
	ctx, cancel := context.WithCancelCause(context.Background())

	return &executionRuntime{
		target: target,
		input:  input,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (r *executionRuntime) run() runtimeRunResult {
	session, err := r.target.plan.NewSession(r.ctx, r.sessionOptions()...)
	if err != nil {
		return runtimeRunResult{err: err, category: FailureSessionCreation}
	}
	r.ferretSession = session

	output, runErr := session.Run(r.ctx)
	closeErr := r.closeSession()
	result := runtimeRunResult{
		output:   r.output(output),
		err:      errors.Join(runErr, closeErr),
		category: FailureRuntime,
	}

	if runErr == nil && closeErr != nil {
		result.category = FailureCleanup
	}

	return result
}

func (r *executionRuntime) sessionOptions() []ferret.SessionOption {
	options := []ferret.SessionOption{ferret.WithSessionRuntimeParams(r.input.values)}
	if r.input.options.OutputContentType != "" {
		options = append(options, ferret.WithOutputContentType(r.input.options.OutputContentType))
	}

	return options
}

func (r *executionRuntime) closeSession() error {
	r.closeOnce.Do(func() {
		if r.ferretSession != nil {
			r.closeErr = r.ferretSession.Close()
		}
	})

	return r.closeErr
}

func (r *executionRuntime) failure(err error) *RuntimeFailure {
	if err == nil {
		return nil
	}

	return &RuntimeFailure{
		Message:     err.Error(),
		Diagnostics: diagnostic.FromError(r.target.source.URI, r.target.text, err),
	}
}

func (r *executionRuntime) output(output *ferret.Output) *RuntimeOutput {
	if output == nil {
		return nil
	}

	return &RuntimeOutput{
		ContentType: output.ContentType,
		Content:     append([]byte(nil), output.Content...),
	}
}

func (r *executionRuntime) parameters() map[string]any {
	return daemonparams.Clone(r.input.parameters)
}

func (r *executionRuntime) options() RuntimeOptions {
	return r.input.options
}
