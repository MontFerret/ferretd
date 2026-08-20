package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/MontFerret/ferret/v2"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
)

func TestCompilationCloseReleasesOwnedPlanOnce(t *testing.T) {
	want := errors.New("plan close failed")
	closeCalls := 0
	engine, err := ferret.New(ferret.WithPlanCloseHook(func() error {
		closeCalls++

		return want
	}))
	if err != nil {
		t.Fatalf("New engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	plan, err := engine.Compile(context.Background(), ferretsource.New("query.fql", "RETURN 1"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	compilation := Compilation{Plan: plan}
	if err := compilation.Close(); !errors.Is(err, want) {
		t.Fatalf("first Close error = %v, want %v", err, want)
	}
	if compilation.Plan != nil || closeCalls != 1 {
		t.Fatalf("first Close retained Plan = %t, close calls = %d", compilation.Plan != nil, closeCalls)
	}

	if err := compilation.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", closeCalls)
	}
}

func TestCompilationCloseZeroValue(t *testing.T) {
	var compilation Compilation
	if err := compilation.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
