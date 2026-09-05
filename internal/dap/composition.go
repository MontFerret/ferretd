package dap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2"

	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/ferretapi"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// New creates a single-session DAP server over non-nil input and output using
// the supplied options. It returns an error when either stream is nil or its
// owned service graph cannot be constructed.
func New(input io.Reader, output io.Writer, options Options) (*Server, error) {
	if input == nil {
		return nil, errNilInput
	}

	if output == nil {
		return nil, errNilOutput
	}

	options = options.normalized()

	engine, err := ferret.New()
	if err != nil {
		return nil, fmt.Errorf("create runtime: %w", err)
	}

	runtime := ferretapi.New(engine)

	return newServer(input, output, options, runtime)
}

func newServer(input io.Reader, output io.Writer, options Options, runtime api.Runtime) (*Server, error) {
	workspaces := workspace.New()

	executions, err := exec.New(workspaces, runtime)
	if err != nil {
		cleanupErr := errors.Join(workspaces.Clear(context.Background()), runtime.Close())

		return nil, errors.Join(fmt.Errorf("create execution manager: %w", err), cleanupErr)
	}

	debugs, err := debug.New(executions)
	if err != nil {
		ctx := context.Background()
		cleanupErr := errors.Join(executions.Close(ctx), workspaces.Clear(ctx), runtime.Close())

		return nil, errors.Join(fmt.Errorf("create debug manager: %w", err), cleanupErr)
	}

	readerClose, _ := input.(io.Closer)

	return &Server{
		reader:                    bufio.NewReader(input),
		readerClose:               readerClose,
		writer:                    output,
		workspaces:                workspaces,
		executions:                executions,
		debugs:                    debugs,
		runtime:                   runtime,
		logger:                    options.Logger.With().Str("component", "dap").Logger(),
		handles:                   newHandleTable(),
		nextBreakpointID:          1,
		stableBreakpoints:         make(map[breakpointKey]int),
		debuggerBreakpoints:       make(map[apidebugger.BreakpointID]int),
		debuggerBreakpointSources: make(map[apidebugger.BreakpointID]string),
		client: clientOptions{
			pathFormat:      pathFormatPath,
			linesStartAt1:   true,
			columnsStartAt1: true,
		},
	}, nil
}
