package exec

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MontFerret/api"
)

func TestAcquisitionCancelBeforeAndAfterPublication(t *testing.T) {
	f := newExecutionFixture(t, "RETURN 1")
	ws := f.workspace.ID()

	ctx := t.Context()
	// An expired waiter cannot undo a fence once the cancellation is admitted.
	waiter, stop := context.WithCancel(ctx)
	stop()

	if err := f.manager.CancelSessionAcquisition(waiter, ws, "early"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.manager.AcquireSession(ctx, ws, "query.fql", "", "early"); !errors.Is(err, context.Canceled) {
		t.Fatalf("late creation: %v", err)
	}

	created, err := f.manager.AcquireSession(ctx, ws, "query.fql", "", "published")
	if err != nil || created.AcquisitionID != "published" {
		t.Fatalf("create = %+v, %v", created, err)
	}

	child, err := f.manager.CreateExecution(ctx, created.ID, nil, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := f.manager.CancelSessionAcquisition(ctx, ws, "published"); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := f.manager.GetSession(ctx, created.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("published Session still present: %v", err)
	}

	if _, err := f.manager.GetExecution(ctx, child.ID); !errors.Is(err, ErrExecutionNotFound) {
		t.Fatalf("child still present: %v", err)
	}

	if _, err := f.manager.GetSession(ctx, f.session.ID); err != nil {
		t.Fatalf("sibling: %v", err)
	}

	if err := f.workspaces.Close(ctx, ws); err != nil {
		t.Fatal(err)
	}

	if _, err := f.manager.AcquireSession(ctx, ws, "query.fql", "", "early"); err == nil {
		t.Fatal("stale workspace accepted")
	}

	opened, err := f.workspaces.Open(ctx, f.workspace.Root())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.manager.AcquireSession(ctx, opened.ID(), "query.fql", "", "early"); err != nil {
		t.Fatal(err)
	}
}

func TestAcquisitionCancelJoinsRollback(t *testing.T) {
	f := newExecutionFixture(t, "RETURN 1")
	ws := f.workspace.ID()
	entered, closing, resume := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	t.Cleanup(release)
	failure := errors.New("plan rollback failed")
	f.runtime.compileFn = func(ctx context.Context) (api.Plan, error) {
		close(entered)
		<-ctx.Done()

		return &planSpy{runtime: f.runtime, closeFn: func() error { close(closing); <-resume; return failure }}, ctx.Err()
	}
	created := make(chan error, 1)
	go func() {
		_, err := f.manager.AcquireSession(t.Context(), ws, "query.fql", "", "blocked")
		created <- err
	}()
	waitForSignal(t, entered, "compile")

	if _, err := f.manager.AcquireSession(t.Context(), ws, "query.fql", "", "blocked"); !errors.Is(err, ErrDuplicateAcquisition) {
		t.Fatalf("duplicate: %v", err)
	}

	waiter, stop := context.WithCancel(t.Context())
	defer stop()
	first := make(chan error, 1)
	go func() { first <- f.manager.CancelSessionAcquisition(waiter, ws, "blocked") }()
	waitForSignal(t, closing, "rollback")
	stop()

	if err := receiveConstructionError(t, first, "waiter"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	second := make(chan error, 1)
	go func() { second <- f.manager.CancelSessionAcquisition(t.Context(), ws, "blocked") }()
	select {
	case err := <-second:
		t.Fatalf("early settlement: %v", err)
	default:
	}

	release()

	if err := receiveConstructionError(t, created, "creation"); !errors.Is(err, failure) {
		t.Fatal(err)
	}

	if err := receiveConstructionError(t, second, "cleanup"); !errors.Is(err, failure) {
		t.Fatal(err)
	}

	if err := f.manager.CancelSessionAcquisition(t.Context(), ws, "blocked"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestAcquisitionCancelledAtPublication(t *testing.T) {
	f := newExecutionFixture(t, "RETURN 1")
	ws := f.workspace.ID()
	operation, preparing := newConstruction(t.Context())
	defer operation.finish()

	creation, err := f.manager.sessions.beginCreate(ws, operation, "prepared")
	if err != nil {
		t.Fatal(err)
	}

	finish := sync.OnceFunc(func() { _ = f.manager.sessions.finishCreate(creation) })
	defer finish()

	compiled, err := f.manager.prepareSession(preparing, operation, ws, "query.fql", "")
	if err != nil {
		t.Fatal(err)
	}

	waiter := make(chan error, 1)
	go func() { waiter <- f.manager.CancelSessionAcquisition(t.Context(), ws, "prepared") }()
	waitForSignal(t, preparing.Done(), "cancellation fence")

	// Even a constructor which missed context cancellation cannot publish.
	err = f.manager.sessions.commitCreate(t.Context(), creation, compiled)
	_ = operation.recordCleanup(compiled.closeUnpublished())
	finish()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("publication after cancellation: %v", err)
	}

	if err := receiveConstructionError(t, waiter, "settlement"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.manager.GetSession(t.Context(), compiled.id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("found unpublished Session: %v", err)
	}
}

func TestNormalSessionCloseReleasesAcquisition(t *testing.T) {
	f := newExecutionFixture(t, "RETURN 1")
	for range 2 {
		session, err := f.manager.AcquireSession(t.Context(), f.workspace.ID(), "query.fql", "", "ordinary")
		if err != nil {
			t.Fatal(err)
		}

		if err := f.manager.CloseSession(t.Context(), session.ID); err != nil {
			t.Fatal(err)
		}
	}
}
