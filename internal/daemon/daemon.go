// Package daemon coordinates the long-running ferretd services.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2"

	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/ferretapi"
	grpcadapter "github.com/MontFerret/ferretd/internal/grpc"
	"github.com/MontFerret/ferretd/internal/transport"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Daemon owns the services and lifecycle that make up ferretd.
type Daemon struct {
	workspaces *workspace.Manager
	executions *exec.Manager
	grpc       *grpcadapter.Server
	runtime    api.Runtime

	endpoint transport.Endpoint
	version  string
	logger   zerolog.Logger
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
		d.mu.Lock()
		if d.state != stateStopping {
			d.state = stateStopping
		}
		stopDone := d.stopDone
		d.mu.Unlock()
		d.startCleanup(nil, false)
		<-stopDone

		return fmt.Errorf("listen for daemon connections: %w", err)
	}

	boundEndpoint := listener.Endpoint()

	d.mu.Lock()
	if d.state == stateStopping {
		d.mu.Unlock()

		d.startCleanup(listener, false)

		return nil
	}

	d.listener = listener
	d.endpoint = boundEndpoint
	d.state = stateRunning
	d.grpc.SetServing()
	d.mu.Unlock()

	// Readiness is a process handshake and must not be filtered by --log-level.
	readyLogger := d.logger.Level(zerolog.TraceLevel)
	readyLogger.Info().
		Str("event", "ferretd.ready").
		Str("endpoint", boundEndpoint.String()).
		Str("version", d.version).
		Msg("ferretd started")

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
		stopDone := d.stopDone
		d.mu.Unlock()

		d.startCleanup(nil, false)

		return d.waitForStop(ctx, stopDone)
	case stateStarting:
		d.state = stateStopping
		stopDone := d.stopDone
		d.mu.Unlock()

		return d.waitForStop(ctx, stopDone)
	case stateRunning:
		d.state = stateStopping
		listener := d.listener
		stopDone := d.stopDone
		d.mu.Unlock()

		d.startCleanup(listener, true)

		return d.waitForStop(ctx, stopDone)
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

func (d *Daemon) startCleanup(listener net.Listener, serving bool) {
	go func() {
		if serving {
			d.grpc.SetNotServing()
		}

		ctx := context.Background()
		executionErr := d.executions.Close(ctx)
		workspaceErr := d.workspaces.Clear(ctx)
		var grpcErr error
		if serving {
			grpcErr = d.grpc.Stop(ctx)
		}
		var listenerErr error
		if listener != nil {
			listenerErr = listener.Close()
		}
		runtimeErr := d.runtime.Close()
		d.finishStop(errors.Join(executionErr, workspaceErr, grpcErr, listenerErr, runtimeErr))
	}()
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
		d.logger.Error().Err(err).Msg("ferretd shutdown failed")
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
