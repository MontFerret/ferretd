package lifecycle

import (
	"context"
	"errors"
	"testing"
)

func TestCloseOperationConcurrentBeginAndSharedResult(t *testing.T) {
	const callers = 16

	var operation CloseOperation
	start := make(chan struct{})
	owners := make(chan bool, callers)
	for range callers {
		go func() {
			<-start
			owners <- operation.Begin()
		}()
	}

	close(start)
	ownerCount := 0
	for range callers {
		if <-owners {
			ownerCount++
		}
	}

	if ownerCount != 1 {
		t.Fatalf("close owners = %d, want 1", ownerCount)
	}

	want := errors.New("close failed")
	results := make(chan error, callers)
	for range callers {
		go func() {
			results <- operation.Wait(context.Background())
		}()
	}

	operation.Finish(want)
	for range callers {
		if err := <-results; !errors.Is(err, want) {
			t.Fatalf("Wait error = %v, want %v", err, want)
		}
	}

	if !operation.Started() || !operation.Finished() {
		t.Fatalf("operation state = started %t, finished %t", operation.Started(), operation.Finished())
	}
}

func TestCloseOperationCanceledWaitDoesNotCancelTeardown(t *testing.T) {
	var operation CloseOperation
	if !operation.Begin() {
		t.Fatal("first Begin did not own teardown")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := operation.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Wait error = %v, want context.Canceled", err)
	}

	if operation.Finished() {
		t.Fatal("canceled waiter finished the close operation")
	}

	want := errors.New("close failed")
	operation.Finish(want)

	if err := operation.Wait(ctx); !errors.Is(err, want) {
		t.Fatalf("completed Wait error = %v, want %v", err, want)
	}
}

func TestCloseOperationPublishesSuccessfulResult(t *testing.T) {
	var operation CloseOperation
	if !operation.Begin() {
		t.Fatal("first Begin did not own teardown")
	}

	result := make(chan error, 1)
	go func() {
		result <- operation.Wait(context.Background())
	}()

	operation.Finish(nil)

	if err := <-result; err != nil {
		t.Fatalf("Wait error = %v, want nil", err)
	}
}

func TestCloseOperationRejectsInvalidFinish(t *testing.T) {
	t.Run("before begin", func(t *testing.T) {
		var operation CloseOperation
		assertPanics(t, func() { operation.Finish(nil) })
	})

	t.Run("after finish", func(t *testing.T) {
		var operation CloseOperation
		operation.Begin()
		operation.Finish(nil)
		assertPanics(t, func() { operation.Finish(nil) })
	})
}

func assertPanics(t *testing.T, call func()) {
	t.Helper()

	defer func() {
		if recover() == nil {
			t.Fatal("call did not panic")
		}
	}()

	call()
}
