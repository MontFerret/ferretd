package workspace

import "errors"

// watchRegistration holds provisional watches until the corresponding retained
// state is published. Workspace mutationGate serializes registrations and rollback.
type watchRegistration struct {
	watcher   *workspaceWatcher
	added     []string
	committed bool
}

func (r *watchRegistration) observe(directory string) error {
	if r.watcher == nil {
		return nil
	}

	added, err := r.watcher.addDirectory(directory)
	if added {
		r.added = append(r.added, directory)
	}

	return err
}

func (r *watchRegistration) finish(err *error) {
	if !r.committed && len(r.added) != 0 {
		*err = errors.Join(*err, r.watcher.removeDirectories(r.added))
	}
}
