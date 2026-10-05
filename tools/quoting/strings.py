"""Writes the strings that tools/quoting checks: every string of one to three
characters from a set of YAML's special characters, the values YAML 1.1 or 1.2
read as something other than a string, and 20,000 random strings."""
import itertools
import json
import random
import sys

chars = list("aZ09_./-:@=+,() #'\"!&*%?|>{}[]`~\\\t") + ["é", "\u0085", "\u00a0"]
out = set()
for n in (1, 2, 3):
    for combo in itertools.product(chars, repeat=n):
        out.add("".join(combo))
out.update([
    "yes", "no", "on", "off", "y", "n", "Y", "N", "~", "null", "Null", "NULL", "true", "True", "FALSE",
    "1_000", "0x1F", "0o17", "0b101", "017", "1e3", "1E3", "1.5e-3", "1:30", "190:20:30", ".inf", "-.inf",
    "+.inf", ".NaN", ".5", "2026-09-30", "2026-9-3", "2001-12-14t21:59:43.10-05:00", "2001-12-14 21:59:43.10 -5",
    "12:30:45", "-1", "+1", "1.", "1.0", "0.1", "-", "--", "---", "...", "<<", "=", "!", "&a", "*a", "%x", "@x",
    "`x", "?x", "? x", ":x", "- x", "-x", "a:b", "a: b", "a #b", "a#b", "a ", "a\tb",
    "postgres://u:p@localhost:5432/db", "redis://localhost:6379/0", "ubuntu-24.04", "22", "3.12", "1.25",
    ".venv/bin/pytest -q", "pnpm@10", "go test ./...", "http://x.y/z?a=b&c=d", "a,b", "(x)", "x(y)", "y)",
    "1-3.12-bookworm", "16-alpine", "0", "00", "08", "09", "1_2", "0x", "0o", "1__0", "6.8523015e+5",
    "685.230_15e+03", "685_230.15", "-0", "+.5", "--1",
])
random.seed(7)
alpha = "aZ09_./-:@=+,() #'\"!&*%?|>{}[]`~\\é"
for _ in range(20000):
    out.add("".join(random.choice(alpha) for _ in range(random.randint(1, 8))))
json.dump(sorted(out), open(sys.argv[1], "w"))
print(len(out), "strings")
