// Package language provides shared language-service behavior.
package language

import (
	"context"
	"fmt"
	"sync"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
	"github.com/MontFerret/ferret/v2/pkg/runtime"

	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Service provides protocol-neutral Ferret language behavior.
type Service struct {
	mu         sync.RWMutex
	overlays   map[source.URI]overlay
	cache      map[source.URI]*analysisEntry
	compiler   *compiler.Compiler
	workspaces *workspace.Manager
	functions  *FunctionCatalog
	parameters runtime.Params
	generation uint64
	analyze    analyzeFunc
}

// New creates a language service using the supplied workspace and immutable function catalog.
// It returns an error when either required dependency is nil.
func New(
	workspaces *workspace.Manager,
	functions *FunctionCatalog,
	options Options,
) (*Service, error) {
	if workspaces == nil {
		return nil, errNilWorkspaceManager
	}

	if functions == nil {
		return nil, errNilFunctionCatalog
	}

	options = options.normalized()
	compilerInstance, err := compiler.New()
	if err != nil {
		return nil, fmt.Errorf("create compiler: %w", err)
	}

	result := &Service{
		overlays:   make(map[source.URI]overlay),
		cache:      make(map[source.URI]*analysisEntry),
		compiler:   compilerInstance,
		workspaces: workspaces,
		functions:  functions,
		parameters: options.Parameters,
	}
	result.analyze = compilerInstance.Analyze

	return result, nil
}

// OpenWorkspace synchronously opens a dynamically tracked workspace root.
func (s *Service) OpenWorkspace(ctx context.Context, root string) error {
	_, err := s.workspaces.Open(ctx, root)

	return err
}

// OpenDocument stores or replaces an editor overlay snapshot.
func (s *Service) OpenDocument(
	ctx context.Context,
	uri source.URI,
	version int32,
	text string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := uri.Path()
	if err != nil {
		return fmt.Errorf("resolve document URI: %w", err)
	}

	s.mu.Lock()
	s.generation++
	s.overlays[uri] = overlay{
		URI:        uri,
		Path:       path,
		Version:    version,
		Text:       text,
		generation: s.generation,
	}
	delete(s.cache, uri)
	s.mu.Unlock()

	return nil
}

// ChangeDocument applies full-document changes to an editor overlay.
func (s *Service) ChangeDocument(
	ctx context.Context,
	uri source.URI,
	version int32,
	changes []TextChange,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(changes) == 0 {
		return ErrNoTextChanges
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	document, ok := s.overlays[uri]
	if !ok {
		return fmt.Errorf("%w: %s", ErrDocumentNotOpen, uri)
	}

	if version <= document.Version {
		return fmt.Errorf("%w: got %d, current %d", ErrStaleDocumentVersion, version, document.Version)
	}

	s.generation++
	document.Version = version
	document.Text = changes[len(changes)-1].Text
	document.generation = s.generation
	s.overlays[uri] = document
	delete(s.cache, uri)

	return nil
}

// CloseDocument removes an editor overlay. Closing an unknown overlay is safe.
func (s *Service) CloseDocument(ctx context.Context, uri source.URI) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.overlays, uri)
	delete(s.cache, uri)
	s.mu.Unlock()

	return nil
}

func (s *Service) overlay(ctx context.Context, uri source.URI) (*overlay, bool) {
	if ctx.Err() != nil {
		return nil, false
	}

	s.mu.RLock()
	document, ok := s.overlays[uri]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}

	return &document, true
}
