// Package daemon coordinates the long-running ferretd services.
package daemon

import (
	"context"

	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/lsp"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Daemon owns the services that make up ferretd.
type Daemon struct {
	workspaces *workspace.Manager
	language   *language.Service
	execution  *exec.SessionManager
	debug      *debug.SessionManager
	lsp        *lsp.Server
}

// New constructs a daemon and its service boundaries.
func New() (*Daemon, error) {
	languageService := language.New()

	return &Daemon{
		workspaces: workspace.New(),
		language:   languageService,
		execution:  exec.New(),
		debug:      debug.New(),
		lsp:        lsp.New(languageService),
	}, nil
}

// Start runs the daemon until the context is canceled.
func (d *Daemon) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Stop stops the daemon. It is safe to call more than once.
func (d *Daemon) Stop(context.Context) error {
	return nil
}
