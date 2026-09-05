package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2"

	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/ferretapi"
	grpcadapter "github.com/MontFerret/ferretd/internal/grpc"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// New constructs a daemon and its service boundaries.
func New(options Options) (*Daemon, error) {
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}

	engine, err := ferret.New()
	if err != nil {
		return nil, fmt.Errorf("create runtime: %w", err)
	}
	runtime := ferretapi.New(engine)

	return newDaemon(options, runtime)
}

func newDaemon(options Options, runtime api.Runtime) (*Daemon, error) {
	instanceID, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("generate daemon instance ID: %w", err),
			runtime.Close(),
		)
	}

	workspaceManager := workspace.New()
	executionManager, err := exec.New(workspaceManager, runtime)
	if err != nil {
		cleanupErr := errors.Join(workspaceManager.Clear(context.Background()), runtime.Close())

		return nil, errors.Join(fmt.Errorf("create execution manager: %w", err), cleanupErr)
	}

	result := &Daemon{
		workspaces: workspaceManager,
		executions: executionManager,
		runtime:    runtime,
		endpoint:   options.Endpoint,
		version:    options.Version,
		logger:     *options.Logger,
		shutdown:   make(chan struct{}),
		stopDone:   make(chan struct{}),
		state:      stateNew,
	}
	grpcServer, err := grpcadapter.New(
		result.workspaces,
		result.executions,
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
			result.runtime.Close(),
		)

		return nil, errors.Join(fmt.Errorf("create gRPC server: %w", err), cleanupErr)
	}

	result.grpc = grpcServer

	return result, nil
}
