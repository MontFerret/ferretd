package workspace

import (
	"errors"
	"testing"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
	"github.com/MontFerret/ferretd/internal/source"
)

func TestDocumentProjectDiagnostics(t *testing.T) {
	file := File{Path: "/query.fql", URI: "file:///query.fql", RelativePath: "query.fql"}
	document := newDocument(file, "RETURN '😀'\nRETURN 1")
	document.diagnostics = []*ferretdiagnostics.Diagnostic{{
		Message: "source problem",
		Spans: []ferretdiagnostics.ErrorSpan{
			ferretdiagnostics.NewMainErrorSpan(ferretsource.Span{Start: 8, End: 12}, "emoji"),
			ferretdiagnostics.NewSecondaryErrorSpan(ferretsource.Span{Start: 0, End: 6}, "related"),
		},
	}}

	projected := document.ProjectDiagnostics()
	if len(projected) != 1 || projected[0].URI != file.URI || projected[0].Range != (source.Range{
		Start: source.Position{Character: 8},
		End:   source.Position{Character: 10},
	}) {
		t.Fatalf("projected diagnostics = %+v", projected)
	}

	projected[0].RelatedInformation[0].Message = "changed"
	if got := document.ProjectDiagnostics()[0].RelatedInformation[0].Message; got != "related" {
		t.Fatalf("retained annotation = %q", got)
	}

	unreadable := newUnreadableDocument(file, errors.New("read failure"))
	if got := unreadable.ProjectDiagnostics(); len(got) != 1 || got[0].Message == "" || got[0].URI != file.URI {
		t.Fatalf("unreadable document diagnostics = %+v", got)
	}
}
