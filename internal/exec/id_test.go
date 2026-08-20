package exec

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewSessionID(t *testing.T) {
	id, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID: %v", err)
	}
	assertUUID(t, id.String())
}

func TestNewExecutionID(t *testing.T) {
	id, err := newExecutionID()
	if err != nil {
		t.Fatalf("newExecutionID: %v", err)
	}
	assertUUID(t, id.String())
}

func assertUUID(t *testing.T, value string) {
	t.Helper()

	if value == "" {
		t.Fatal("generated ID is empty")
	}
	if _, err := uuid.Parse(value); err != nil {
		t.Fatalf("ID is not a UUID: %v", err)
	}
}
