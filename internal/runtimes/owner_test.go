package runtimes

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MontFerret/api"
	wireclient "github.com/MontFerret/wire/client"
)

func newTestOwner(t testing.TB, config Config) (*Owner, *rootSpy) {
	t.Helper()

	catalog, err := NewCatalog(config)
	if err != nil {
		t.Fatal(err)
	}

	native := &rootSpy{}

	owner, err := NewOwner(catalog, native)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := owner.Close(ctx); errors.Is(err, context.DeadlineExceeded) {
			t.Error("runtime owner cleanup did not settle")
		}
	})

	return owner, native
}

func wireConfig() Config {
	return Config{Runtimes: []DefinitionConfig{{Name: "app", Provider: ProviderWire, Endpoint: "tcp://localhost:9000"}}}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for lifecycle event")

		var zero T

		return zero
	}
}

func TestOwnerRequiredDependencies(t *testing.T) {
	native := &rootSpy{}
	if owner, err := NewOwner(nil, native); owner != nil || err == nil || native.closes.Load() != 0 {
		t.Fatalf("NewOwner(nil, native) = %v, %v; closes = %d", owner, err, native.closes.Load())
	}

	catalog, err := NewCatalog(Config{})
	if err != nil {
		t.Fatal(err)
	}

	if owner, err := NewOwner(catalog, nil); owner != nil || err == nil {
		t.Fatalf("NewOwner(catalog, nil) = %v, %v", owner, err)
	}
}

func TestOwnerNativeBindings(t *testing.T) {
	owner, native := newTestOwner(t, Config{})
	for _, name := range []string{"", LocalName} {
		binding, err := owner.Acquire(t.Context(), name)
		if err != nil || binding.Name() != LocalName || binding.Runtime() != native {
			t.Fatalf("Acquire(%q) = %+v, %v", name, binding, err)
		}

		for range 2 {
			if err := binding.Release(); err != nil {
				t.Fatal(err)
			}
		}

		if native.closes.Load() != 0 {
			t.Fatal("native binding closed shared root")
		}

		if _, err := binding.Runtime().Version(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	for range 2 {
		if err := owner.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	if native.closes.Load() != 1 {
		t.Fatalf("native closes = %d", native.closes.Load())
	}

	if binding, err := owner.Acquire(t.Context(), LocalName); binding != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("Acquire after Close = %v, %v", binding, err)
	}
}

func TestOwnerContextAndUnknownSelection(t *testing.T) {
	owner, _ := newTestOwner(t, wireConfig())
	owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) {
		t.Fatal("unexpected Wire construction")

		return nil, nil
	}

	for _, name := range []string{LocalName, "app"} {
		if binding, err := owner.Acquire(nil, name); binding != nil || !errors.Is(err, errNilContext) { //nolint:staticcheck // Exercise the non-nil context contract.
			t.Fatalf("nil Acquire = %v, %v", binding, err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if binding, err := owner.Acquire(ctx, name); binding != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled Acquire = %v, %v", binding, err)
		}
	}

	if binding, err := owner.Acquire(t.Context(), "unknown"); binding != nil || !errors.Is(err, ErrRuntimeNotFound) {
		t.Fatalf("unknown Acquire = %v, %v", binding, err)
	}

	if err := owner.Close(nil); !errors.Is(err, errNilContext) { //nolint:staticcheck // Exercise the non-nil context contract.
		t.Fatalf("nil Close = %v", err)
	}

	if binding, err := owner.Acquire(t.Context(), LocalName); binding == nil || err != nil {
		t.Fatalf("nil Close committed shutdown: %v, %v", binding, err)
	}
}

func TestOwnerWireResolutionAndOptions(t *testing.T) {
	config := wireConfig()
	config.DefaultRuntime = new("app")
	config.Runtimes[0].Insecure = true
	owner, _ := newTestOwner(t, config)
	roots := []*rootSpy{{}, {}}
	calls := 0
	started := time.Now()
	owner.connect = func(ctx context.Context, target string, options ...wireclient.Option) (api.Runtime, error) {
		if target != "localhost:9000" || len(options) != 1 {
			t.Fatalf("constructor target/options = %q/%d", target, len(options))
		}

		deadline, ok := ctx.Deadline()
		if !ok || deadline.Before(started.Add(5*time.Second)) || deadline.After(time.Now().Add(5*time.Second)) {
			t.Fatalf("startup deadline = %v, %v", deadline, ok)
		}

		root := roots[calls]
		calls++

		return root, nil
	}

	first, err := owner.Acquire(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}

	second, err := owner.Acquire(t.Context(), "app")
	if err != nil || first.Name() != "app" || second.Runtime() == first.Runtime() {
		t.Fatalf("independent bindings = %v, %v; error = %v", first, second, err)
	}

	if err := first.Release(); err != nil || roots[0].closes.Load() != 1 || roots[1].closes.Load() != 0 {
		t.Fatalf("first release = %v; closes = %d/%d", err, roots[0].closes.Load(), roots[1].closes.Load())
	}

	if _, err := second.Runtime().Version(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := owner.Close(t.Context()); err != nil || roots[1].closes.Load() != 1 {
		t.Fatalf("Close = %v; second closes = %d", err, roots[1].closes.Load())
	}
}

func TestOwnerSlowAcquisitionDoesNotSerialize(t *testing.T) {
	owner, _ := newTestOwner(t, wireConfig())
	entered, resume := make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(resume) }) })
	var calls atomic.Int32
	owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-resume
		}

		return &rootSpy{}, nil
	}

	first := make(chan error, 1)
	go func() {
		_, err := owner.Acquire(t.Context(), "app")
		first <- err
	}()
	receive(t, entered)

	second := make(chan error, 1)
	go func() {
		_, err := owner.Acquire(t.Context(), "app")
		second <- err
	}()

	if err := receive(t, second); err != nil {
		t.Fatal(err)
	}

	release.Do(func() { close(resume) })

	if err := receive(t, first); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerStartupFailureAndRetry(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, &wireclient.Error{Message: "handshake rejected"}} {
		t.Run(failure.Error(), func(t *testing.T) {
			owner, _ := newTestOwner(t, wireConfig())
			owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) { return nil, failure }

			binding, err := owner.Acquire(t.Context(), "app")
			if binding != nil || !errors.Is(err, failure) || !strings.Contains(err.Error(), `"app"`) {
				t.Fatalf("failed Acquire = %v, %v", binding, err)
			}

			var wireErr *wireclient.Error
			if errors.As(failure, &wireErr) && !errors.As(err, &wireErr) {
				t.Fatalf("lost Wire identity: %v", err)
			}

			if len(owner.pending) != 0 || len(owner.bindings) != 0 {
				t.Fatal("failed attempt retained resources")
			}

			owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) { return &rootSpy{}, nil }
			if binding, err := owner.Acquire(t.Context(), "app"); binding == nil || err != nil {
				t.Fatalf("retry = %v, %v", binding, err)
			}
		})
	}
}

func TestOwnerStartupDeadline(t *testing.T) {
	for _, callerFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "configured", true: "caller"}[callerFirst], func(t *testing.T) {
			config := wireConfig()
			config.Runtimes[0].ConnectTimeout = new("20ms")
			ctx := t.Context()

			if callerFirst {
				config.Runtimes[0].ConnectTimeout = new("1m")
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}

			owner, _ := newTestOwner(t, config)
			owner.connect = func(ctx context.Context, _ string, options ...wireclient.Option) (api.Runtime, error) {
				if len(options) != 0 {
					t.Error("omitted insecure supplied transport options")
				}

				<-ctx.Done()

				return nil, ctx.Err()
			}

			if binding, err := owner.Acquire(ctx, "app"); binding != nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline Acquire = %v, %v", binding, err)
			}
		})
	}
}

func TestOwnerConstructorPartialFailure(t *testing.T) {
	owner, _ := newTestOwner(t, wireConfig())
	constructionErr, cleanupErr := errors.New("construction failed"), errors.New("partial cleanup failed")
	root := &rootSpy{onClose: func() error { return cleanupErr }}
	owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) {
		return root, constructionErr
	}

	binding, err := owner.Acquire(t.Context(), "app")
	if binding != nil || !errors.Is(err, constructionErr) || !errors.Is(err, cleanupErr) || root.closes.Load() != 1 {
		t.Fatalf("partial construction = %v, %v; closes = %d", binding, err, root.closes.Load())
	}

	owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) { return nil, nil }
	if binding, err := owner.Acquire(t.Context(), "app"); binding != nil || err == nil {
		t.Fatalf("empty construction = %v, %v", binding, err)
	}

	if err := owner.Close(t.Context()); !errors.Is(err, cleanupErr) || root.closes.Load() != 1 {
		t.Fatalf("partial cleanup not retained: %v; closes = %d", err, root.closes.Load())
	}
}

func TestOwnerRejectsCancelledPublicationAndRetainsRollback(t *testing.T) {
	owner, _ := newTestOwner(t, wireConfig())
	rollbackErr := errors.New("rollback failure")
	root := &rootSpy{onClose: func() error { return rollbackErr }}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) {
		cancel()

		return root, nil
	}

	binding, err := owner.Acquire(ctx, "app")
	if binding != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, rollbackErr) || root.closes.Load() != 1 {
		t.Fatalf("cancelled publication = %v, %v; closes = %d", binding, err, root.closes.Load())
	}

	if err := owner.Close(t.Context()); !errors.Is(err, rollbackErr) {
		t.Fatalf("Close lost rollback error: %v", err)
	}
}

func TestOwnerPreservesWireConstructorRollbackWithoutHandle(t *testing.T) {
	owner, _ := newTestOwner(t, wireConfig())
	failure := errors.New("connection release failed")
	owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) {
		return nil, errors.Join(context.Canceled, failure)
	}

	_, err := owner.Acquire(t.Context(), "app")

	var rollback *RollbackError
	if !errors.As(err, &rollback) || !errors.Is(rollback, failure) {
		t.Fatalf("constructor lost rollback result: %v", err)
	}
}

func TestOwnerShutdownWaitsForUnpublishedRollback(t *testing.T) {
	owner, native := newTestOwner(t, wireConfig())
	entered, resume := make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(resume) }) })
	rollbackErr := errors.New("unpublished rollback failed")
	root := &rootSpy{onClose: func() error {
		close(entered)
		<-resume

		return rollbackErr
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) {
		cancel()

		return root, nil
	}

	result := make(chan error, 1)
	go func() {
		binding, err := owner.Acquire(ctx, "app")
		if binding != nil {
			err = errors.New("published cancelled runtime")
		}

		result <- err
	}()
	receive(t, entered)

	if err := owner.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Close waiter = %v", err)
	}

	if owner.gate.Idle() || native.closes.Load() != 0 {
		t.Fatal("shutdown abandoned pending rollback")
	}

	release.Do(func() { close(resume) })

	if err := receive(t, result); !errors.Is(err, context.Canceled) || !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback result = %v", err)
	}

	if err := owner.Close(t.Context()); !errors.Is(err, rollbackErr) || root.closes.Load() != 1 || native.closes.Load() != 1 {
		t.Fatalf("shutdown result = %v; closes = %d/%d", err, root.closes.Load(), native.closes.Load())
	}
}

func TestOwnerShutdownCancelsAndWaitsForLateConstruction(t *testing.T) {
	owner, native := newTestOwner(t, wireConfig())
	entered, cancelled, resume := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(resume) }) })
	root := &rootSpy{}
	owner.connect = func(ctx context.Context, _ string, _ ...wireclient.Option) (api.Runtime, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-resume

		return root, nil
	}

	acquired := make(chan error, 1)
	go func() {
		binding, err := owner.Acquire(t.Context(), "app")
		if binding != nil {
			err = errors.New("published binding after shutdown")
		}

		acquired <- err
	}()
	receive(t, entered)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := owner.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Close waiter = %v", err)
	}

	receive(t, cancelled)

	if native.closes.Load() != 0 || owner.gate.Idle() {
		t.Fatal("shutdown did not retain pending construction")
	}

	if binding, err := owner.Acquire(t.Context(), "app"); binding != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("acquisition during shutdown = %v, %v", binding, err)
	}

	release.Do(func() { close(resume) })

	if err := receive(t, acquired); !errors.Is(err, ErrClosed) || !errors.Is(err, context.Canceled) {
		t.Fatalf("late success = %v", err)
	}

	if err := owner.Close(t.Context()); err != nil || root.closes.Load() != 1 || native.closes.Load() != 1 {
		t.Fatalf("Close = %v; wire/native closes = %d/%d", err, root.closes.Load(), native.closes.Load())
	}
}

func TestOwnerReleaseRacingShutdownRetainsAllFailures(t *testing.T) {
	owner, native := newTestOwner(t, wireConfig())
	firstErr, secondErr, nativeErr := errors.New("first release"), errors.New("second release"), errors.New("native close")
	entered, resume := make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(resume) }) })
	first := &rootSpy{onClose: func() error {
		close(entered)
		<-resume

		return firstErr
	}}
	second := &rootSpy{onClose: func() error { return secondErr }}
	native.onClose = func() error { return nativeErr }
	roots := []api.Runtime{first, second}
	owner.connect = func(context.Context, string, ...wireclient.Option) (api.Runtime, error) {
		root := roots[0]
		roots = roots[1:]

		return root, nil
	}

	binding, err := owner.Acquire(t.Context(), "app")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := owner.Acquire(t.Context(), "app"); err != nil {
		t.Fatal(err)
	}

	const callers = 8
	results := make(chan error, callers)
	for range callers {
		go func() { results <- binding.Release() }()
	}

	receive(t, entered)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := owner.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close waiter = %v", err)
	}

	owner.mu.Lock()
	_, retained := owner.bindings[binding]
	owner.mu.Unlock()

	if !retained || native.closes.Load() != 0 {
		t.Fatal("shutdown overlooked an ongoing release")
	}

	release.Do(func() { close(resume) })
	for range callers {
		if err := receive(t, results); !errors.Is(err, firstErr) {
			t.Fatalf("Release lost retained result: %v", err)
		}
	}

	for range callers {
		go func() { results <- owner.Close(t.Context()) }()
	}

	for range callers {
		err := receive(t, results)
		if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) || !errors.Is(err, nativeErr) {
			t.Fatalf("Close lost failure: %v", err)
		}
	}

	if first.closes.Load() != 1 || second.closes.Load() != 1 || native.closes.Load() != 1 {
		t.Fatalf("close counts = %d/%d/%d", first.closes.Load(), second.closes.Load(), native.closes.Load())
	}
}

func BenchmarkNativeAcquireRelease(b *testing.B) {
	owner, _ := newTestOwner(b, Config{})
	b.ReportAllocs()
	for b.Loop() {
		binding, err := owner.Acquire(b.Context(), "")
		if err != nil {
			b.Fatal(err)
		}

		if err := binding.Release(); err != nil {
			b.Fatal(err)
		}
	}
}
