package workspace

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewID(t *testing.T) {
	id, err := newID()
	if err != nil {
		t.Fatalf("newID: %v", err)
	}
	if id == "" || id.String() == "" {
		t.Fatal("newID returned an empty ID")
	}
	if _, err := uuid.Parse(id.String()); err != nil {
		t.Fatalf("ID is not a UUID: %v", err)
	}
}
