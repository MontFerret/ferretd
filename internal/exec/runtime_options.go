package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultOutputContentType = "application/json"

// RuntimeOptions contains per-run Ferret settings shared by ordinary and
// debugger execution.
type RuntimeOptions struct {
	OutputContentType string
	WorkingDirectory  string
}

func (o RuntimeOptions) normalized() (RuntimeOptions, error) {
	o.OutputContentType = strings.TrimSpace(o.OutputContentType)
	if o.OutputContentType == "" {
		o.OutputContentType = defaultOutputContentType
	}

	if o.WorkingDirectory == "" {
		return o, nil
	}

	o.WorkingDirectory = strings.TrimSpace(o.WorkingDirectory)
	if o.WorkingDirectory == "" {
		return RuntimeOptions{}, fmt.Errorf("%w: working directory must not be blank", ErrInvalidExecutionOptions)
	}

	if !filepath.IsAbs(o.WorkingDirectory) {
		return RuntimeOptions{}, fmt.Errorf("%w: working directory must be absolute", ErrInvalidExecutionOptions)
	}

	canonical, err := filepath.EvalSymlinks(filepath.Clean(o.WorkingDirectory))
	if err != nil {
		return RuntimeOptions{}, fmt.Errorf("%w: resolve working directory: %w", ErrInvalidExecutionOptions, err)
	}

	canonical = filepath.Clean(canonical)
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return RuntimeOptions{}, fmt.Errorf("%w: open working directory: %w", ErrInvalidExecutionOptions, err)
	}

	if err := root.Close(); err != nil {
		return RuntimeOptions{}, fmt.Errorf("%w: close working directory validation handle: %w", ErrInvalidExecutionOptions, err)
	}

	o.WorkingDirectory = canonical

	return o, nil
}
