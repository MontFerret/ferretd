package language

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MontFerret/ferret/v2/pkg/runtime"

	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestWorkspaceFallbackAndOverlayPrecedence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "query.fql")
	if err := os.WriteFile(path, []byte("RETURN missing"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := workspace.New()
	if _, err := manager.Open(ctx, root); err != nil {
		t.Fatal(err)
	}
	service := New(Options{Workspaces: manager})
	uri, err := source.PathToURI(path)
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.Diagnostics(ctx, uri)
	if err != nil || len(report.Items) == 0 || report.Version != nil {
		t.Fatalf("workspace diagnostics = %+v, %v", report, err)
	}

	if err := service.OpenDocument(ctx, uri, "ferret", 1, "RETURN 1"); err != nil {
		t.Fatal(err)
	}
	report, err = service.Diagnostics(ctx, uri)
	if err != nil || len(report.Items) != 0 || report.Version == nil || *report.Version != 1 {
		t.Fatalf("overlay diagnostics = %+v, %v", report, err)
	}

	if err := service.CloseDocument(ctx, uri); err != nil {
		t.Fatal(err)
	}
	report, err = service.Diagnostics(ctx, uri)
	if err != nil || len(report.Items) == 0 || report.Version != nil {
		t.Fatalf("workspace diagnostics after close = %+v, %v", report, err)
	}
}

func TestDocumentSymbolsDefinitionsReferencesAndHoverUseCompilerIdentity(t *testing.T) {
	query := `LET shared = 1
FUNC outer(param) {
  LET shared = param
  FUNC inner() => shared
  RETURN inner()
}
RETURN [outer(shared), shared]`
	service, uri := openLanguageDocument(t, query)

	symbols, err := service.DocumentSymbols(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	outer := findDocumentSymbol(symbols, "outer")
	if outer == nil || findDocumentSymbol(outer.Children, "param") == nil || findDocumentSymbol(outer.Children, "inner") == nil {
		t.Fatalf("document symbol hierarchy = %+v", symbols)
	}

	mapper := source.NewMapper(query)
	capturedOffset := strings.Index(query, "=> shared") + len("=> ")
	definition, err := service.Definition(context.Background(), uri, mapper.OffsetToPosition(capturedOffset))
	if err != nil || definition == nil {
		t.Fatalf("captured definition = %+v, %v", definition, err)
	}
	if got := mapper.PositionToOffset(definition.Range.Start); got != strings.Index(query, "shared = param") {
		t.Fatalf("captured definition offset = %d", got)
	}

	references, err := service.References(context.Background(), uri, mapper.OffsetToPosition(capturedOffset), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 2 {
		t.Fatalf("inner shared references = %+v", references)
	}

	hover, err := service.Hover(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "outer(param)")))
	if err != nil || hover == nil || hover.Signature == nil || hover.Signature.Label != "outer(param)" {
		t.Fatalf("UDF hover = %+v, %v", hover, err)
	}
}

func TestDefinitionsCoverLexicalSymbolKindsAndRejectExternalLocations(t *testing.T) {
	query := `LET top = 1
FUNC run(param) {
  LET local = param
  RETURN FOR item IN [local] RETURN item + top
}
RETURN run(top)`
	service, uri := openLanguageDocument(t, query)
	mapper := source.NewMapper(query)
	tests := []struct {
		name       string
		useOffset  int
		definition int
	}{
		{name: "parameter", useOffset: strings.Index(query, "local = param") + len("local = "), definition: strings.Index(query, "param)")},
		{name: "local", useOffset: strings.Index(query, "[local]") + 1, definition: strings.Index(query, "local =")},
		{name: "loop", useOffset: strings.Index(query, "item +"), definition: strings.Index(query, "item IN")},
		{name: "capture", useOffset: strings.Index(query, "top\n}"), definition: strings.Index(query, "top =")},
		{name: "UDF", useOffset: strings.LastIndex(query, "run("), definition: strings.Index(query, "run(param)")},
		{name: "outer local", useOffset: strings.LastIndex(query, "top)"), definition: strings.Index(query, "top =")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location, err := service.Definition(context.Background(), uri, mapper.OffsetToPosition(test.useOffset))
			if err != nil || location == nil {
				t.Fatalf("definition = %+v, %v", location, err)
			}
			if got := mapper.PositionToOffset(location.Range.Start); got != test.definition {
				t.Fatalf("definition offset = %d, want %d", got, test.definition)
			}
		})
	}

	udfReferences, err := service.References(
		context.Background(),
		uri,
		mapper.OffsetToPosition(strings.LastIndex(query, "run(")),
		true,
	)
	if err != nil || len(udfReferences) != 2 {
		t.Fatalf("UDF references = %+v, %v", udfReferences, err)
	}

	externalQuery := "RETURN [print(1), @parameter]"
	externalService, externalURI := openLanguageDocument(t, externalQuery)
	externalMapper := source.NewMapper(externalQuery)
	for _, offset := range []int{strings.Index(externalQuery, "print"), strings.Index(externalQuery, "parameter")} {
		location, err := externalService.Definition(context.Background(), externalURI, externalMapper.OffsetToPosition(offset))
		if err != nil || location != nil {
			t.Fatalf("external definition at %d = %+v, %v", offset, location, err)
		}
	}
}

func TestCompletionSignatureSemanticTokensAndFormatting(t *testing.T) {
	query := `// comment
LET value = 1
FUNC add(left, right) => left + right
RETURN add(value, 2)`
	service, uri := openLanguageDocument(t, query)
	mapper := source.NewMapper(query)

	items, err := service.Completion(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "RETURN")+len("RETURN ")))
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"value", "add", "RETURN", "print"} {
		if !hasCompletion(items, label) {
			t.Errorf("completion does not contain %q", label)
		}
	}

	signature, err := service.SignatureHelp(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "value, 2")+len("value, ")))
	if err != nil || signature == nil {
		t.Fatalf("signature = %+v, %v", signature, err)
	}
	if len(signature.Signatures) != 1 || signature.Signatures[0].Label != "add(left, right)" || signature.ActiveParameter != 1 {
		t.Fatalf("signature = %+v", signature)
	}

	tokens, err := service.SemanticTokens(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []SemanticTokenKind{
		SemanticTokenFunction, SemanticTokenVariable, SemanticTokenParameter,
		SemanticTokenKeyword, SemanticTokenNumber, SemanticTokenComment, SemanticTokenOperator,
	} {
		if !hasSemanticKind(tokens, kind) {
			t.Errorf("semantic tokens do not contain kind %d: %+v", kind, tokens)
		}
	}

	unformatted := "LET value=1\nRETURN {value:value}"
	formatService, formatURI := openLanguageDocument(t, unformatted)
	edits, err := formatService.Format(context.Background(), formatURI, 2)
	if err != nil || edits == nil || edits.Text == unformatted || strings.Contains(edits.Text, "\t") {
		t.Fatalf("format edits = %+v, %v", edits, err)
	}

	formattedService, formattedURI := openLanguageDocument(t, edits.Text)
	formattedMapper := source.NewMapper(edits.Text)
	formattedUse := strings.LastIndex(edits.Text, "value")
	formattedDefinition, err := formattedService.Definition(
		context.Background(),
		formattedURI,
		formattedMapper.OffsetToPosition(formattedUse),
	)
	if err != nil || formattedDefinition == nil ||
		formattedMapper.PositionToOffset(formattedDefinition.Range.Start) != strings.Index(edits.Text, "value") {
		t.Fatalf("formatted definition = %+v, %v", formattedDefinition, err)
	}

	second, err := formattedService.Format(context.Background(), formattedURI, 2)
	if err != nil || second != nil {
		t.Fatalf("idempotent format = %+v, %v", second, err)
	}

	invalidService, invalidURI := openLanguageDocument(t, "RETURN [")
	invalid, err := invalidService.Format(context.Background(), invalidURI, 4)
	if err != nil || invalid != nil {
		t.Fatalf("invalid format = %+v, %v", invalid, err)
	}
}

func TestConfiguredRegistryAndParametersDriveLanguageFeatures(t *testing.T) {
	library := runtime.NewLibrary()
	library.Namespace("CuStOm").Function().A1().Add("DoThing", func(context.Context, runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("CuStOm").Function().A2().Add("DoThing", func(context.Context, runtime.Value, runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("CuStOm").Function().Var().Add("DoThing", func(context.Context, ...runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	functions, err := library.Build()
	if err != nil {
		t.Fatal(err)
	}
	configuredParams := runtime.Params{"Configured": runtime.Int(1)}
	service := New(Options{
		Functions: functions,
		Params:    configuredParams,
	})
	configuredParams["AddedLater"] = runtime.Int(2)
	query := "RETURN CuStOm::DoThing(@Known)"
	uri := documentURI(t, "registry.fql")
	if err := service.OpenDocument(context.Background(), uri, "ferret", 1, query); err != nil {
		t.Fatal(err)
	}
	mapper := source.NewMapper(query)

	items, err := service.Completion(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "DoThing")))
	if err != nil {
		t.Fatal(err)
	}
	if !hasCompletion(items, "CuStOm::DoThing") {
		t.Fatalf("namespace completion = %+v", items)
	}
	typedItems, err := service.Completion(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "DoThing")+len("Do")))
	if err != nil || !hasCompletion(typedItems, "CuStOm::DoThing") {
		t.Fatalf("typed namespace completion = %+v, %v", typedItems, err)
	}
	bindItems, err := service.Completion(
		context.Background(),
		uri,
		mapper.OffsetToPosition(strings.Index(query, "@Known")+len("@K")),
	)
	if err != nil || !hasCompletion(bindItems, "Known") || !hasCompletion(bindItems, "Configured") || hasCompletion(bindItems, "AddedLater") {
		t.Fatalf("bind parameter completion = %+v, %v", bindItems, err)
	}

	signature, err := service.SignatureHelp(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "@Known")))
	if err != nil || signature == nil || len(signature.Signatures) != 3 ||
		signature.Signatures[0].Label != "CuStOm::DoThing(arg1)" ||
		signature.Signatures[1].Label != "CuStOm::DoThing(arg1, arg2)" ||
		!signature.Signatures[2].Variadic {
		t.Fatalf("registered signature = %+v, %v", signature, err)
	}

	paramService := New(Options{Functions: functions, Params: runtime.Params{"Configured": runtime.Int(1)}})
	paramURI := documentURI(t, "params.fql")
	if err := paramService.OpenDocument(context.Background(), paramURI, "ferret", 1, "RETURN @"); err != nil {
		t.Fatal(err)
	}
	params, err := paramService.Completion(context.Background(), paramURI, source.Position{Character: 8})
	if err != nil || !hasCompletion(params, "Configured") {
		t.Fatalf("configured parameter completion = %+v, %v", params, err)
	}
}

func TestCompletionPreservesCaseSensitiveSourceNames(t *testing.T) {
	query := "LET foo = 1\nLET FOO = 2\nRETURN foo"
	service, uri := openLanguageDocument(t, query)
	items, err := service.Completion(context.Background(), uri, source.NewMapper(query).OffsetToPosition(strings.LastIndex(query, "foo")))
	if err != nil {
		t.Fatal(err)
	}
	if !hasCompletion(items, "foo") || !hasCompletion(items, "FOO") {
		t.Fatalf("case-sensitive completion = %+v", items)
	}
}

func TestCompletionDistinguishesStatementExpressionAndDeclarationContexts(t *testing.T) {
	statementService, statementURI := openLanguageDocument(t, "")
	statement, err := statementService.Completion(context.Background(), statementURI, source.Position{})
	if err != nil || !hasCompletion(statement, "LET") || hasCompletion(statement, "print") {
		t.Fatalf("statement completion = %+v, %v", statement, err)
	}

	expressionService, expressionURI := openLanguageDocument(t, "RETURN ")
	expression, err := expressionService.Completion(context.Background(), expressionURI, source.Position{Character: 7})
	if err != nil || !hasCompletion(expression, "print") {
		t.Fatalf("expression completion = %+v, %v", expression, err)
	}

	declarationService, declarationURI := openLanguageDocument(t, "LET ")
	declaration, err := declarationService.Completion(context.Background(), declarationURI, source.Position{Character: 4})
	if err != nil || len(declaration) != 0 {
		t.Fatalf("declaration completion = %+v, %v", declaration, err)
	}
}

func TestSemanticTokensSplitQualifiedCallsIntoNamespaceAndFunction(t *testing.T) {
	query := `USE WEB::HTML AS html
RETURN html::PARSE("<p/>")`
	service, uri := openLanguageDocument(t, query)
	tokens, err := service.SemanticTokens(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	mapper := source.NewMapper(query)

	var namespaceText, functionText string
	for _, token := range tokens {
		span := mapper.RangeToSpan(token.Range)
		switch token.Kind {
		case SemanticTokenNamespace:
			if query[span.Start:span.End] == "html" {
				namespaceText = "html"
			}
		case SemanticTokenFunction:
			if query[span.Start:span.End] == "PARSE" {
				functionText = "PARSE"
			}
		}
	}
	if namespaceText == "" || functionText == "" {
		t.Fatalf("qualified semantic tokens = %+v", tokens)
	}
}

func TestSemanticTokensSplitMultilineStringsAndComments(t *testing.T) {
	query := "/* first\r\nsecond */\nRETURN `first\nsecond`"
	service, uri := openLanguageDocument(t, query)
	tokens, err := service.SemanticTokens(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}

	counts := map[SemanticTokenKind]int{}
	for _, token := range tokens {
		if token.Range.Start.Line != token.Range.End.Line {
			t.Fatalf("multiline semantic token = %+v", token)
		}
		counts[token.Kind]++
	}
	if counts[SemanticTokenComment] != 2 || counts[SemanticTokenString] < 2 {
		t.Fatalf("split semantic token counts = %#v, tokens = %+v", counts, tokens)
	}
}

func openLanguageDocument(t *testing.T, text string) (*Service, string) {
	t.Helper()

	service := New(Options{})
	uri := documentURI(t, "features.fql")
	if err := service.OpenDocument(context.Background(), uri, "ferret", 1, text); err != nil {
		t.Fatal(err)
	}

	return service, uri
}

func findDocumentSymbol(values []DocumentSymbol, name string) *DocumentSymbol {
	for index := range values {
		if values[index].Name == name {
			return &values[index]
		}
		if found := findDocumentSymbol(values[index].Children, name); found != nil {
			return found
		}
	}

	return nil
}

func hasCompletion(values []CompletionItem, label string) bool {
	for _, value := range values {
		if value.Label == label {
			return true
		}
	}

	return false
}

func hasSemanticKind(values []SemanticToken, kind SemanticTokenKind) bool {
	for _, value := range values {
		if value.Kind == kind {
			return true
		}
	}

	return false
}
