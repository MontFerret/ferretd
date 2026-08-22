package workspace

import (
	"reflect"

	antlr "github.com/antlr4-go/antlr/v4"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	"github.com/MontFerret/ferret/v2/pkg/parser"
	parserdiagnostics "github.com/MontFerret/ferret/v2/pkg/parser/diagnostics"
	"github.com/MontFerret/ferret/v2/pkg/parser/fql"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
)

// Document is a daemon-owned source snapshot for a discovered file. Source and
// diagnostic accessors return copies; retained parser state is shared and must
// be treated as read-only by visitors.
type Document struct {
	file        File
	revision    Revision
	generation  uint64
	loaded      bool
	source      *ferretsource.Source
	syntax      *parser.Parser
	diagnostics []*ferretdiagnostics.Diagnostic
}

func newDocument(file File, content string) (result Document) {
	source := ferretsource.New(file.Path, content)
	result = Document{
		file:     file,
		revision: 1,
		loaded:   true,
		source:   source,
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			result.syntax = nil
			result.diagnostics = []*ferretdiagnostics.Diagnostic{
				ferretdiagnostics.NewUnexpectedErrorWith(
					source,
					"failed to parse source",
					parserPanicError(recovered),
				),
			}
		}
	}()

	history := parserdiagnostics.NewTokenHistory(64)
	result.syntax = parser.New(content, func(stream antlr.TokenStream) antlr.TokenStream {
		return parserdiagnostics.NewTrackingTokenStream(stream, history)
	})

	handler := parserdiagnostics.NewErrorHandler(source, 10)
	result.syntax.RemoveErrorListeners()
	result.syntax.AddErrorListener(parserdiagnostics.NewErrorListener(source, handler, history))
	result.syntax.Program()

	result.diagnostics = make([]*ferretdiagnostics.Diagnostic, 0, handler.Errors().Size())
	for _, diagnostic := range handler.Errors().Errors() {
		result.diagnostics = append(result.diagnostics, diagnostic)
	}

	return result
}

func newUnreadableDocument(file File, err error) Document {
	source := ferretsource.New(file.Path, "")

	return Document{
		file:     file,
		revision: 1,
		source:   source,
		diagnostics: []*ferretdiagnostics.Diagnostic{
			ferretdiagnostics.NewUnexpectedErrorWith(source, "failed to load source", err),
		},
	}
}

func (d Document) withRevision(revision Revision) Document {
	d.revision = revision

	return d
}

func (d Document) withGeneration(generation uint64) Document {
	d.generation = generation

	return d
}

func (d Document) sameState(other Document) bool {
	return d.File() == other.File() && d.Loaded() == other.Loaded() &&
		d.Content() == other.Content() &&
		reflect.DeepEqual(d.Diagnostics(), other.Diagnostics())
}

// File returns the filesystem identity associated with this document.
func (d Document) File() File {
	return d.file
}

// Revision returns the daemon-owned source revision.
func (d Document) Revision() Revision {
	return d.revision
}

// Loaded reports whether source contents were read successfully.
func (d Document) Loaded() bool {
	return d.loaded
}

// Content returns the retained source contents.
func (d Document) Content() string {
	if d.source == nil {
		return ""
	}

	return d.source.Content()
}

// Source returns a copy of the retained Ferret source.
func (d Document) Source() *ferretsource.Source {
	if d.source == nil {
		return nil
	}

	return ferretsource.New(d.source.Name(), d.source.Content())
}

// HasSyntax reports whether the document retains a Ferret parse tree.
func (d Document) HasSyntax() bool {
	return d.syntax != nil
}

// VisitSyntax visits the retained Ferret parse tree without reparsing source.
// Visitors must treat the shared syntax tree as read-only.
func (d Document) VisitSyntax(visitor fql.FqlParserVisitor) (any, bool) {
	if d.syntax == nil || visitor == nil {
		return nil, false
	}

	return d.syntax.Visit(visitor), true
}

// Diagnostics returns copies of the document's load and syntax diagnostics.
func (d Document) Diagnostics() []*ferretdiagnostics.Diagnostic {
	return cloneDiagnostics(d.diagnostics)
}
