package grpc

import (
	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
)

func toProtoExecutionOptions(value exec.RuntimeOptions) *executionv1.ExecutionOptions {
	result := &executionv1.ExecutionOptions{
		OutputContentType: value.OutputContentType,
	}

	if value.WorkingDirectorySet {
		workingDirectory := value.WorkingDirectory
		result.WorkingDirectory = &workingDirectory
	}

	return result
}

func fromProtoExecutionOptions(value *executionv1.ExecutionOptions) exec.RuntimeOptions {
	if value == nil {
		return exec.RuntimeOptions{}
	}

	result := exec.RuntimeOptions{
		OutputContentType: value.OutputContentType,
	}

	if value.WorkingDirectory != nil {
		result.WorkingDirectory = value.GetWorkingDirectory()
		result.WorkingDirectorySet = true
	}

	return result
}
