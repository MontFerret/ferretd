// Package daemon coordinates the long-running ferretd services.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/google/uuid"

	"github.com/MontFerret/ferretd/internal/exec"
	grpcadapter "github.com/MontFerret/ferretd/internal/grpc"
	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/lsp"
	"github.com/MontFerret/ferretd/internal/transport"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	// Options configures a daemon instance.
	Options struct {
		Version  string
		Endpoint transport.Endpoint
		Logger   *slog.Logger
	}

	// Daemon owns the services and lifecycle that make up ferretd.
	Daemon struct {
		workspaces *workspace.Manager
		language   *language.Service
		execution  *exec.Manager
		lsp        *lsp.Server
		rpc        *grpcadapter.Server

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
)

// New constructs a daemon and its service boundaries.
func New(options Options) (*Daemon, error) {
	endpoint := options.Endpoint
	if endpoint == (transport.Endpoint{}) {
		var err error
		endpoint, err = transport.DefaultEndpoint()
		if err != nil {
			return nil, fmt.Errorf("resolve daemon endpoint: %w", err)
		}
	}

	version := options.Version
	if version == "" {
		version = "dev"
	}

	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	workspaceManager := workspace.New()
	languageService := language.New(language.Options{Workspaces: workspaceManager})
	executionManager := exec.New(workspaceManager)
	instanceID, err := uuid.NewRandom()

	if err != nil {
		return nil, fmt.Errorf("generate daemon instance ID: %w", err)
	}

	result := &Daemon{
		workspaces: workspaceManager,
		language:   languageService,
		execution:  executionManager,
		lsp:        lsp.New(languageService),
		endpoint:   endpoint,
		version:    version,
		logger:     logger,
		shutdown:   make(chan struct{}),
		stopDone:   make(chan struct{}),
		state:      stateNew,
	}
	result.rpc = grpcadapter.New(
		result.workspaces,
		result.execution,
		version,
		instanceID.String(),
		result.requestShutdown,
	)

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
		executionErr := d.execution.Close(context.Background())
		workspaceErr := d.workspaces.Clear(context.Background())
		d.finishStop(errors.Join(executionErr, workspaceErr, closeErr))

		return nil
	}

	d.listener = listener
	d.state = stateRunning
	d.rpc.SetServing()
	d.mu.Unlock()

	d.logger.Info("ferretd started", "endpoint", d.endpoint.String(), "version", d.version)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- d.rpc.Serve(listener)
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
		d.state = stateStopped
		executionErr := d.execution.Close(ctx)
		workspaceErr := d.workspaces.Clear(ctx)
		d.stopErr = errors.Join(executionErr, workspaceErr)
		close(d.stopDone)
		d.mu.Unlock()

		return d.stopErr
	case stateStarting:
		d.state = stateStopping
		stopDone := d.stopDone
		d.mu.Unlock()

		return d.waitForStop(ctx, stopDone)
	case stateRunning:
		d.state = stateStopping
		listener := d.listener
		d.mu.Unlock()

		d.rpc.SetNotServing()
		executionErr := d.execution.Close(ctx)
		workspaceErr := d.workspaces.Clear(ctx)
		stopErr := d.rpc.Stop(ctx)
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
