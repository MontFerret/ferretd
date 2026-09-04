package exec

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRuntimeOptionsNormalized(t *testing.T) {
	tests := []struct {
		name  string
		input RuntimeOptions
		want  string
	}{
		{name: "zero", want: defaultOutputContentType},
		{name: "whitespace", input: RuntimeOptions{OutputContentType: " \t\n"}, want: defaultOutputContentType},
		{name: "trimmed", input: RuntimeOptions{OutputContentType: " text/plain "}, want: "text/plain"},
		{name: "explicit", input: RuntimeOptions{OutputContentType: "application/msgpack"}, want: "application/msgpack"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.input.normalized()
			if err != nil {
				t.Fatalf("normalized: %v", err)
			}
			if got.OutputContentType != test.want {
				t.Fatalf("OutputContentType = %q, want %q", got.OutputContentType, test.want)
			}
		})
	}
}

func TestRuntimeOptionsWorkingDirectoryValidation(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		got, err := (RuntimeOptions{}).normalized()
		if err != nil {
			t.Fatalf("normalized: %v", err)
		}
		if got.WorkingDirectory != "" {
			t.Fatalf("WorkingDirectory = %q, want absent", got.WorkingDirectory)
		}
	})

	t.Run("canonical", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "runtime root ü")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}

		got, err := (RuntimeOptions{WorkingDirectory: "  " + root + "  "}).normalized()
		if err != nil {
			t.Fatalf("normalized: %v", err)
		}
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}
		if got.WorkingDirectory != filepath.Clean(canonical) {
			t.Fatalf("WorkingDirectory = %q, want %q", got.WorkingDirectory, canonical)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		target := t.TempDir()
		link := filepath.Join(t.TempDir(), "runtime-link")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("create directory symlink: %v", err)
		}

		got, err := (RuntimeOptions{WorkingDirectory: link}).normalized()
		if err != nil {
			t.Fatalf("normalized: %v", err)
		}
		canonical, err := filepath.EvalSymlinks(target)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}
		if got.WorkingDirectory != filepath.Clean(canonical) {
			t.Fatalf("WorkingDirectory = %q, want %q", got.WorkingDirectory, canonical)
		}
	})

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	tests := []struct {
		name      string
		path      string
		wantCause error
	}{
		{name: "blank", path: " \t\n "},
		{name: "relative", path: "runtime"},
		{name: "missing", path: filepath.Join(t.TempDir(), "missing"), wantCause: os.ErrNotExist},
		{name: "file", path: file},
		{name: "invalid", path: string([]byte{'/', 0, 'x'})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (RuntimeOptions{WorkingDirectory: test.path}).normalized()
			if !errors.Is(err, ErrInvalidExecutionOptions) {
				t.Fatalf("normalized error = %v, want ErrInvalidExecutionOptions", err)
			}
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatalf("normalized error = %v, want cause %v", err, test.wantCause)
			}
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("inaccessible", func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "inaccessible")
			if err := os.Mkdir(root, 0); err != nil {
				t.Fatalf("Mkdir: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

			_, err := (RuntimeOptions{WorkingDirectory: root}).normalized()
			if err == nil {
				t.Skip("current user can open a mode-000 directory")
			}
			if !errors.Is(err, ErrInvalidExecutionOptions) {
				t.Fatalf("normalized error = %v, want ErrInvalidExecutionOptions", err)
			}
		})
	}
}
