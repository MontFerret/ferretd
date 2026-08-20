package workspace

import (
	"context"
	"testing"
)

func TestStateReservesZero(t *testing.T) {
	var state State
	if state == StateOpening || state == StateReady || state == StateFailed ||
		state == StateClosing || state == StateClosed {
		t.Fatalf("zero State aliases a meaningful state: %d", state)
	}
}

func TestWorkspaceRequiresReceiver(t *testing.T) {
	var workspace *Workspace
	ctx := context.Background()
	tests := []struct {
		name string
		call func()
	}{
		{name: "ID", call: func() { _ = workspace.ID() }},
		{name: "Root", call: func() { _ = workspace.Root() }},
		{name: "State", call: func() { _ = workspace.State() }},
		{name: "Failure", call: func() { _ = workspace.Failure() }},
		{name: "Files", call: func() { _ = workspace.Files() }},
		{name: "Documents", call: func() { _ = workspace.Documents() }},
		{name: "Document", call: func() { _, _ = workspace.Document("query.fql") }},
		{name: "Diagnostics", call: func() { _ = workspace.Diagnostics() }},
		{name: "Compile", call: func() { _, _ = workspace.Compile(ctx, "query.fql") }},
		{name: "CompileDebug", call: func() { _, _ = workspace.CompileDebug(ctx, "query.fql") }},
		{name: "CompileDocument", call: func() { _, _ = workspace.CompileDocument(ctx, Document{}) }},
		{name: "CompileDebugSnapshot", call: func() {
			_, _ = workspace.CompileDebugSnapshot(ctx, SourceSnapshot{}, "")
		}},
		{name: "RefreshDocument", call: func() { _, _ = workspace.RefreshDocument(ctx, "query.fql") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPanics(t, tt.call)
		})
	}
}
