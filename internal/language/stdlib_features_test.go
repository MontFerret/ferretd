package language

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestStandardLibraryMetadataEnrichesLanguageFeatures(t *testing.T) {
	query := "RETURN abs(-1)"
	service, uri := openLanguageDocument(t, query)
	mapper := source.NewMapper(query)

	items, err := service.Completion(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "abs")+2))
	if err != nil {
		t.Fatal(err)
	}

	completion, ok := completionByLabel(items, "abs")
	if !ok {
		t.Fatal("completion omits abs")
	}

	if completion.Detail != "abs(number: Int | Float) → Float" ||
		!strings.Contains(completion.Documentation, "### Parameters") || completion.Deprecated {
		t.Fatalf("abs completion = %+v", completion)
	}

	help, err := service.SignatureHelp(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "-1")))
	if err != nil || help == nil || len(help.Signatures) != 1 {
		t.Fatalf("abs signature help = %+v, %v", help, err)
	}

	signature := help.Signatures[0]
	if signature.Label != "abs(number: Int | Float)" || signature.Parameters[0].Name != "number" ||
		signature.Parameters[0].Type != "Int | Float" || signature.Parameters[0].Description == "" ||
		signature.Return == nil || signature.Return.Type != "Float" || signature.Description == "" {
		t.Fatalf("abs signature = %+v", signature)
	}

	hover, err := service.Hover(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, "abs")))
	if err != nil || hover == nil || len(hover.RegisteredSignatures) != 1 || hover.RegisteredSignatures[0].Label != signature.Label {
		t.Fatalf("abs hover = %+v, %v", hover, err)
	}
}

func TestStandardLibraryVariadicSignatureClampsActiveParameter(t *testing.T) {
	query := `RETURN concat("a", "b", "c")`
	service, uri := openLanguageDocument(t, query)
	mapper := source.NewMapper(query)

	help, err := service.SignatureHelp(context.Background(), uri, mapper.OffsetToPosition(strings.Index(query, `"c"`)))
	if err != nil || help == nil || len(help.Signatures) != 1 {
		t.Fatalf("concat signature help = %+v, %v", help, err)
	}

	if !help.Signatures[0].Variadic || !help.Signatures[0].Parameters[0].Variadic || help.ActiveParameter != 0 {
		t.Fatalf("concat signature help = %+v", help)
	}
}

func TestSignatureHelpPreservesOrderAndPrefersFixedAuthoredOverload(t *testing.T) {
	signatures := []Signature{
		{
			Label:      "choose(values...: Any)",
			Parameters: []SignatureParameter{{Name: "values", Label: "values...: Any", Type: "Any", Variadic: true}},
			Variadic:   true,
		},
		{
			Label:      "choose(value: Int)",
			Parameters: []SignatureParameter{{Name: "value", Label: "value: Int", Type: "Int"}},
		},
	}
	function := functionSymbol{
		name:       "choose",
		identity:   "choose",
		authored:   true,
		signatures: cloneSignatures(signatures),
	}
	function.cacheCompletion()
	catalog := &FunctionCatalog{
		ordered: []functionSymbol{function},
		byName:  map[string]int{"choose": 0},
	}
	service := mustNewService(t, workspace.New(), catalog, Options{})
	query := "RETURN choose(1)"

	uri := documentURI(t, "authored-overloads.fql")
	if err := service.OpenDocument(context.Background(), uri, 1, query); err != nil {
		t.Fatal(err)
	}

	help, err := service.SignatureHelp(context.Background(), uri, source.NewMapper(query).OffsetToPosition(strings.Index(query, "1")))
	if err != nil || help == nil {
		t.Fatalf("signature help = %+v, %v", help, err)
	}

	if !reflect.DeepEqual(help.Signatures, signatures) || help.ActiveSignature != 1 || help.ActiveParameter != 0 {
		t.Fatalf("signature help = %+v", help)
	}
}
