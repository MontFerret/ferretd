package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"syscall"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestIsInvalidatedWatchRemoval(t *testing.T) {
	linux := runtime.GOOS == "linux"
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "invalidated watch", err: syscall.EINVAL, want: linux},
		{name: "wrapped invalidated watch", err: fmt.Errorf("remove watch: %w", syscall.EINVAL), want: linux},
		{
			name: "path error",
			err:  &os.PathError{Op: "remove watch", Path: "removed", Err: syscall.EINVAL},
			want: linux,
		},
		{
			name: "joined invalidated watches",
			err:  errors.Join(syscall.EINVAL, fmt.Errorf("remove watch: %w", syscall.EINVAL)),
			want: linux,
		},
		{name: "invalid descriptor", err: syscall.EBADF},
		{name: "permission denied", err: fs.ErrPermission},
		{name: "invalid filesystem operation", err: fs.ErrInvalid},
		{name: "same error text", err: errors.New("invalid argument")},
		{name: "missing path", err: fs.ErrNotExist},
		{name: "missing watch", err: fsnotify.ErrNonExistentWatch},
		{name: "closed watcher", err: fsnotify.ErrClosed},
		{name: "joined invalid descriptor", err: errors.Join(syscall.EINVAL, syscall.EBADF)},
		{name: "joined permission error", err: errors.Join(syscall.EINVAL, fs.ErrPermission)},
		{name: "joined missing path", err: errors.Join(syscall.EINVAL, fs.ErrNotExist)},
		{name: "joined closed watcher", err: errors.Join(syscall.EINVAL, fsnotify.ErrClosed)},
		{
			name: "nested mixed errors",
			err:  fmt.Errorf("remove watches: %w", errors.Join(syscall.EINVAL, errors.Join(syscall.EINVAL, fs.ErrPermission))),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isInvalidatedWatchRemoval(test.err); got != test.want {
				t.Fatalf("isInvalidatedWatchRemoval(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}
