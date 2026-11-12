package dap

import (
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/exec"
)

type launchArguments struct {
	RuntimeName      json.RawMessage `json:"runtimeName,omitempty"`
	NoDebug          json.RawMessage `json:"noDebug,omitempty"`
	Program          string          `json:"program"`
	CWD              string          `json:"cwd,omitempty"`
	WorkingDirectory json.RawMessage `json:"workingDirectory,omitempty"`
	Parameters       map[string]any  `json:"parameters,omitempty"`
	StopOnEntry      bool            `json:"stopOnEntry,omitempty"`
}

func (a launchArguments) runtimeOptions() (exec.RuntimeOptions, error) {
	if len(a.WorkingDirectory) == 0 {
		return exec.RuntimeOptions{}, nil
	}

	var directory *string
	if err := json.Unmarshal(a.WorkingDirectory, &directory); err != nil {
		return exec.RuntimeOptions{}, fmt.Errorf("%w: working directory must be a string: %w", exec.ErrInvalidExecutionOptions, err)
	}

	if directory == nil {
		return exec.RuntimeOptions{}, fmt.Errorf("%w: working directory must be a string", exec.ErrInvalidExecutionOptions)
	}

	// The domain uses empty for no override; only the transport knows it was supplied.
	if *directory == "" {
		return exec.RuntimeOptions{}, fmt.Errorf("%w: working directory must not be blank", exec.ErrInvalidExecutionOptions)
	}

	return exec.RuntimeOptions{WorkingDirectory: *directory}, nil
}

func (a launchArguments) logWorkingDirectory(event *zerolog.Event) {
	var directory *string
	if err := json.Unmarshal(a.WorkingDirectory, &directory); err == nil && directory != nil {
		event.Str("working_directory", *directory)
	}
}

func (a launchArguments) runtimeName() (string, error) {
	if len(a.RuntimeName) == 0 {
		return "", nil
	}

	var name *string
	if err := json.Unmarshal(a.RuntimeName, &name); err != nil || name == nil {
		return "", fmt.Errorf("runtimeName must be a string")
	}

	return *name, nil
}

func (a launchArguments) validateDebugMode() error {
	if len(a.NoDebug) == 0 {
		return nil
	}

	var noDebug *bool
	if err := json.Unmarshal(a.NoDebug, &noDebug); err != nil || noDebug == nil {
		return fmt.Errorf("noDebug must be a boolean")
	}

	if *noDebug {
		return fmt.Errorf("noDebug is not supported")
	}

	return nil
}
