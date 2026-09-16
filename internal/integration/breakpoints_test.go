package integration_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2"
	ferruntime "github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/uapi"
	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestLiveBreakpointReplacementThroughUniversalRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	entered, release := make(chan struct{}), make(chan struct{})

	runtime, err := uapi.New(ferret.WithFunctionsRegistrar(func(ns ferruntime.Namespace) {
		ns.Function().A0().Add("WAIT_NATIVE", func(runCtx context.Context) (ferruntime.Value, error) {
			close(entered)
			select {
			case <-release:
				return ferruntime.NewInt(1), nil
			case <-runCtx.Done():
				return nil, runCtx.Err()
			}
		})
	}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = runtime.Close() })

	plan, err := runtime.CompileDebug(ctx, api.NewSource("query.fql", "LET value = WAIT_NATIVE()\nRETURN value"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = plan.Close() })

	session, err := plan.NewDebugSession(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = session.Close() })
	requests := []apidebugger.BreakpointRequest{
		{Position: api.Position{Line: 2}, Options: apidebugger.BreakpointOptions{BindingMode: apidebugger.BreakpointBindExact}},
		{Position: api.Position{Line: 2}},
		{Position: api.Position{Line: 99}},
	}

	first, err := session.ReplaceBreakpoints(ctx, "query.fql", requests)
	if err != nil || len(first) != 3 || !first[0].Bound || !first[1].Bound || first[2].Bound || first[0].ID == first[1].ID {
		t.Fatalf("initial breakpoints = %+v, %v", first, err)
	}

	for index, breakpoint := range first {
		if breakpoint.RequestedLocation.SourceName != "query.fql" || breakpoint.RequestedLocation.Position != requests[index].Position || breakpoint.BindingMode != requests[index].Options.BindingMode {
			t.Fatalf("request translation = %+v", breakpoint)
		}

		if breakpoint.Bound && (breakpoint.Location.SourceName != "query.fql" || breakpoint.Location.Line != 2) {
			t.Fatalf("resolved location = %+v", breakpoint)
		}
	}

	if _, err := session.Start(ctx); err != nil {
		t.Fatal(err)
	}

	type result struct {
		event *apidebugger.Event
		err   error
	}
	done := make(chan result, 1)
	go func() {
		event, err := session.Continue(ctx)
		done <- result{event: event, err: err}
	}()

	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	unchanged, err := session.ReplaceBreakpoints(ctx, "query.fql", requests)
	if err != nil || !reflect.DeepEqual(unchanged, first) {
		t.Fatalf("unchanged replacement = %+v, %v", unchanged, err)
	}

	for _, invalid := range [][]apidebugger.BreakpointRequest{
		{{Position: api.Position{Line: 2}}, {Position: api.Position{Line: 0}}},
		{{Position: api.Position{Line: 2}, Options: apidebugger.BreakpointOptions{BindingMode: 99}}},
	} {
		if _, err := session.ReplaceBreakpoints(ctx, "query.fql", invalid); !errors.Is(err, ferruntime.ErrInvalidArgument) {
			t.Fatalf("invalid replacement lost native error identity: %v", err)
		}

		retained, err := session.Breakpoints(ctx)
		if err != nil || !reflect.DeepEqual(retained, first) {
			t.Fatalf("failed replacement changed previous set: %+v, %v", retained, err)
		}
	}

	canceled, cancelRequest := context.WithCancel(ctx)
	cancelRequest()

	if _, err := session.ReplaceBreakpoints(canceled, "query.fql", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replacement = %v", err)
	}

	retained, err := session.Breakpoints(ctx)
	if err != nil || !reflect.DeepEqual(retained, first) {
		t.Fatalf("canceled replacement changed previous set: %+v, %v", retained, err)
	}

	cleared, err := session.ReplaceBreakpoints(ctx, "query.fql", nil)
	if err != nil || len(cleared) != 0 {
		t.Fatalf("clear = %+v, %v", cleared, err)
	}

	next, err := session.ReplaceBreakpoints(ctx, "query.fql", requests[:1])
	if err != nil || len(next) != 1 || next[0].ID <= first[2].ID {
		t.Fatalf("replacement reused removed ID: %+v, %v", next, err)
	}

	close(release)
	select {
	case result := <-done:
		if result.err != nil || result.event == nil || result.event.Reason != apidebugger.ReasonBreakpoint ||
			len(result.event.HitBreakpointIDs) != 1 || result.event.HitBreakpointIDs[0] != next[0].ID {
			t.Fatalf("live breakpoint stop = %+v, %v", result.event, result.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if _, err := session.ReplaceBreakpoints(ctx, "query.fql", nil); err != nil {
		t.Fatal(err)
	}

	completed, err := session.Continue(ctx)
	if err != nil || completed == nil || completed.Reason != apidebugger.ReasonCompleted {
		t.Fatalf("completion = %+v, %v", completed, err)
	}

	if _, err := session.ReplaceBreakpoints(ctx, "query.fql", requests); err == nil {
		t.Fatal("completed session accepted replacement")
	}
}

func TestDebugManagerLiveBreakpointReplacement(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	entered, release := make(chan struct{}), make(chan struct{})

	runtime, err := uapi.New(ferret.WithFunctionsRegistrar(func(ns ferruntime.Namespace) {
		ns.Function().A0().Add("GATE", func(ctx context.Context) (ferruntime.Value, error) {
			select {
			case entered <- struct{}{}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			select {
			case <-release:
				return ferruntime.NewInt(1), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
	}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = runtime.Close() })
	workspaces := workspace.New()
	t.Cleanup(func() { _ = workspaces.Clear(context.Background()) })
	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN FOR i IN 1..4\n LET gate = GATE()\n LET x = i + gate\n RETURN x + 1")

	opened, err := workspaces.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	executions, err := exec.New(workspaces, runtime)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = executions.Close(context.Background()) })

	manager, err := debug.New(executions)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	compiled, err := executions.CreateSession(ctx, opened.ID(), "query.fql")
	if err != nil {
		t.Fatal(err)
	}

	created, err := manager.CreateSession(ctx, compiled.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := manager.WatchSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Cancel)
	waitState := func(state debug.State) debug.SessionSnapshot {
		t.Helper()

		for {
			select {
			case event, ok := <-subscription.Events:
				if !ok {
					t.Fatal("watch ended before expected state")
				}

				if event.Snapshot.State == state {
					return event.Snapshot
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	replace := func(line int) []apidebugger.Breakpoint {
		t.Helper()
		var requests []apidebugger.BreakpointRequest

		if line != 0 {
			requests = []apidebugger.BreakpointRequest{{Position: api.Position{Line: line}}}
		}

		results, err := manager.ReplaceBreakpoints(ctx, created.ID, filepath.Join(root, "query.fql"), requests)
		if err != nil || len(results) != len(requests) {
			t.Fatalf("replacement = %+v, %v", results, err)
		}

		return results
	}

	replace(0)

	if _, err := manager.StartSession(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	waitState(debug.StateStopped)

	for visit := 0; visit < 4; visit++ {
		if visit != 2 {
			if _, err := manager.ContinueSession(ctx, created.ID); err != nil {
				t.Fatal(err)
			}
		}

		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}

		line := 3

		if visit == 1 {
			line = 0
		} else if visit >= 2 {
			line = 4
		}

		if visit == 3 {
			replace(3)
		}

		bound := replace(line)

		select {
		case release <- struct{}{}:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}

		if line != 0 {
			stopped := waitState(debug.StateStopped)
			if stopped.Reason != apidebugger.ReasonBreakpoint || stopped.Location.Line != line || !reflect.DeepEqual(stopped.HitBreakpointIDs, []apidebugger.BreakpointID{bound[0].ID}) {
				t.Fatalf("live stop = %+v, want %+v", stopped, bound)
			}
		}
	}

	replace(0)

	if _, err := manager.ContinueSession(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	waitState(debug.StateCompleted)
}
