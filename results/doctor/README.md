# Preconfig Doctor's results

Measured on the test machine, two CPUs of an Intel Xeon at 2.8 GHz with 7 GB
of memory, Linux 6.18 and Docker 29, on September 30 and October 1, 2026.

| File or folder | What it holds |
|---|---|
| `freeze.txt` | The time the rules were frozen, and the SHA-256 of every file that holds a rule. `sha256sum -c freeze.txt` from the tree's root checks them |
| `corpus-recording.log` | The first recording of the tuning corpus, case by case. Two cases were recorded again later: `port-5432-taken`, after the port blocker was fixed, and `python-3.13-blocked`, renamed `python-3.13` when it passed |
| `holdout-recording.log` | The holdout's recording, after the freeze. `needs-python-below-3.12` ran in the same batch, after the freeze, but the batch stopped printing when `spec-error`'s first attempt failed, so its line is missing; it was recorded at 19:27:37 UTC on September 30. `spec-error` was recorded on its own at 19:29:07 UTC, once `corpus.py` let a spec that doesn't build go on to verify. Each case's `case.json` has its time |
| `tuning-eval.json`, `holdout-eval.json` | `evaluate.py`'s scores: each log's verdict, Doctor's full diagnosis and the command's time |
| `proofs/` | The proofs on the tuning corpus: each case's rounds, with the diagnosis, the diff of `preconfig.yaml` and the new run's log, and `proofs.json` |
| `proofs-holdout/` | The same on the holdout corpus |
| `proofs-2026-09-30-cut-off/` | The first proof run, cut off when the machine restarted; kept as a record. Its six cases were proven again in `proofs/` |
| `packages.txt` | Every package name Doctor can write, looked up in Ubuntu 24.04's archive |
| `measure/` | The measurement round: tests, coverage, benchmark, planted bugs (`mutate-first-run.txt` is the first run, before the two missing checks were added), fuzzing, the browser build against the command line, the package names again, the demo's headless check, the v2 run kit run offline and with verify, the binaries' sizes, and the machine |
