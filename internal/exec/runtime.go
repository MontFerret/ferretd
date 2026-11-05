package exec

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	runtimeTarget struct {
		sessionID SessionID
		source    workspace.SourceSnapshot
		text      string
		fsRoot    string
		plan      api.Plan
	}

	runtimeInput struct {
		parameters Parameters
		options    RuntimeOptions
	}

	// executionRuntime owns the daemon-level state and resources shared by
	// ordinary and debugger execution. Observable lifecycle state remains with
	// Execution or debug.Session.
	executionRuntime struct {
		target runtimeTarget
		input  runtimeInput

		ctx       context.Context
		cancel    context.CancelCauseFunc
		session   io.Closer
		output    api.Output
		closeOnce sync.Once
		closeErr  error
	}

	runtimeRunResult struct {
		output   *api.Content
		err      error
		category FailureCategory
	}
)

func newRuntimeInput(parameters Parameters, options RuntimeOptions) (runtimeInput, error) {
	normalizedOptions, err := options.normalized()
	if err != nil {
		return runtimeInput{}, err
	}

	return runtimeInput{
		parameters: parameters.Clone(),
		options:    normalizedOptions,
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
		r.session = session

		return runtimeRunResult{
			err:      errors.Join(err, r.closeResources()),
			category: FailureSessionCreation,
		}
	}

	if session == nil {
		return runtimeRunResult{
			err:      errors.New("runtime returned no session"),
			category: FailureSessionCreation,
		}
	}

	r.session = session

	output, runErr := session.Run(r.ctx)
	r.output = output

	var content *api.Content

	if runErr == nil {
		if output == nil {
			runErr = errors.New("runtime returned no output")
		} else {
			content, runErr = output.Collect(r.ctx)
		}
	}

	closeErr := r.closeResources()
	result := runtimeRunResult{
		output:   content,
		err:      errors.Join(runErr, closeErr),
		category: FailureRuntime,
	}

	if runErr == nil && closeErr != nil {
		result.category = FailureCleanup
	}

	return result
}

func (r *executionRuntime) sessionOptions() []api.SessionOption {
	options := []api.SessionOption{
		api.WithParams(map[string]any(r.input.parameters.Clone())),
		api.WithOutputContentType(r.input.options.OutputContentType),
	}

	fsRoot := r.target.fsRoot
	if r.input.options.WorkingDirectory != "" {
		fsRoot = r.input.options.WorkingDirectory
	}

	if fsRoot != "" {
		options = append(options, api.WithFSRoot(fsRoot))
	}

	return options
}

// closeResources settles output before the caller-owned session. Ordinary runs
// invoke it after collection; their close workers wait for runDone before
// reentering. Debug runtimes have no live output handle.
func (r *executionRuntime) closeResources() error {
	r.closeOnce.Do(func() {
		if r.output != nil {
			r.closeErr = r.output.Close()
			r.output = nil
		}

		if r.session != nil {
			r.closeErr = errors.Join(r.closeErr, r.session.Close())
		}
	})

	return r.closeErr
}

func (r *executionRuntime) materializeFailure(err error) *RuntimeFailure {
	if err == nil {
		return nil
	}

	return &RuntimeFailure{
		Message:     err.Error(),
		Diagnostics: diagnostic.FromError(r.target.source.URI, r.target.text, err),
	}
}

func (r *executionRuntime) parameters() Parameters {
	return r.input.parameters.Clone()
}

func (r *executionRuntime) options() RuntimeOptions {
	return r.input.options
}
