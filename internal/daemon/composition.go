package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2/uapi"

	"github.com/MontFerret/ferretd/internal/exec"
	grpcadapter "github.com/MontFerret/ferretd/internal/grpc"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// New constructs a daemon and its service boundaries.
func New(options Options) (*Daemon, error) {
	return bootstrap(options, func() (api.Runtime, error) { return uapi.New(runtimes.NativeVersion) })
}

func bootstrap(options Options, createRuntime runtimes.Factory) (*Daemon, error) {
	catalog, err := runtimes.NewCatalog(options.RuntimeConfig)
	if err != nil {
		return nil, err
	}

	options, err = options.normalized()
	if err != nil {
		return nil, err
	}

	runtime, err := createRuntime()
	if err != nil {
		if runtime != nil {
			err = errors.Join(err, runtime.Close())
		}

		return nil, fmt.Errorf("create runtime: %w", err)
	}

	return newDaemon(options, runtime, catalog)
}

// newDaemon consumes runtime ownership, including on construction failure.
func newDaemon(options Options, runtime api.Runtime, catalog *runtimes.Catalog) (*Daemon, error) {
	owner, err := runtimes.NewOwner(catalog, runtime)
	if err != nil {
		if runtime != nil {
			err = errors.Join(err, runtime.Close())
		}

		return nil, err
	}

	instanceID, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("generate daemon instance ID: %w", err),
			owner.Close(context.Background()),
		)
	}

	workspaceManager := workspace.New()

	executionManager, err := exec.New(workspaceManager, owner)
	if err != nil {
		cleanupErr := errors.Join(workspaceManager.Clear(context.Background()), owner.Close(context.Background()))

		return nil, errors.Join(fmt.Errorf("create execution manager: %w", err), cleanupErr)
	}

	result := &Daemon{
		workspaces:   workspaceManager,
		executions:   executionManager,
		runtimeOwner: owner,
		endpoint:     options.Endpoint,
		version:      options.Version,
		logger:       *options.Logger,
		shutdown:     make(chan struct{}),
		stopDone:     make(chan struct{}),
		state:        stateNew,
	}

	grpcServer, err := grpcadapter.New(
		result.workspaces,
		result.executions,
		catalog,
		options.Version,
		instanceID.String(),
		result.requestShutdown,
		grpcadapter.Options{BearerToken: options.BearerToken},
	)
	if err != nil {
		ctx := context.Background()
		cleanupErr := errors.Join(
			result.executions.Close(ctx),
			result.workspaces.Clear(ctx),
			result.runtimeOwner.Close(ctx),
		)

		return nil, errors.Join(fmt.Errorf("create gRPC server: %w", err), cleanupErr)
	}

	result.grpc = grpcServer

	return result, nil
}
