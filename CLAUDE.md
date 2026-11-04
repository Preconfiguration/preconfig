# Notes for Claude

This file says where the next session starts and what comes next. What comes
next is agreed with the owner (YK) before it goes here, and work starts only
when he says go.

preconfig runs on the owner's low maintenance workflow: Claude does the code,
tests, docs, CI, releases, site and upkeep; the owner gives the go-ahead. Apache
2.0, "Copyright Preconfiguration.com 2026". README is the manual.

## Releases

Releases are automated. To release, raise `Version` in `internal/kb/kb.go`
and push to `main`; never ask the owner to publish one.
[RELEASING.md](RELEASING.md) has the whole procedure and the checks to run
afterwards. Binaries are not committed (bin/ was removed in 0.2.0); each
release builds its own with the current stable Go.

## Start here

- Latest release: v0.3.0 (November 4 2026). In progress: item 11 of
  softdev-plan.md (photozeitgeist/jollyroger.biz), approved by YK on
  2026-11-04 ("Go"): Phase 0 and Phase A as 0.2.0, Phase B as 0.3.0.
  Phase C (real runs on Copilot and Cursor) needs YK and isn't started.
- Build here with `export PATH=/usr/lib/go-1.26/bin:$PATH GOTOOLCHAIN=local
  GOPROXY=off GOFLAGS=-mod=mod`. go.mod says 1.24 (stdlib only); CI and
  releases use the current stable Go.
- Site: photozeitgeist/preconfiguration.com (Hugo, standard template:
  content and config values only; pages in content/pages, posts in
  content/posts, mainSections = ["posts"]).

## Layout

cmd/preconfig (CLI), cmd/wasm (browser engine, TinyGo), internal/spec
(preconfig.yaml), internal/gen (targets and the setup script), internal/check
(platform rules, cross-checks, drift), internal/detect, internal/verify,
internal/kb (the dated platform facts: every name, path and rule a target
writes or checks lives here), internal/doctor (Preconfig Doctor), the YAML,
JSONC, tree and diff helpers. The browser demos (demo/app, demo/doctor) run
engines built from 0.1.0 and aren't rebuilt (TinyGo isn't in the sandbox).

## Now: 0.2 and 0.3, item 11 of softdev-plan.md

Phase 0 (with 0.2.0): this file; bin/ removed (THIRD-PARTY-LICENSES.txt
moved to the root, the release packs it from there); README stops calling
itself the Alpha; workflows on stable Go (upkeep).

Phase A, 0.2.0: targets for Claude Code cloud environments, OpenAI Codex
environments, Ona (Gitpod) and Devin, each researched from the platform's
own docs first. Done when each new target's file is accepted by its
platform's validator or a recorded run; where a platform has neither, the
file is checked against its documented schema and the README says so.

Phase B, 0.3.0: a GitHub Action running `preconfig check` and `verify` on
pull requests; a dated knowledge-base check that warns when the KB is older
than N days (60 proposed).

Steps:
1. [x] Phase 0 cleanup
2. [x] research the four formats (2026-11-04; Codex and Claude Code keep
       their environments in their own settings, Ona and Devin read files)
3. [x] the four targets, checks, tests, README; the hook's command run in
       this Claude Code cloud session (results/platforms/), which found
       the image's python3 is 3.13 (fixed in setup.sh)
4. [x] independent review, two rounds (12 findings, then 1 should-fix and
       5 minor; env moved from settings.json to CLAUDE_ENV_FILE, quoted
       $ENVRC, failures on the hook's stdout, duplicate keys as JSON.parse
       reads them), fixes with tests; release 0.2.0
5. [x] after the release (tag on 15b0874, run 41837923182): six assets,
       Linux binary checked against SHA256SUMS and run on web-shop with
       the four new targets; preconfiguration.com 2b01c14 (post, About,
       Roadmap); softdevstudio note 2944e55; softdev-plan.md ae431fd
6. [x] Phase B: `check --format github|json`, `--summary`, `--path-prefix`,
       `--max-age` (P001, default 60 days); action.yml (download checked
       against SHA256SUMS, check annotations and job summary, verify when
       the spec or setup script changes, Doctor's reading on failure);
       action-test.yml (check on three OSes, verify ready and not-ready).
       Review two rounds (10 findings, then 1 should-fix and 5 minor:
       subfolder paths, verify without a spec, SHA pins, secrets on
       pull_request_target, releases before 0.3.0), fixed with tests
7. [ ] release 0.3.0; download job; site post and pages; softdevstudio
       note; softdev-plan.md
