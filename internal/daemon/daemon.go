// Package daemon coordinates the long-running ferretd services.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/google/uuid"

	"github.com/MontFerret/ferretd/internal/exec"
	grpcadapter "github.com/MontFerret/ferretd/internal/grpc"
	"github.com/MontFerret/ferretd/internal/transport"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Daemon owns the services and lifecycle that make up ferretd.
type Daemon struct {
	workspaces *workspace.Manager
	executions *exec.Manager
	grpc       *grpcadapter.Server

	endpoint transport.Endpoint
	version  string
	logger   *slog.Logger
	shutdown chan struct{}
	stopDone chan struct{}

	mu           sync.Mutex
	state        lifecycleState
	listener     net.Listener
	stopErr      error
	shutdownOnce sync.Once
}

// New constructs a daemon and its service boundaries.
func New(options Options) (*Daemon, error) {
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}

	instanceID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("generate daemon instance ID: %w", err)
	}

	workspaceManager := workspace.New()
	executionManager, err := exec.New(workspaceManager)
	if err != nil {
		cleanupErr := workspaceManager.Clear(context.Background())

		return nil, errors.Join(fmt.Errorf("create execution manager: %w", err), cleanupErr)
	}

	result := &Daemon{
		workspaces: workspaceManager,
		executions: executionManager,
		endpoint:   options.Endpoint,
		version:    options.Version,
		logger:     options.Logger,
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
	)

	if err != nil {
		ctx := context.Background()
		cleanupErr := errors.Join(result.executions.Close(ctx), result.workspaces.Clear(ctx))

		return nil, errors.Join(fmt.Errorf("create gRPC server: %w", err), cleanupErr)
	}

	result.grpc = grpcServer

	return result, nil
}

// Start serves the daemon until cancellation, RPC shutdown, or transport failure.
func (d *Daemon) Start(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}

	d.mu.Lock()
	if d.state != stateNew {
		d.mu.Unlock()

		return errors.New("daemon has already been started")
	}
	d.state = stateStarting
	d.mu.Unlock()

	listener, err := transport.Listen(d.endpoint)
	if err != nil {
		d.finishStartupFailure()

		return fmt.Errorf("listen for daemon connections: %w", err)
	}

	d.mu.Lock()
	if d.state == stateStopping {
		d.mu.Unlock()

		closeErr := listener.Close()
		executionErr := d.executions.Close(context.Background())
		workspaceErr := d.workspaces.Clear(context.Background())
		d.finishStop(errors.Join(executionErr, workspaceErr, closeErr))

		return nil
	}

	d.listener = listener
	d.state = stateRunning
	d.grpc.SetServing()
	d.mu.Unlock()

	d.logger.Info("ferretd started", "endpoint", d.endpoint.String(), "version", d.version)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- d.grpc.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		return nil
	case <-d.shutdown:
		return nil
	case serveErr := <-serveDone:
		if serveErr == nil || grpcadapter.IsStoppedError(serveErr) {
			return nil
		}

		return fmt.Errorf("serve gRPC: %w", serveErr)
	}
}

// Stop gracefully stops the daemon. It is safe to call more than once.
func (d *Daemon) Stop(ctx context.Context) error {
	d.mu.Lock()
	switch d.state {
	case stateNew:
		d.state = stateStopping
		d.mu.Unlock()

		executionErr := d.executions.Close(ctx)
		workspaceErr := d.workspaces.Clear(ctx)
		d.finishStop(errors.Join(executionErr, workspaceErr))

		return d.stopResult()
	case stateStarting:
		d.state = stateStopping
		stopDone := d.stopDone
		d.mu.Unlock()

		return d.waitForStop(ctx, stopDone)
	case stateRunning:
		d.state = stateStopping
		listener := d.listener
		d.mu.Unlock()

		d.grpc.SetNotServing()
		executionErr := d.executions.Close(ctx)
		workspaceErr := d.workspaces.Clear(ctx)
		stopErr := d.grpc.Stop(ctx)
		closeErr := listener.Close()
		d.finishStop(errors.Join(executionErr, workspaceErr, stopErr, closeErr))

		return d.stopResult()
	case stateStopping:
		stopDone := d.stopDone
		d.mu.Unlock()

		return d.waitForStop(ctx, stopDone)
	case stateStopped:
		result := d.stopErr
		d.mu.Unlock()

		return result
	default:
		d.mu.Unlock()

		return errors.New("invalid daemon lifecycle state")
	}
}

func (d *Daemon) requestShutdown() {
	d.shutdownOnce.Do(func() {
		close(d.shutdown)
	})
}

func (d *Daemon) finishStartupFailure() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.state = stateStopped
	close(d.stopDone)
}

func (d *Daemon) finishStop(err error) {
	d.mu.Lock()
	if d.state == stateStopped {
		d.mu.Unlock()

		return
	}

	d.stopErr = err
	d.state = stateStopped
	close(d.stopDone)
	d.mu.Unlock()

	if err != nil {
		d.logger.Error("ferretd shutdown failed", "error", err)
	}
}

func (d *Daemon) stopResult() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.stopErr
}

func (d *Daemon) waitForStop(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return d.stopResult()
	case <-ctx.Done():
		return ctx.Err()
	}
}
