package exec

import (
	"fmt"

	"github.com/google/uuid"
)

// ExecutionID is an opaque daemon Execution identifier.
type ExecutionID string

func newExecutionID() (ExecutionID, error) {
	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate execution ID: %w", err)
	}

	return ExecutionID(value.String()), nil
}

// String returns the opaque identifier value.
func (id ExecutionID) String() string {
	return string(id)
}
