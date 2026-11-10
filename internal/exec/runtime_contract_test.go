package exec

import (
	"context"
	"errors"
	"reflect"
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

			snapshot, err := fixture.manager.CreateSession(t.Context(), fixture.workspace.ID(), "query.fql", "")
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

func TestCreateSessionMetadataUsesCreationContext(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN @value")
	type contextKey struct{}
	operation, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "caller"))
	t.Cleanup(cancel)
	contexts := make(chan context.Context, 1)
	cleanupErr := errors.New("unpublished plan cleanup")
	var closes atomic.Int64
	fixture.runtime.paramsFn = func(ctx context.Context) ([]string, error) {
		contexts <- ctx
		<-ctx.Done()

		return nil, ctx.Err()
	}
	fixture.runtime.planClose = func() error {
		closes.Add(1)

		return cleanupErr
	}
	t.Cleanup(func() { fixture.runtime.planClose = nil })
	result := make(chan error, 1)
	go func() {
		snapshot, err := fixture.manager.CreateSession(operation, fixture.workspace.ID(), "query.fql", "")
		if snapshot.ID != "" {
			t.Error("metadata operation published a session")
		}

		result <- err
	}()

	if ctx := <-contexts; ctx.Err() != nil || ctx.Done() == nil || ctx.Value(contextKey{}) != "caller" {
		t.Fatal("metadata did not receive a live construction context")
	}

	lookup := make(chan error, 1)
	go func() {
		_, err := fixture.manager.GetSession(t.Context(), fixture.session.ID)
		lookup <- err
	}()
	waitForResult(t, lookup, "lookup during metadata retrieval")
	cancel()

	err := <-result
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) {
		t.Fatalf("metadata cancellation = %v", err)
	}

	if closes.Load() != 1 {
		t.Fatalf("unpublished plan closes = %d", closes.Load())
	}

	fixture.manager.sessions.mu.RLock()
	count := len(fixture.manager.sessions.entries)
	fixture.manager.sessions.mu.RUnlock()

	if count != 1 {
		t.Fatalf("published sessions = %d, want original session only", count)
	}
}

func TestExecutionPreservesOutputPresence(t *testing.T) {
	failure := errors.New("collection failure")
	for _, test := range []struct {
		name       string
		output     *api.Content
		collectErr error
		closeErr   error
	}{
		{name: "absent"},
		{name: "absent with error", collectErr: failure},
		{name: "empty", output: &api.Content{}},
		{name: "empty non-nil data", output: &api.Content{Data: []byte{}}},
		{name: "empty with collection error", output: &api.Content{}, collectErr: failure},
		{name: "empty with cleanup error", output: &api.Content{}, closeErr: failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			want := cloneContent(test.output)
			manager, session, runtime := newHookedManager(t, "RETURN 1", withSessionCloseHook(func() error {
				if test.output != nil {
					test.output.Metadata.ContentType = "mutated during close"
				}

				return test.closeErr
			}))
			runtime.collect = func(context.Context, sessionOptionsSpy) (*api.Content, error) {
				return test.output, test.collectErr
			}

			created, err := manager.CreateExecution(t.Context(), session.ID, nil, RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}

			terminal, _ := runAndObserve(t, manager, created.ID)
			if (terminal.Output != nil) != (test.output != nil) {
				t.Fatalf("output presence = %+v, input = %+v", terminal.Output, test.output)
			}

			if !reflect.DeepEqual(terminal.Output, want) {
				t.Fatalf("retained output = %+v, want %+v", terminal.Output, want)
			}

			switch {
			case test.collectErr != nil:
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
