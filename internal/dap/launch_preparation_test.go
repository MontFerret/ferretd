package dap

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2/uapi"
)

func newPreparationClient(t *testing.T, stage string, cleanupErr error) (*testClient, <-chan struct{}, *atomic.Int32) {
	t.Helper()

	native, err := uapi.New("test")
	if err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	closed := new(atomic.Int32)
	first := true
	runtime := &preparationRuntime{Runtime: native}
	runtime.compileFn = func(ctx context.Context, source api.Source, debug bool) (api.Plan, error) {
		selected := (stage == "compile" && !debug) || (stage == "debug compile" && debug)
		if first && selected {
			first = false
			close(entered)
			<-ctx.Done()
			// Deliberately return a successful resource after cancellation.
			// Its owner must settle it instead of publishing late success.
			plan, compileErr := native.Compile(context.Background(), source)

			if debug {
				_ = plan.Close()
				plan, compileErr = native.CompileDebug(context.Background(), source)
			}

			return &preparationPlan{Plan: plan, closeFn: func() error { closed.Add(1); return cleanupErr }}, compileErr
		}

		if !debug {
			return native.Compile(ctx, source)
		}

		plan, compileErr := native.CompileDebug(ctx, source)
		if stage == "debugger" && first && compileErr == nil {
			first = false

			return &preparationPlan{Plan: plan, createFn: func(ctx context.Context, options ...api.SessionOption) (apidebugger.Session, error) {
				close(entered)
				<-ctx.Done()
				session, createErr := plan.NewDebugSession(context.Background(), options...)
				closed.Add(1)

				return session, createErr
			}}, nil
		}

		return plan, compileErr
	}

	return newTestClientWithRuntime(t, Options{}, runtime), entered, closed
}

func awaitDAPSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("DAP operation did not reach synchronization point")
	}
}
