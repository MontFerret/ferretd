package dap

import (
	"testing"

	apidebugger "github.com/MontFerret/api/debugger"
)

func TestHandleTableClassifiesCurrentAndMostRecentlyInvalidatedHandles(t *testing.T) {
	table := newHandleTable()
	frameIDs := []int{table.Frame(0), table.Frame(1), table.Frame(2)}
	if frameIDs[0] == frameIDs[1] || frameIDs[0] == frameIDs[2] || frameIDs[1] == frameIDs[2] {
		t.Fatalf("recursive frame handles are not distinct: %v", frameIDs)
	}
	for index, handle := range frameIDs {
		got, status := table.FrameIndex(handle)
		if status != handleCurrent || got != index {
			t.Fatalf("FrameIndex(%d) = (%d, %v), want (%d, current)", handle, got, status, index)
		}
	}

	scopeVariables := []apidebugger.Variable{{
		Name:  "value",
		Value: apidebugger.Value{Display: "1"},
	}}
	scopeID := table.Scope(scopeVariables)
	variableID := table.Variable(apidebugger.ValueReference(17))
	if variableID == 0 || table.Variable(0) != 0 {
		t.Fatalf("variable handles = (%d, %d), want nonzero and zero", variableID, table.Variable(0))
	}
	if _, status := table.ScopeVariables(frameIDs[0]); status != handleInvalid {
		t.Fatalf("frame handle classified as scope: %v", status)
	}
	if _, status := table.VariableReference(scopeID); status != handleInvalid {
		t.Fatalf("scope handle classified as variable: %v", status)
	}
	if _, status := table.FrameIndex(variableID); status != handleInvalid {
		t.Fatalf("variable handle classified as frame: %v", status)
	}

	invalidated := table.Invalidate()
	if invalidated.frames != 3 || invalidated.scopes != 1 || invalidated.variables != 1 ||
		invalidated.stale != 5 {
		t.Fatalf("Invalidate() = %+v, want 3 frames, 1 scope, 1 variable, 5 stale", invalidated)
	}
	for _, handle := range frameIDs {
		if _, status := table.FrameIndex(handle); status != handleStale {
			t.Fatalf("FrameIndex(%d) status = %v, want stale", handle, status)
		}
	}
	if variables, status := table.ScopeVariables(scopeID); status != handleStale || variables != nil {
		t.Fatalf("ScopeVariables(%d) = (%v, %v), want (nil, stale)", scopeID, variables, status)
	}
	if reference, status := table.VariableReference(variableID); status != handleStale || reference != 0 {
		t.Fatalf("VariableReference(%d) = (%d, %v), want (0, stale)", variableID, reference, status)
	}
	if _, status := table.VariableReference(frameIDs[0]); status != handleInvalid {
		t.Fatalf("stale frame handle classified as stale variable: %v", status)
	}
	if _, status := table.FrameIndex(scopeID); status != handleInvalid {
		t.Fatalf("stale scope handle classified as stale frame: %v", status)
	}
	if _, status := table.FrameIndex(1_000_000); status != handleInvalid {
		t.Fatalf("random frame handle status = %v, want invalid", status)
	}
	if _, status := table.FrameIndex(-1); status != handleInvalid {
		t.Fatalf("negative frame handle status = %v, want invalid", status)
	}

	duplicate := table.Invalidate()
	if duplicate.frames != 0 || duplicate.scopes != 0 || duplicate.variables != 0 ||
		duplicate.stale != invalidated.stale {
		t.Fatalf("duplicate Invalidate() = %+v, want tombstones preserved from %+v", duplicate, invalidated)
	}

	nextFrame := table.Frame(9)
	if nextFrame <= variableID {
		t.Fatalf("next frame handle = %d, want greater than prior handle %d", nextFrame, variableID)
	}
	if got, status := table.FrameIndex(nextFrame); status != handleCurrent || got != 9 {
		t.Fatalf("FrameIndex(%d) = (%d, %v), want (9, current)", nextFrame, got, status)
	}
	if _, status := table.FrameIndex(frameIDs[0]); status != handleStale {
		t.Fatalf("prior frame status before next invalidation = %v, want stale", status)
	}

	table.Invalidate()
	if _, status := table.FrameIndex(nextFrame); status != handleStale {
		t.Fatalf("new frame status after invalidation = %v, want stale", status)
	}
	if _, status := table.FrameIndex(frameIDs[0]); status != handleInvalid {
		t.Fatalf("older tombstone status = %v, want invalid", status)
	}
}

func TestHandleTableNeverAliasesHandlesAcrossRepeatedCycles(t *testing.T) {
	table := newHandleTable()
	lastHandle := 0
	allocated := make([]int, 0, 100)

	for cycle := range 100 {
		handle := table.Frame(cycle)
		if handle <= lastHandle {
			t.Fatalf("cycle %d handle = %d, want greater than %d", cycle, handle, lastHandle)
		}
		for priorCycle, prior := range allocated {
			if _, status := table.FrameIndex(prior); status == handleCurrent {
				t.Fatalf("cycle %d prior handle %d from cycle %d aliases current payload", cycle, prior, priorCycle)
			}
		}

		allocated = append(allocated, handle)
		lastHandle = handle
		table.Invalidate()
		if _, status := table.FrameIndex(handle); status != handleStale {
			t.Fatalf("cycle %d invalidated handle status = %v, want stale", cycle, status)
		}
	}
}
