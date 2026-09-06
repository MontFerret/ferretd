package workspace

import (
	"runtime"
	"syscall"
)

// Linux can invalidate a deleted directory's inotify watch before fsnotify
// processes the deletion event. Only pure EINVAL removal failures are benign;
// any other joined failure must still reach the caller.
func isInvalidatedWatchRemoval(err error) bool {
	return runtime.GOOS == "linux" && errorOnlyMatches(err, syscall.EINVAL)
}
