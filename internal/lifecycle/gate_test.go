package lifecycle

import (
	"context"
	"errors"
	"testing"
)

func TestGateCloseWaitsForAdmittedCreators(t *testing.T) {
	var gate Gate
	if !gate.BeginCreate() {
		t.Fatal("Gate rejected first creation before close")
	}
	if !gate.BeginCreate() {
		t.Fatal("Gate rejected second creation before close")
	}
	if gate.Idle() {
		t.Fatal("Gate reported idle with active creators")
	}
	if !gate.Accepting() {
		t.Fatal("Gate stopped accepting before close")
	}
	if !gate.BeginClose() {
		t.Fatal("first BeginClose did not own teardown")
	}
	if gate.BeginClose() {
		t.Fatal("second BeginClose owned teardown")
	}
	if gate.Accepting() || gate.BeginCreate() {
		t.Fatal("Gate accepted creation after close")
	}

	waitStarted := make(chan struct{})
	waitDone := make(chan struct{})
	go func() {
		close(waitStarted)
		gate.WaitForCreates()
		close(waitDone)
	}()
	<-waitStarted

	if gate.EndCreate() {
		t.Fatal("first EndCreate reported an empty gate")
	}
	select {
	case <-waitDone:
		t.Fatal("close passed the creation barrier with one creator active")
	default:
	}

	if !gate.EndCreate() {
		t.Fatal("last EndCreate did not report an empty gate")
	}
	if !gate.Idle() {
		t.Fatal("Gate did not report idle after the last creator left")
	}
	<-waitDone

	want := errors.New("close failed")
	gate.FinishClose(want)
	if err := gate.WaitClose(context.Background()); !errors.Is(err, want) {
		t.Fatalf("WaitClose error = %v, want %v", err, want)
	}
}
