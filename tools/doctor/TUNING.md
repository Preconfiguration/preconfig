# Rule changes made while reading the tuning corpus

The rules were written first from the known formats of apt, pip, curl, libpq,
redis-py and the setup script's markers. Reading the tuning corpus showed
formats they didn't match. Each change below was made after seeing a tuning
case, so the tuning corpus doesn't measure the rules independently; the
holdout corpus, recorded after the rules were frozen, does.

| Case | What the log showed | Change |
|---|---|---|
| typo-package | `E: Unable to locate package libpq-devv`: the rule's pattern lacked the space before the name | Pattern fixed |
| no-postgres | libpq 17's `connection to server at "127.0.0.1", port 5432 failed: Connection refused`, without the address in brackets | Pattern accepts both forms |
| psycopg2-1 | `error: [Errno 2] No such file or directory: 'x86_64-linux-gnu-gcc'`, setuptools' newer wording | Written into the first version of the compiler rule, after this log was seen |
| go-blocked | The go command's `reading https://proxy.golang.org/...: 403 Forbidden`, which no pattern matched, so Doctor said "unknown" | Pattern added for the go command's blocked downloads (403, 407, 451) |

The rules were frozen after the last change above, on September 30, 2026,
and the holdout corpus was recorded after that.

## How the holdout was scored

The holdout corpus was recorded on September 30, 2026, after the freeze;
`results/doctor/freeze.txt` has the time and the SHA-256 of every file that
holds a rule, and those files haven't changed since. Before Doctor read the
holdout logs, each log was read by hand to check its label, and three labels
were corrected from what the logs showed: `github-requirement` reached READY,
`lxml-source` asks for libxslt's headers as well as libxml2's, and
`pillow-source` stopped on libjpeg because zlib's headers were already
installed. Each case's `note` says why.

One slip in that order is on record. While the scorer was being extended for
`lxml-source`, which needs two changes, a run of the test suite scored the ten
holdout cases recorded by then against their design labels, before the three
corrections were written into `case.json`. The corrections had already been
decided from the logs, and they were written as decided; no other label
changed. The label of `needs-python-below-3.12` was checked against its log
after that run and stood as designed. `spec-error` was recorded after it,
once `corpus.py` let a case whose spec doesn't build go on to verify.
