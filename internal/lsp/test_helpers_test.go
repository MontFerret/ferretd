package lsp

import (
	"testing"

	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func newTestServer(t testing.TB) *Server {
	t.Helper()

	return mustNewServer(t, newTestLanguageService(t))
}

func mustNewServer(t testing.TB, service *language.Service) *Server {
	t.Helper()

	server, err := New(service)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return server
}

func newTestLanguageService(t testing.TB) *language.Service {
	t.Helper()

	functions, err := language.NewDefaultFunctions()
	if err != nil {
		t.Fatalf("NewDefaultFunctions: %v", err)
	}

	service, err := language.New(workspace.New(), functions, language.Options{})
	if err != nil {
		t.Fatalf("language.New: %v", err)
	}

	return service
}
