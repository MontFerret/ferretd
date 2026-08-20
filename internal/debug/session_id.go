package debug

import (
	"fmt"

	"github.com/google/uuid"
)

// SessionID is an opaque daemon debug Session identifier.
type SessionID string

func newSessionID() (SessionID, error) {
	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate debug session ID: %w", err)
	}

	return SessionID(value.String()), nil
}

// String returns the opaque identifier value.
func (id SessionID) String() string {
	return string(id)
}
