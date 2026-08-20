package debug

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewSessionID(t *testing.T) {
	id, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID: %v", err)
	}
	if id == "" || id.String() == "" {
		t.Fatal("newSessionID returned an empty ID")
	}
	if _, err := uuid.Parse(id.String()); err != nil {
		t.Fatalf("ID is not a UUID: %v", err)
	}
}
