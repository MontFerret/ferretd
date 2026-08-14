// Package language provides shared language-service behavior.
package language

import (
	"context"
	"fmt"
	"sync"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/stdlib"

	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Service provides protocol-neutral Ferret language behavior.
type Service struct {
	mu         sync.RWMutex
	overlays   map[string]Document
	cache      map[string]*analysisEntry
	compiler   *compiler.Compiler
	workspaces *workspace.Manager
	functions  *runtime.Functions
	params     runtime.Params
	generation uint64
	analyze    analyzeFunc
}

// New creates a language service with immutable compiler and runtime environments.
func New(options Options) *Service {
	workspaces := options.Workspaces
	if workspaces == nil {
		workspaces = workspace.New()
	}

	functions := options.Functions
	if functions == nil {
		library := runtime.NewLibrary()
		if err := stdlib.Full().Register(library); err != nil {
			panic(fmt.Errorf("register Ferret standard library: %w", err))
		}

		var err error
		functions, err = library.Build()
		if err != nil {
			panic(fmt.Errorf("build Ferret standard library: %w", err))
		}
	}

	compilerInstance := compiler.New()
	result := &Service{
		overlays:   make(map[string]Document),
		cache:      make(map[string]*analysisEntry),
		compiler:   compilerInstance,
		workspaces: workspaces,
		functions:  functions,
		params:     options.Params.Clone(),
	}
	result.analyze = compilerInstance.Analyze

	return result
}

// OpenWorkspace synchronously opens a static workspace root.
func (s *Service) OpenWorkspace(ctx context.Context, root string) error {
	_, err := s.workspaces.Open(ctx, root)

	return err
}

// OpenDocument stores or replaces an editor overlay snapshot.
func (s *Service) OpenDocument(ctx context.Context, uri, _ string, version int32, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := source.URIToPath(uri)
	if err != nil {
		return fmt.Errorf("resolve document URI: %w", err)
	}

	s.mu.Lock()
	s.generation++
	s.overlays[uri] = Document{
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
func (s *Service) ChangeDocument(ctx context.Context, uri string, version int32, changes []TextChange) error {
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
func (s *Service) CloseDocument(ctx context.Context, uri string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.overlays, uri)
	delete(s.cache, uri)
	s.mu.Unlock()

	return nil
}

// GetDocument returns a copy of an editor overlay.
func (s *Service) GetDocument(ctx context.Context, uri string) (*Document, bool) {
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
