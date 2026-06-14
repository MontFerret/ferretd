// Package language provides shared language-service behavior.
package language

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

var (
	// ErrDocumentNotOpen indicates that an operation requires an open document.
	ErrDocumentNotOpen = errors.New("document is not open")
	// ErrStaleDocumentVersion indicates that a change does not advance a document version.
	ErrStaleDocumentVersion = errors.New("document version is stale")
	// ErrNoTextChanges indicates that a change notification contained no content.
	ErrNoTextChanges = errors.New("document change contains no text changes")
)

// Service provides protocol-neutral Ferret language behavior.
type Service struct {
	mu        sync.RWMutex
	documents map[string]Document
	compiler  *compiler.Compiler
}

// New creates a language service with a reusable concurrency-safe compiler.
func New() *Service {
	return &Service{
		documents: make(map[string]Document),
		compiler:  compiler.New(),
	}
}

// OpenDocument stores or replaces an open document snapshot.
func (s *Service) OpenDocument(ctx context.Context, uri, _ string, version int32, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := source.URIToPath(uri)
	if err != nil {
		return fmt.Errorf("resolve document URI: %w", err)
	}

	s.mu.Lock()
	s.documents[uri] = Document{
		URI:     uri,
		Path:    path,
		Version: version,
		Text:    text,
	}
	s.mu.Unlock()

	return nil
}

// ChangeDocument applies full-document changes to an open document.
func (s *Service) ChangeDocument(ctx context.Context, uri string, version int32, changes []TextChange) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(changes) == 0 {
		return ErrNoTextChanges
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	document, ok := s.documents[uri]
	if !ok {
		return fmt.Errorf("%w: %s", ErrDocumentNotOpen, uri)
	}
	if version <= document.Version {
		return fmt.Errorf("%w: got %d, current %d", ErrStaleDocumentVersion, version, document.Version)
	}

	document.Version = version
	document.Text = changes[len(changes)-1].Text
	s.documents[uri] = document

	return nil
}

// CloseDocument removes an open document. Closing an unknown document is safe.
func (s *Service) CloseDocument(ctx context.Context, uri string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.documents, uri)
	s.mu.Unlock()

	return nil
}

// GetDocument returns a copy of an open document.
func (s *Service) GetDocument(ctx context.Context, uri string) (*Document, bool) {
	if ctx.Err() != nil {
		return nil, false
	}

	s.mu.RLock()
	document, ok := s.documents[uri]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}

	return &document, true
}
