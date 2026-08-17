// Package dap adapts the transport-neutral execution manager to DAP stdio.
package dap

import (
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	launchArguments struct {
		Program     string         `json:"program"`
		CWD         string         `json:"cwd,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
		StopOnEntry bool           `json:"stopOnEntry,omitempty"`
	}

	clientOptions struct {
		pathFormat      string
		linesStartAt1   bool
		columnsStartAt1 bool
	}

	ownedSession struct {
		workspace   workspace.ID
		session     exec.SessionID
		debug       exec.DebugSessionID
		program     string
		stopOnEntry bool
	}

	breakpointKey struct {
		file   string
		line   int
		column int
	}
)

const (
	threadID   = 1
	threadName = "Ferret"
)
