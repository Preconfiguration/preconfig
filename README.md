# preconfig

preconfig gets an AI coding agent's machine ready before the agent starts. A
team describes the machine once, in `preconfig.yaml`, and preconfig writes the
setup file each agent platform reads, checks the setup files a repository
already has against each platform's rules, and proves the setup by running the
project's own ready check, usually its tests, on a clean machine.

When a setup fails, Preconfig Doctor reads its log, names the cause and, when
the cause is in the spec, writes the change to `preconfig.yaml` that fixes it.

This repository holds the engine, its tests, the tools that measured it and the
live demos' source. preconfig 0.2.0, knowledge base dated 2026-11-04, with
Preconfig Doctor, its knowledge dated 2026-09-30. Releases, with binaries for
Linux, macOS and Windows, are on GitHub.

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

The nine targets:

| Target | Writes | For |
|---|---|---|
| devcontainer | `.devcontainer/devcontainer.json`, and `compose.yaml` with services | Dev containers, Codespaces |
| copilot | `.github/workflows/copilot-setup-steps.yml` | GitHub Copilot's coding agent |
| cursor | `.cursor/environment.json` and `.cursor/Dockerfile` | Cursor's cloud agents |
| claude-code | `.claude/settings.json` (a SessionStart hook) and `.preconfig/claude-code.sh` | Claude Code cloud sessions |
| codex | `.preconfig/codex.md`, the values to enter in Codex's settings | OpenAI Codex cloud environments |
| ona | `.ona/config.yaml`, with the devcontainer target | Ona (formerly Gitpod) |
| devin | `.devin/blueprint.yaml` | Devin |
| cloud-init | `cloud-init.yaml` | a plain cloud server |
| script | `.preconfig/setup.sh` | anything that runs a shell script |

`targets:` in preconfig.yaml picks them. Without it, build writes the five
that 0.1 wrote (devcontainer, copilot, cursor, cloud-init, script), so a new
version never adds files to a repository unasked; the others are listed by
name. Targets that run the setup script bring the script target along, and
ona brings devcontainer.

## The agent platforms added in 0.2

Each platform's own documentation was read on 2026-11-04 before its target
was written. Two of them take their setup from the repository, two from
their own settings, so the targets differ in what they can do.

**Claude Code cloud sessions.** The environment (network access, environment
variables, a setup script) is set in Claude Code's settings; a repository
reaches a session through `.claude/settings.json`. preconfig adds a
SessionStart hook there, merged into the file you already have: your own
settings and hooks stay (comments don't, as the file is written back as plain
JSON), and preconfig's hook replaces an earlier one. The hook runs
`.preconfig/claude-code.sh`, which does nothing on your own machine and, in a
cloud session, sets up the machine and the project once per version of the
setup script, starts the services every session (a session keeps files but
not running processes), and hands the project's environment variables to
Claude's later commands through `$CLAUDE_ENV_FILE`. Nothing goes into the
settings' own `env`, which local sessions read too. When a step fails, the
hook tells Claude which one and shows the end of its log; the session still
starts. After changing dependencies, run `bash .preconfig/setup.sh project`. The setup script
needs only the Trusted network level unless it reaches NodeSource or the
PostgreSQL or Redis apt repositories, which build names. Tested: the hook's
command run in a Claude Code cloud session (results/platforms/claude-code-hook-run.txt),
which found that its image makes `python3` Python 3.13; the setup script
now points `python3` at Ubuntu's 3.12 when that's the version asked for.
Not tested: Claude Code reading the settings file in a fresh session.

**OpenAI Codex.** Codex keeps environments in its own settings, where it
works out the setup with you and records an install script and a start
step; no repository file configures it. The codex target writes
`.preconfig/codex.md` with what goes in each field: the install script, the
maintenance script of Codex Cloud (Legacy) environments, the services'
start, the ready check, the environment variables, how to hold the secrets
(in Legacy environments they reach only the setup script) and the network
access setup needs. Not run on Codex.

**Ona.** Ona builds the dev container from `devcontainer.json` and reads tasks
and services from `.ona/config.yaml`. The ona target adds the ready check as a
manual task, and, since Ona requires `network_mode: host` on every Compose
service, puts the dev container's services on the host network (on Docker
Desktop that needs host networking turned on). check reads `.ona/config.yaml`
and the older names Ona still reads, and warns about `.gitpod.yml`. Ona's CLI
validates dev containers but not the tasks file, and neither ran here; the
file is checked against the documented schema. Not run on Ona.

**Devin.** Devin builds a snapshot from `.devin/blueprint.yaml` once it is
synced in Devin's settings. The devin target puts the machine step in
`initialize`, the environment in `$ENVRC`, the project step in
`maintenance`, and the ready check and the services' start in `knowledge`,
because nothing runs at a Devin session's start. check applies the
blueprint reference's rules (run or uses, no uses in maintenance, no
post-build in a repository). Devin documents no validator. Not run on Devin.

## On every pull request: the GitHub Action

```yaml
# .github/workflows/preconfig.yml
on: [pull_request]
jobs:
  preconfig:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: Preconfiguration/preconfig@v0.3.0
```

The action downloads the preconfig release for the runner, checks it against
the release's SHA256SUMS, and runs `preconfig check`: each finding becomes an
annotation on the line it's about, and a table goes to the job summary. When
the pull request changes `preconfig.yaml` or `.preconfig/setup.sh`, it also
runs `preconfig verify`, which runs the setup and the ready check in a clean
Ubuntu container (Linux runners have the Docker it needs). If the setup
isn't ready, Preconfig Doctor's reading of the log goes into the summary.
The step fails when check finds an error or verify isn't ready.

Inputs: `version` (default: the action's own tag, or the latest release),
`dir` (the folder with preconfig.yaml), `verify` (`auto`, `true` or `false`),
`verify-args` (such as `--pass-env PAYMENTS_API_KEY`, with the secret in the
step's `env`), `check-args`. Outputs: `check-exit-code` and `verify` (`ready`,
`not-ready`, `setup-failed`, `error` or `skipped`).

Outside GitHub, the same output is `preconfig check --format github` and
`--summary FILE` (Markdown). check also warns (P001) once the knowledge base
is more than 60 days old, since the platforms change their formats often;
`--max-age DAYS` changes the limit and 0 turns it off.

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
results/platforms/   recorded runs on the agent platforms
action.yml           the GitHub Action
```

Go 1.24 or later, standard library only. Core (cmd/preconfig and internal/
apart from Doctor) is 7,679 lines in 20 files, with 3,619 lines of tests.
Doctor adds 2,999 lines, with 820 lines of tests.

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

Each release on GitHub has preconfig for Linux (x86-64 and Arm), macOS (Intel
and Apple silicon) and Windows (x86-64), packed with the license files, at
`releases/latest/download/preconfig_{linux,darwin}_{amd64,arm64}.tar.gz` and
`preconfig_windows_amd64.zip`, with a `SHA256SUMS`:

```
sha256sum -c --ignore-missing SHA256SUMS      # Linux
shasum -a 256 -c --ignore-missing SHA256SUMS  # macOS
```

Before a release goes out, `tools/smoke.sh` runs each binary on its own
system: Linux on x86-64 and Arm, macOS on Apple silicon (and Intel where the
runner can), and Windows. Binaries aren't kept in the repository.

## Releases

Releases go out on their own. Raise `Version` in `internal/kb/kb.go` on main;
when the tests pass on that commit, `.github/workflows/release.yml` builds the
binaries with `tools/build.sh`, smoke-tests them, tags the commit `vX.Y.Z` and
publishes the release. Nothing is published unless every build and check
passes.

## Test and reproduce the figures

| Command | What it reproduces |
|---|---|
| `go test ./...` | 137 test functions, Core's 122 and Doctor's 15; the golden files; the setup script and the Claude Code hook run under bash |
| `go test -coverpkg=<Core's packages> ./cmd/preconfig ./internal/...` | Coverage: 92.8% of Core's statements; Doctor's own package 84.1% |
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

A working compiler, checker and verifier, tested on Linux, with release
binaries smoke-tested on Linux, macOS and Windows, and a Doctor measured on 39
recorded cases, 11 of them recorded after its rules were frozen. Of the agent
platforms, only Claude Code's cloud sessions have run preconfig's setup (the
hook's command, recorded). Not done yet: runs on Copilot, Cursor, Codex, Ona,
Devin and Codespaces themselves; the Node.js, Go, PostgreSQL (other than 16)
and Redis 8 install paths on a clean machine; real repositories at scale;
cloud-init on a real server.

Contact: info@preconfiguration.com
