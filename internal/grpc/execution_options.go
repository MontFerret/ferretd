package grpc

import (
	"fmt"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
)

func toProtoExecutionOptions(value exec.RuntimeOptions) *executionv1.ExecutionOptions {
	result := &executionv1.ExecutionOptions{
		OutputContentType: value.OutputContentType,
	}

	if value.WorkingDirectory != "" {
		workingDirectory := value.WorkingDirectory
		result.WorkingDirectory = &workingDirectory
	}

	return result
}

func fromProtoExecutionOptions(value *executionv1.ExecutionOptions) (exec.RuntimeOptions, error) {
	if value == nil {
		return exec.RuntimeOptions{}, nil
	}

	result := exec.RuntimeOptions{
		OutputContentType: value.OutputContentType,
	}

	if value.WorkingDirectory != nil {
		if *value.WorkingDirectory == "" {
			return exec.RuntimeOptions{}, fmt.Errorf("%w: working directory must not be blank", exec.ErrInvalidExecutionOptions)
		}

		result.WorkingDirectory = value.GetWorkingDirectory()
	}

	return result, nil
}
