package exec

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MontFerret/api"
)

func TestCreateSessionMetadataFailureClosesUnpublishedPlan(t *testing.T) {
	for _, metadataErr := range []error{errors.New("metadata unavailable"), context.Canceled, context.DeadlineExceeded} {
		t.Run(metadataErr.Error(), func(t *testing.T) {
			fixture := newExecutionFixture(t, "RETURN 1")
			cleanupErr := errors.New("plan cleanup")
			var closes atomic.Int64
			fixture.runtime.paramsErr = metadataErr
			fixture.runtime.planClose = func() error {
				closes.Add(1)

				return cleanupErr
			}
			t.Cleanup(func() { fixture.runtime.planClose = nil })

			snapshot, err := fixture.manager.CreateSession(t.Context(), fixture.workspace.ID(), "query.fql")
			if snapshot.ID != "" || !errors.Is(err, metadataErr) || !errors.Is(err, cleanupErr) {
				t.Fatalf("CreateSession = %+v, %v", snapshot, err)
			}

			var compilation *CompilationError

			wantCompilation := !errors.Is(metadataErr, context.Canceled) && !errors.Is(metadataErr, context.DeadlineExceeded)
			if errors.As(err, &compilation) != wantCompilation {
				t.Fatalf("compilation classification = %v, want %v", err, wantCompilation)
			}

			if closes.Load() != 1 {
				t.Fatalf("plan close calls = %d, want 1", closes.Load())
			}

			fixture.manager.sessions.mu.RLock()
			count := len(fixture.manager.sessions.entries)
			fixture.manager.sessions.mu.RUnlock()

			if count != 1 {
				t.Fatalf("retained sessions = %d, want only the original session", count)
			}
		})
	}
}

func TestExecutionPreservesOutputPresence(t *testing.T) {
	failure := errors.New("run failure")
	for _, test := range []struct {
		name     string
		output   *api.Output
		runErr   error
		closeErr error
	}{
		{name: "absent", runErr: failure},
		{name: "empty", output: &api.Output{}},
		{name: "empty with run error", output: &api.Output{}, runErr: failure},
		{name: "empty with cleanup error", output: &api.Output{}, closeErr: failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, session, runtime := newHookedManager(t, "RETURN 1", withSessionCloseHook(func() error {
				if test.output != nil {
					test.output.ContentType = "mutated during close"
				}

				return test.closeErr
			}))
			runtime.run = func(context.Context, sessionOptionsSpy) (*api.Output, error) {
				return test.output, test.runErr
			}

			created, err := manager.CreateExecution(t.Context(), session.ID, nil, RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}

			terminal, _ := runAndObserve(t, manager, created.ID)
			if (terminal.Output != nil) != (test.output != nil) {
				t.Fatalf("output presence = %+v, input = %+v", terminal.Output, test.output)
			}

			if terminal.Output != nil && (terminal.Output.ContentType != "" || terminal.Output.Content != nil) {
				t.Fatalf("retained empty output = %+v", terminal.Output)
			}

			switch {
			case test.runErr != nil:
				assertFailure(t, terminal, FailureRuntime, test.output != nil)
			case test.closeErr != nil:
				assertFailure(t, terminal, FailureCleanup, true)
			default:
				if terminal.State != StateCompleted {
					t.Fatalf("state = %v", terminal.State)
				}
			}
		})
	}
}
