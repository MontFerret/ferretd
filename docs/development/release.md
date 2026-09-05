# Build, Generation, CI, and Release Development

Repository commands and release behavior are defined by configuration rather
than this document. Recheck the authoritative files before changing or running
the workflow.

## Sources of truth

* `go.mod`: Go toolchain and dependency versions.
* `Makefile`: local installation, formatting, linting, testing, compilation,
  protobuf generation, generation checking, and release entry points.
* `.golangci.yml`: Go lint rules, formatter settings, and generated-code exclusions.
* `.github/workflows/ci.yml`: operating-system test matrix, quality checks,
  protobuf checks, and the current race-test package set.
* `buf.yaml` and `buf.gen.yaml`: protobuf source linting, generation inputs,
  plugins, and output layout.
* `scripts/versions.sh`: development version derivation.
* `scripts/release.sh`: release-tag validation, creation, and push.
* `.goreleaser.yml`: release build targets, archives, checksums, linker flags,
  changelog filtering, and draft-release behavior.
* `.github/workflows/release.yaml`: tag trigger, Go setup, and GoReleaser action.

Avoid copying exact tool or dependency versions into additional documentation.
The pinned values in these files are authoritative.

## Local build and validation

The broad build target runs vet, repository lint, Go tests, and compilation. It
therefore requires the normal Go toolchain plus the lint tools installed by the
repository tooling target. Compilation writes the `ferretd` binary below
`bin/` and injects the selected version through linker flags.

Validation should start with the package or contract being changed and broaden
according to risk. The CI test job runs tests, vet, and build across Linux,
macOS, and Windows. The quality job installs pinned repository tools, runs lint,
lints protobuf sources, checks generated protobuf output, and race-tests the
listed shared-state packages.

The CI race list documents current coverage, not an exemption for another
package whose shared state changes. Add affected packages to local race
validation and update CI when ongoing coverage is required.

### Go linting and formatting

Run `make install-lint` before the first lint or build, and again after the
Makefile's golangci-lint version changes. The target downloads the installer
from that release tag and uses its checksum verification to install the official
binary under `bin/tools/golangci-lint/<version>/`. It requires curl and a POSIX
shell; Windows contributors can use Git Bash. `make install-tools` includes this
step along with protobuf tool installation. Local commands and the CI quality
job use the same version and configuration.

`make lint` validates `.golangci.yml` and runs the complete configured analysis
without rewriting source or module dependencies. Checks cover errors, resource
cleanup, suspicious constructs, unused code, naming, comments, type declaration
grouping, and control-flow spacing. Tests are included. Generated files and
vendor directories are excluded from lint findings and formatting.

`make fmt` applies gofmt and goimports with the repository's MontFerret import
grouping. It does not apply linter fixes such as control-flow spacing. For a
deliberate spacing cleanup, use the installed binary's
`run --enable-only=wsl_v5 --fix ./...` command, review the diff, then rerun
`make fmt` and `make lint`.

Fix findings at their owning boundary. When a check conflicts with an
intentional contract, use a narrowly scoped `//nolint:<linter> // reason`
directive explaining that contract. Directives must name the linter, include an
explanation, and suppress an actual finding. For example, nil-context rejection
tests must keep exercising nil input, and error translation must preserve its
documented error identities. Do not exclude entire test files or packages to
silence a specific finding. Architectural rules that require judgment remain
part of the mandatory review.

## Generated artifacts

### Standard Library API Reference

The repository also checks in the Standard Library API Reference consumed by
language tooling. Its version is derived from the selected
`github.com/MontFerret/ferret/v2` module with `go list`; there is no separate
version constant. After updating Ferret, refresh every generated artifact with:

```sh
go get github.com/MontFerret/ferret/v2@<version>
make generate
```

`make generate` downloads the exact versioned API Reference selected through
the published index, validates it through Specs, verifies the canonical
`montferret/core` identity and Ferret version, writes it atomically, and then
runs protobuf generation. Do not edit
`internal/language/stdlib/api.json` manually.

`make check-generate` performs no API Reference network request. It validates
the embedded artifact and its dependency version locally, regenerates the
deterministic protobuf output, and fails when either generated area is stale or
untracked. CI therefore catches a Ferret bump whose matching reference was not
refreshed without depending on the artifact host during ordinary validation.

### Protobuf generation

The generation target invokes the pinned Buf CLI configuration. Generation
cleans and recreates checked-in daemon, workspace, and execution outputs below
`gen/`. The debug source contract is intentionally absent from generation
inputs.

For protobuf changes:

1. edit versioned sources or Buf configuration, never generated Go files;
2. lint all source contracts;
3. run pinned generation;
4. inspect the complete source and generated diff;
5. run focused server, client, conversion, and wire tests;
6. run the checked-in generation gate to detect drift and unexpected output.

Generation can produce broad mechanical changes when tool versions change.
Upgrade tools only when the task requires it and review the resulting API and
wire diff independently from formatting churn.

## Version derivation

Development builds derive their default version from Git using
`scripts/versions.sh`. A dirty worktree is reflected in that value. The Makefile
allows an explicit version override for local or release-oriented builds.

GoReleaser injects the tag-derived version directly into the same `main.version`
linker variable. Version output and linker configuration therefore form one
observable CLI contract across local and packaged builds.

## Release tag workflow

The release target delegates to `scripts/release.sh` with a positional version.
The script requires a `v`-prefixed SemVer value and a clean tracked worktree,
creates the Git tag, and pushes that tag to `origin`. Creating or pushing a tag
is an external release action, not a validation step; run it only when a release
has been explicitly authorized and all preconditions have been checked.

The script does not build artifacts locally or publish a final GitHub release.
Pushing a matching tag triggers the release workflow.

## GoReleaser workflow

The release workflow checks out full history, uses the Go version from `go.mod`,
and runs GoReleaser v2. The GoReleaser configuration builds `cmd/ferretd` with
CGO disabled for Linux, macOS, and Windows on amd64 and arm64.

The six platform archives use tarballs except for Windows ZIP files, normalize
the amd64 archive name to `x86_64`, package the project binary, and produce one
checksum file. Release notes are sorted and omit documentation-only and
test-only conventional commit entries.

GitHub releases are created as drafts. Artifact and release-note review is a
manual gate before publication. The repository does not dispatch a website
notification as part of this workflow.

## Validating release changes

Release configuration changes should be checked with the current GoReleaser
configuration validator and a clean snapshot build before any tag is created.
Inspect the exact target count, archive names and formats, archive contents,
checksums, embedded version output on a native artifact, changelog behavior, and
draft-release setting.

Workflow changes also require syntax and action-version review against current
upstream contracts. Never expose GitHub tokens or authorization data in logs,
commands, fixtures, or rejected input. Report snapshot validation separately
from an actual tag push or release publication; one does not prove the other.
