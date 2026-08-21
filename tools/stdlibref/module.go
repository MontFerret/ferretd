package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/mod/semver"
)

const ferretModule = "github.com/MontFerret/ferret/v2"

type (
	moduleCommand func(context.Context, ...string) ([]byte, error)

	moduleInfo struct {
		Path    string      `json:"Path"`
		Version string      `json:"Version"`
		Replace *moduleInfo `json:"Replace"`
	}
)

func resolveFerretVersion(ctx context.Context, execute moduleCommand) (string, error) {
	data, err := execute(ctx, "list", "-mod=readonly", "-m", "-json", ferretModule)
	if err != nil {
		return "", fmt.Errorf("resolve Ferret module: %w", err)
	}

	var module moduleInfo
	if err := json.Unmarshal(data, &module); err != nil {
		return "", fmt.Errorf("decode Ferret module metadata: %w", err)
	}

	if module.Path != ferretModule {
		return "", fmt.Errorf("resolved unexpected Ferret module %q", module.Path)
	}

	if module.Replace != nil {
		return "", fmt.Errorf("published API Reference selection does not support Ferret module replacements")
	}

	if module.Version == "" {
		return "", fmt.Errorf("resolved Ferret module has no version")
	}

	if !semver.IsValid(module.Version) || semver.Canonical(module.Version) != module.Version {
		return "", fmt.Errorf("resolved Ferret module version %q is not canonical SemVer", module.Version)
	}

	return strings.TrimPrefix(module.Version, "v"), nil
}

func runGoModuleCommand(ctx context.Context, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "go", arguments...)
	data, err := command.Output()
	if err == nil {
		return data, nil
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || len(exitErr.Stderr) == 0 {
		return nil, err
	}

	return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
}
