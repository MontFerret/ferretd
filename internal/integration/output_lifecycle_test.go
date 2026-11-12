package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MontFerret/ferret/v2"
	ferruntime "github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/uapi"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestNativeExecutionOwnsInvocationUntilOutputSettlement(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	entered := make(chan struct{})

	runtime, err := uapi.New("test", ferret.WithFunctionsRegistrar(func(ns ferruntime.Namespace) {
		ns.Function().A0().Add("WAIT_NATIVE", func(ctx context.Context) (ferruntime.Value, error) {
			close(entered)
			<-ctx.Done()

			return nil, ctx.Err()
		})
	}))
	if err != nil {
		t.Fatal(err)
	}

	owner := newTestOwner(t, runtime)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN WAIT_NATIVE()"), 0o600); err != nil {
		t.Fatal(err)
	}

	workspaces := workspace.New()
	t.Cleanup(func() { _ = workspaces.Clear(context.Background()) })

	opened, err := workspaces.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := exec.New(workspaces, owner)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	session, err := manager.CreateSession(ctx, opened.ID(), "query.fql", "")
	if err != nil {
		t.Fatal(err)
	}

	created, err := manager.CreateExecution(ctx, session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := manager.WatchExecution(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Cancel)

	invocation, cancelInvocation := context.WithCancel(ctx)
	t.Cleanup(cancelInvocation)

	if _, err := manager.RunExecution(invocation, created.ID); err != nil {
		t.Fatal(err)
	}

	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	cancelInvocation()

	running, err := manager.GetExecution(ctx, created.ID)
	if err != nil || running.State != exec.StateRunning {
		t.Fatalf("execution after RPC cancellation = %+v, %v", running, err)
	}

	if _, err := manager.CancelExecution(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	var kinds []exec.EventKind
	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				if len(kinds) != 2 || kinds[0] != exec.EventStarted || kinds[1] != exec.EventCancelled {
					t.Fatalf("execution events = %v", kinds)
				}

				return
			}

			kinds = append(kinds, event.Kind)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
