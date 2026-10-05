# Preconfig Doctor's tools

Doctor reads the log of a failed setup and says why. These tools make the logs
it is measured on, score it, and prove its fixes. All of them run on the test
machine the Alpha used: Linux, Docker, and a network that reaches Ubuntu's
archive, PyPI and npm but not every source the targets use.

| Tool | What it does |
|---|---|
| `corpus.py` | Records the corpus: each case copies a sample repository, plants one known cause of failure, and runs `preconfig verify` or builds Cursor's Dockerfile, keeping everything the run printed |
| `evaluate.py` | Runs `preconfig doctor` on every log of a corpus and scores it: right, unknown or wrong |
| `prove.py` | For every fix Doctor writes into a spec, applies it with `doctor --fix` and runs the setup again on a clean machine, round after round, until READY |
| `mutate.py` | Plants one bug at a time in Doctor and checks that the tests catch it |
| `check-packages.sh` | Checks every package name Doctor can write against Ubuntu 24.04's archive |
| `demo-data.py` | Writes the demo's recorded cases from the corpus and the proofs |
| `wasm.sh` | Runs Doctor's browser build on every corpus log and compares its answers, fixed specs and rebuilt files with the command line's (with `wasm-native.py` and `wasm-run.cjs`) |
| `demo-check.py` | Checks the Doctor demo headless at a desktop and a phone width: every recorded case against the command line, the fix, the proof, no sideways scroll, no console errors |

## How the corpus is made

A case is made by changing one thing in a sample repository: leaving Redis out
of the spec, misspelling a package, adding a test that needs a locale. The
cause is written into the case's `case.json` when the case is designed, before
the log exists. Five labels were corrected after reading the logs, from what
the logs showed; each case says why in its `note`:

- `psycopg2-1` was designed to fail on a missing `pg_config`. The PostgreSQL
  service's own packages provide one, so the build got as far as the compiler.
- `python-3.13` was designed to fail on a blocked download. uv downloaded
  Python 3.13.15 and the setup reached READY, so it is a passing log.
- `github-requirement`, in the holdout, was designed the same way, and pip
  cloned the repository from GitHub: a passing log too.
- `lxml-source`, in the holdout, asks for libxslt's headers as well as
  libxml2's, so the right fix adds both.
- `pillow-source`, in the holdout, stopped on libjpeg, because python3-dev
  had already brought zlib's headers.

A case's `change` is one change, or a list of changes that are all needed, and
a value may list alternatives. The scorer calls an answer right when Doctor's
changes are exactly the ones the case needs, partial when some are missing and
none is extra, unknown when Doctor says it doesn't know, and wrong otherwise,
including a right category with the wrong host.

## Tuning and holdout

The rules were written from the known formats of apt, pip, curl, libpq,
redis-py, Docker and the setup script, then run on the tuning corpus
(`testdata/doctor/corpus`). Where a tuning log showed a format the rules
missed, the rules were changed; `TUNING.md` lists each change and the case
behind it. So the tuning corpus doesn't measure the rules independently.

The holdout corpus (`testdata/doctor/holdout`) was designed along with the
rules, so its cases aren't blind to them, and recorded only after the rules
were frozen:
`results/doctor/freeze.txt` has the SHA-256 of each file that holds a rule.
Its score is the honest measure. `TUNING.md` also records one slip in the
order of scoring it.

## Running them

    go build -o /tmp/preconfig ./cmd/preconfig
    python3 tools/doctor/corpus.py testdata/doctor/corpus --preconfig /tmp/preconfig --network host --ca-file CA.crt
    python3 tools/doctor/corpus.py testdata/doctor/holdout --holdout --preconfig /tmp/preconfig --network host --ca-file CA.crt
    python3 tools/doctor/evaluate.py testdata/doctor/corpus --preconfig /tmp/preconfig
    python3 tools/doctor/evaluate.py testdata/doctor/holdout --preconfig /tmp/preconfig
    python3 tools/doctor/prove.py testdata/doctor/corpus results/doctor/proofs --preconfig /tmp/preconfig --network host --ca-file CA.crt
    python3 tools/doctor/prove.py testdata/doctor/holdout results/doctor/proofs-holdout --preconfig /tmp/preconfig --network host --ca-file CA.crt
    python3 tools/doctor/mutate.py
    bash tools/doctor/check-packages.sh --network host
    bash tools/doctor/wasm.sh
    python3 tools/doctor/demo-check.py demo/doctor /tmp/preconfig

Verify runs go one at a time: with `--network host`, two setups at once share
ports 5432 and 6379, and one of them fails for that reason alone.
