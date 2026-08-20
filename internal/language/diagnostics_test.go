package language

import (
	"context"
	"testing"
)

func TestDiagnostics(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t, Options{})
	uri := documentURI(t, "query.fql")

	if err := service.OpenDocument(ctx, uri, "ferret", 1, "RETURN 1"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	diagnostics, err := service.Diagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("Diagnostics valid: %v", err)
	}

	if len(diagnostics.Items) != 0 {
		t.Fatalf("valid diagnostics = %#v", diagnostics)
	}

	if diagnostics.Version == nil || *diagnostics.Version != 1 {
		t.Fatalf("valid diagnostic version = %#v", diagnostics.Version)
	}

	if err := service.ChangeDocument(ctx, uri, 2, []TextChange{{Text: "RETURN missing"}}); err != nil {
		t.Fatalf("ChangeDocument: %v", err)
	}

	diagnostics, err = service.Diagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("Diagnostics invalid: %v", err)
	}

	if len(diagnostics.Items) == 0 {
		t.Fatal("invalid document produced no diagnostics")
	}

	if diagnostics.Items[0].Source != "ferret" || diagnostics.Items[0].Code == "" {
		t.Fatalf("diagnostic metadata = %#v", diagnostics.Items[0])
	}

	if diagnostics.Items[0].Range.Start.Character == diagnostics.Items[0].Range.End.Character {
		t.Fatalf("diagnostic range is empty: %#v", diagnostics.Items[0].Range)
	}
}

func TestDiagnosticsForEmptyDocument(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t, Options{})
	uri := documentURI(t, "empty.fql")

	if err := service.OpenDocument(ctx, uri, "ferret", 1, ""); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	diagnostics, err := service.Diagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}

	if len(diagnostics.Items) != 1 || diagnostics.Items[0].Code != "SyntaxError" {
		t.Fatalf("empty diagnostics = %#v", diagnostics)
	}
}
