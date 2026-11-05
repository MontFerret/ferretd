package dap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2/uapi"

	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// New creates a single-session DAP server over non-nil input and output using
// the supplied options. It returns an error when either stream is nil or its
// owned service graph cannot be constructed.
func New(input io.Reader, output io.Writer, options Options) (*Server, error) {
	return bootstrap(input, output, options, func() (api.Runtime, error) { return uapi.New() })
}

func bootstrap(input io.Reader, output io.Writer, options Options, createRuntime runtimes.Factory) (*Server, error) {
	if input == nil {
		return nil, errNilInput
	}

	if output == nil {
		return nil, errNilOutput
	}

	catalog, err := runtimes.NewCatalog(options.RuntimeConfig)
	if err != nil {
		return nil, err
	}

	definition, err := catalog.Resolve("")
	if err != nil {
		return nil, err
	}

	// DAP launch selection does not yet support Wire defaults.
	if definition.Provider != runtimes.ProviderNative {
		return nil, fmt.Errorf("%w: %q requires Wire DAP support", runtimes.ErrUnsupportedDefault, definition.Name)
	}

	options = options.normalized()
	runtime, err := createRuntime()
	if err != nil {
		return nil, fmt.Errorf("create runtime: %w", err)
	}

	return newServer(input, output, options, runtime, catalog)
}

func newServer(input io.Reader, output io.Writer, options Options, runtime api.Runtime, catalog *runtimes.Catalog) (*Server, error) {
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
		reader:      bufio.NewReader(input),
		readerClose: readerClose,
		writer:      output,
		workspaces:  workspaces,
		executions:  executions,
		debugs:      debugs,
		runtime:     runtime,
		catalog:     catalog,
		logger:      options.Logger.With().Str("component", "dap").Logger(),
		handles:     newHandleTable(),
		breakpoints: newBreakpointIDs(),
		client: clientOptions{
			pathFormat:      pathFormatPath,
			linesStartAt1:   true,
			columnsStartAt1: true,
		},
	}, nil
}
