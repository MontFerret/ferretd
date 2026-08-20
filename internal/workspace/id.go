package workspace

import (
	"fmt"

	"github.com/google/uuid"
)

// ID is an opaque workspace identifier.
type ID string

func newID() (ID, error) {
	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate workspace ID: %w", err)
	}

	return ID(value.String()), nil
}

// String returns the opaque identifier value.
func (id ID) String() string {
	return string(id)
}
