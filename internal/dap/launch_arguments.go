package dap

import (
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/exec"
)

type launchArguments struct {
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
