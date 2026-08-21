package language

import (
	"context"
	"fmt"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"

	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	// SnapshotID identifies one editor-overlay or static-workspace source snapshot.
	SnapshotID struct {
		origin      snapshotOrigin
		generation  uint64
		workspaceID workspace.ID
		revision    workspace.Revision
	}

	documentSnapshot struct {
		id      SnapshotID
		uri     source.URI
		path    string
		text    string
		version *int32
	}

	analysisEntry struct {
		ready    chan struct{}
		snapshot documentSnapshot
		analysis *compiler.Analysis
		err      error
	}

	analyzedDocument struct {
		snapshot documentSnapshot
		mapper   *source.Mapper
		analysis *compiler.Analysis
	}

	analyzeFunc func(*ferretsource.Source) (*compiler.Analysis, error)

	snapshotOrigin uint8
)

const (
	snapshotOverlay snapshotOrigin = iota + 1
	snapshotWorkspace
)

func (s *Service) analyzedDocument(ctx context.Context, uri source.URI) (analyzedDocument, error) {
	var entry *analysisEntry

	for {
		snapshot, err := s.resolveSnapshot(ctx, uri)
		if err != nil {
			return analyzedDocument{}, err
		}

		s.mu.Lock()
		if !s.snapshotCurrentLocked(snapshot) {
			s.mu.Unlock()

			continue
		}

		entry = s.cache[uri]
		if entry == nil || entry.snapshot.id != snapshot.id {
			entry = &analysisEntry{ready: make(chan struct{}), snapshot: snapshot}
			s.cache[uri] = entry
			go s.runAnalysis(uri, entry)
		}
		s.mu.Unlock()

		break
	}

	select {
	case <-ctx.Done():
		return analyzedDocument{}, ctx.Err()
	case <-entry.ready:
	}

	if entry.analysis == nil {
		if entry.err != nil {
			return analyzedDocument{}, entry.err
		}

		return analyzedDocument{}, fmt.Errorf("analyze %q: no snapshot", uri)
	}

	return analyzedDocument{
		snapshot: entry.snapshot,
		mapper:   source.NewMapper(entry.snapshot.text),
		analysis: entry.analysis,
	}, nil
}

func (s *Service) runAnalysis(uri source.URI, entry *analysisEntry) {
	analysis, err := s.analyze(ferretsource.New(entry.snapshot.path, entry.snapshot.text))
	entry.analysis = analysis
	entry.err = err

	s.mu.Lock()
	if s.cache[uri] == entry && !s.snapshotCurrentLocked(entry.snapshot) {
		delete(s.cache, uri)
	}
	close(entry.ready)
	s.mu.Unlock()
}

func (s *Service) resolveSnapshot(ctx context.Context, uri source.URI) (documentSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return documentSnapshot{}, err
	}

	path, err := uri.Path()
	if err != nil {
		return documentSnapshot{}, fmt.Errorf("resolve document URI: %w", err)
	}

	s.mu.RLock()
	overlay, ok := s.overlays[uri]
	s.mu.RUnlock()

	if ok {
		version := overlay.Version

		return documentSnapshot{
			id:      SnapshotID{origin: snapshotOverlay, generation: overlay.generation},
			uri:     uri,
			path:    overlay.Path,
			text:    overlay.Text,
			version: &version,
		}, nil
	}

	lookup, ok, err := s.workspaces.LookupDocument(ctx, path)
	if err != nil {
		return documentSnapshot{}, fmt.Errorf("resolve workspace document: %w", err)
	}

	if !ok {
		return documentSnapshot{}, fmt.Errorf("%w: %s", ErrDocumentNotOpen, uri)
	}

	return documentSnapshot{
		id: SnapshotID{
			origin:      snapshotWorkspace,
			workspaceID: lookup.Workspace,
			revision:    lookup.Revision,
		},
		uri:  uri,
		path: path,
		text: lookup.Document.Content(),
	}, nil
}

func (s *Service) snapshotCurrentLocked(snapshot documentSnapshot) bool {
	overlay, ok := s.overlays[snapshot.uri]
	if snapshot.id.origin == snapshotOverlay {
		return ok && overlay.generation == snapshot.id.generation
	}

	if ok {
		return false
	}

	// The caller holds Service.mu so an overlay cannot interleave with this
	// retained-state lookup. Workspace never calls back into language, establishing
	// the intentional language-to-workspace lock order without a reverse edge.
	lookup, found, err := s.workspaces.LookupDocument(context.Background(), snapshot.path)
	if err != nil || !found {
		return false
	}

	return lookup.Workspace == snapshot.id.workspaceID && lookup.Revision == snapshot.id.revision
}

// IsCurrent reports whether id still identifies the source currently resolved for uri.
func (s *Service) IsCurrent(ctx context.Context, uri source.URI, id SnapshotID) bool {
	if ctx.Err() != nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := documentSnapshot{id: id, uri: uri}
	if id.origin == snapshotWorkspace {
		path, err := uri.Path()
		if err != nil {
			return false
		}

		snapshot.path = path
	}

	return s.snapshotCurrentLocked(snapshot)
}
