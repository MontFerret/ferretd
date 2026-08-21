package dap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MontFerret/ferretd/internal/source"
)

type (
	launchArguments struct {
		Program     string         `json:"program"`
		CWD         string         `json:"cwd,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
		StopOnEntry bool           `json:"stopOnEntry,omitempty"`
	}

	launchPaths struct {
		root         string
		program      string
		relativePath string
	}
)

func (a launchArguments) resolvePaths() (launchPaths, error) {
	programInput := strings.TrimSpace(a.Program)
	if programInput == "" {
		return launchPaths{}, errors.New("launch program is required")
	}

	processCWD, err := os.Getwd()
	if err != nil {
		return launchPaths{}, fmt.Errorf("get current directory: %w", err)
	}

	root := strings.TrimSpace(a.CWD)
	if root != "" && !filepath.IsAbs(root) {
		root = filepath.Join(processCWD, root)
	}

	program := programInput
	if !filepath.IsAbs(program) {
		base := root
		if base == "" {
			base = processCWD
		}
		program = filepath.Join(base, program)
	}

	program = filepath.Clean(program)

	if root == "" {
		root = filepath.Dir(program)
	}

	root = filepath.Clean(root)
	rootInfo, err := os.Stat(root)
	if err != nil {
		return launchPaths{}, fmt.Errorf("stat cwd: %w", err)
	}

	if !rootInfo.IsDir() {
		return launchPaths{}, errors.New("launch cwd is not a directory")
	}

	programInfo, err := os.Stat(program)
	if err != nil {
		return launchPaths{}, fmt.Errorf("stat program: %w", err)
	}

	if !programInfo.Mode().IsRegular() {
		return launchPaths{}, errors.New("launch program is not a regular file")
	}

	if filepath.Ext(program) != ".fql" {
		return launchPaths{}, errors.New("launch program must be a .fql file")
	}

	relative, err := filepath.Rel(root, program)
	if err != nil {
		return launchPaths{}, fmt.Errorf("resolve program in cwd: %w", err)
	}

	if relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return launchPaths{}, errors.New("launch program must be contained within cwd")
	}

	return launchPaths{root: root, program: program, relativePath: filepath.ToSlash(relative)}, nil
}

func (s *Server) sourcePath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("source path is required")
	}

	var path string
	var err error
	if s.client.pathFormat == pathFormatURI {
		path, err = source.URI(value).Path()
		if err != nil {
			return "", err
		}
	} else {
		path = value
	}

	if !filepath.IsAbs(path) {
		return "", errors.New("source path must be absolute")
	}

	return filepath.Clean(path), nil
}

func (s *Server) clientPath(path string) (string, error) {
	if s.client.pathFormat == pathFormatURI {
		uri, err := source.URIFromPath(path)
		if err != nil {
			return "", err
		}

		return uri.String(), nil
	}

	return filepath.Clean(path), nil
}

func (s *Server) fromClientLine(value int) int {
	if s.client.linesStartAt1 {
		return value
	}

	return value + 1
}

func (s *Server) fromClientColumn(value int) int {
	if value == 0 || s.client.columnsStartAt1 {
		return value
	}

	return value + 1
}

func (s *Server) toClientLine(value int) int {
	if value == 0 || s.client.linesStartAt1 {
		return value
	}

	return value - 1
}

func (s *Server) toClientColumn(value int) int {
	if value == 0 || s.client.columnsStartAt1 {
		return value
	}

	return value - 1
}
