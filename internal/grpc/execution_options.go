package grpc

import (
	"fmt"
	"strings"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
)

func runtimeOptionsFromProto(value *executionv1.ExecutionOptions) (exec.RuntimeOptions, error) {
	if value == nil {
		return exec.RuntimeOptions{}, nil
	}

	if value.WorkingDirectory != nil && strings.TrimSpace(value.GetWorkingDirectory()) == "" {
		return exec.RuntimeOptions{}, fmt.Errorf(
			"%w: working directory must not be blank",
			exec.ErrInvalidExecutionOptions,
		)
	}

	return exec.RuntimeOptions{
		OutputContentType: value.OutputContentType,
		WorkingDirectory:  value.GetWorkingDirectory(),
	}, nil
}
