# preconfig: the Preconfiguration.com Alpha

preconfig gets an AI coding agent's machine ready before the agent starts. A
team describes the machine once, in `preconfig.yaml`, and preconfig writes the
setup file each agent platform reads, checks the setup files a repository
already has against each platform's rules, and proves the setup by running the
project's own ready check, usually its tests, on a clean machine.

When a setup fails, Preconfig Doctor reads its log, names the cause and, when
the cause is in the spec, writes the change to `preconfig.yaml` that fixes it.

This is the Alpha prototype: the engine, its tests, the tools that measured it
and the live demos' source. Preconfig Core 0.1.0, knowledge base dated
2026-09-29, with Preconfig Doctor 0.1.0, its knowledge dated 2026-09-30.

The site, with both live demos, is at https://preconfiguration.com/.

> **License.** Open source under the Apache License 2.0. See LICENSE and
> NOTICE.
>
> Copyright Preconfiguration.com 2026

## Commands

```
preconfig detect [--write]     draft preconfig.yaml from the repository's files
preconfig build  [--dry-run]   write every target from preconfig.yaml
preconfig check  [--diff]      check the setup files against each format and the spec
preconfig verify [flags]       run the setup and the ready check on a clean machine
preconfig doctor [--fix] LOG   explain a failed setup from its log, and fix the spec
preconfig targets              list the targets and where they are written
preconfig version
```

detect, build, check, verify and doctor take `--dir` (the repository's folder,
`.` by default) and `--json`. Exit codes: 0 fine or ready; 1 problems found,
or the ready check failed; 2 a setup step failed; 3 verify couldn't start a
clean machine; 4 `preconfig.yaml` has errors; 64 wrong usage. doctor exits
with 0 when the log shows no failure or `--fix` applied the change, 1 when it
found the cause, 2 when no rule explains the failure, and 4 when `--fix`
couldn't change the spec.

The five targets: `.devcontainer/devcontainer.json` (and `compose.yaml` with
services), `.github/workflows/copilot-setup-steps.yml`, `.cursor/environment.json`
and `.cursor/Dockerfile`, `cloud-init.yaml`, and `.preconfig/setup.sh`.

## Layout

```
cmd/preconfig/       the command line
cmd/wasm/            the browser build's interface (TinyGo)
internal/spec/       reads and checks preconfig.yaml: 28 checks
internal/gen/        the five targets and the setup script
internal/check/      platform rules, cross-checks, drift: 28 kinds of finding
internal/detect/     a draft spec from a repository's files
internal/verify/     the clean-machine run
internal/kb/         the knowledge base: the platform facts that change most, dated
internal/yaml/       the strict YAML reader
internal/jsonc/      the JSON reader (comments, trailing commas)
internal/tree/       the document model, JSON writer, YAML quoting
internal/textdiff/   unified diffs
internal/doctor/     Preconfig Doctor: the log readers, 31 rules, the fixes
testdata/            sample repositories, specs, broken files, recorded runs,
                     and Doctor's tuning and holdout corpora
tools/               builds, platform validation, planted bugs, quoting and
                     browser harnesses, the demo's recordings; tools/doctor/
                     for Doctor's corpus, scores, proofs and demo data
results/doctor/      Doctor's measured results: scores, proofs, the freeze
demo/app/            the Core live demo: open index.html in a browser
demo/doctor/         the Doctor live demo
bin/                 ready-built binaries for Linux, macOS and Windows
```

Go 1.24, standard library only. Core is 6,842 lines in 19 files, with 3,144
lines of tests. Doctor adds 2,999 lines, with 820 lines of tests.

## Build

```
go build ./cmd/preconfig        # this machine
bash tools/build.sh OUT_DIR     # Linux, macOS and Windows binaries, and the
                                # browser engine (needs TinyGo 0.39)
```

The binaries are built with `CGO_ENABLED=0 -trimpath -buildvcs=false -ldflags "-s -w"`.
`tools/build.sh` writes `engine.v2.js`, the browser engine with Doctor, which
the Doctor demo uses; the Core demo keeps `engine.v1.js`, built from Core 0.1.0.

## Binaries

`bin/` holds preconfig 0.1.0 built from this tree with `tools/build.sh` and Go
1.24.7, for Linux (x86-64 and Arm), macOS (Intel and Apple silicon) and
Windows (x86-64). The build is reproducible: the same command on this tree
gives the same bytes. Check a download against `bin/SHA256SUMS.txt`:

```
sha256sum -c SHA256SUMS.txt          # Linux, in bin/
shasum -a 256 -c SHA256SUMS.txt      # macOS, in bin/
```

The same binaries come with each release on GitHub, packed with the license
files, at `releases/latest/download/preconfig_{linux,darwin}_{amd64,arm64}.tar.gz`
and `preconfig_windows_amd64.zip`, with a `SHA256SUMS`. Before a release goes
out, `tools/smoke.sh` runs each binary on its own system: Linux on x86-64 and
Arm, macOS on Apple silicon (and Intel where the runner can), and Windows.

## Releases

Releases go out on their own. Raise `Version` in `internal/kb/kb.go` on main;
when the tests pass on that commit, `.github/workflows/release.yml` builds the
binaries with `tools/build.sh`, smoke-tests them, tags the commit `vX.Y.Z` and
publishes the release. Nothing is published unless every build and check
passes.

## Test and reproduce the figures

| Command | What it reproduces |
|---|---|
| `go test ./...` | 125 test functions, Core's 110 and Doctor's 15; the golden files; the setup script run under bash |
| `go test -cover ./cmd/preconfig ./internal/...` | Coverage: 90.5% of Core's statements, 84.1% of Doctor's |
| `go test -run '^$' -bench . ./internal/...` | The speed of build, check and detect |
| `go test ./internal/<package> -fuzz <target>` | Fuzzing: FuzzLoad (spec), FuzzParse (yaml, jsonc), FuzzCheck (check), FuzzDetect and FuzzYamlStr (detect) |
| `python3 tools/mutate.py`, `python3 tools/doctor/mutate.py` | Planted bugs, one at a time, with the source put back after each: 32 in Core, 14 in Doctor |
| `bash tools/schemas/fetch.sh && bash tools/validate-all.sh` | Seven sample specs built and every file checked with the platforms' schemas and tools |
| `bash tools/quoting/run.sh` | 64,686 strings through the YAML quoting, read back by five YAML readers |
| `bash tools/wasm/run.sh` | The TinyGo build against the standard Go build and the command line |
| `PRECONFIG_REAL_DOCKER=1 go test ./internal/verify -run TestRealDocker -v` | verify on a real clean machine |
| `bash tools/demo/record-verify.sh OUT_DIR [verify flags]` | The demo's recorded verify runs |
| `python3 tools/demo/record.py OUT.js` | The demo's recordings of detect, build and check |

The tools need, besides Go: Python 3 with PyYAML, ruamel.yaml and jsonschema;
Node.js; actionlint, shellcheck, Docker with compose, the devcontainers CLI and
cloud-init for the platform checks (each is skipped with a note when missing);
TinyGo 0.39 for the browser build. On networks that inspect TLS, verify needs
`--ca-file` with the proxy's CA bundle, and often `--network host`.

MEASUREMENTS.md has Core's final figures and the machines they were measured
on. Doctor's tools, and how to reproduce its figures, are in
tools/doctor/README.md; its results are in results/doctor/, and the Doctor
Alpha report v1.1 sets them out.

## Status

A working compiler, checker and verifier, tested on one Linux machine, and a
Doctor measured on 39 recorded cases, 11 of them recorded after its rules were
frozen. Python installed with uv has run since the Core Alpha: Python 3.13
reached READY on September 30. Not done yet, and the job of the Beta: runs on
Copilot, Cursor and Codespaces themselves; the Node.js, Go, PostgreSQL (other
than 16) and Redis 8 install paths, which the test machine couldn't reach;
real repositories at scale; macOS, Windows and Arm, whose binaries compile but
haven't run; cloud-init on a real server.

Contact: info@preconfiguration.com
