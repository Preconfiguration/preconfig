#!/usr/bin/env python3
"""Plants one bug at a time in Preconfig Doctor and runs its tests, to see
whether they notice. Same method as tools/mutate.py for the core: a small,
realistic mistake, the tests run, the source put back.

    python3 tools/doctor/mutate.py          # every bug
    python3 tools/doctor/mutate.py 2 5      # only bugs 2 and 5

Results go to tools/doctor/mutate-results.json.
"""
import json
import os
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# (file, text to replace, replacement, what the bug is)
BUGS = [
    ("internal/doctor/read.go", "lg.Steps[idx].Code, _ = strconv.Atoi(fm[2])", "lg.Steps[idx].Code = 1",
     "reader: every failed step reports exit code 1"),
    ("internal/doctor/read.go", "lg.Lines = append(lg.Lines, Line{N: n, Text: strings.TrimRight(m[3], \" \\t\")})",
     "lg.Lines = append(lg.Lines, Line{N: n, Text: l})", "reader: docker's #N prefixes are kept, so the script's markers are missed"),
    ("internal/doctor/read.go", "\t\t\tlg.Steps[cur].Failed = true\n\t\t\tif c := exitCodeRe", "\t\t\tif c := exitCodeRe",
     "reader: the Actions step with an error isn't marked failed"),
    ("internal/doctor/read.go", "\tif !lg.Failed && lastDone {", "\tif !lg.Failed && (lastDone || true) {",
     "reader: a log cut off in the middle of a step counts as passing"),
    ("internal/doctor/rules.go", '\t{"D408", ruleTestsFailed},\n', "",
     "rules: a test failing on its own assertion gets no rule"),
    ("internal/doctor/rules.go", '\t{"D402", rulePgAuth},', '\t{"D408", ruleTestsFailed},\n\t{"D402", rulePgAuth},',
     "rules: failing tests are blamed on the code before the service rules run"),
    ("internal/doctor/rules.go", 'if c.spec != nil && contains(c.spec.Packages, "build-essential") {',
     'if c.spec != nil && !contains(c.spec.Packages, "build-essential") {', "rules: the compiler is blamed only when build-essential is in the spec"),
    ("internal/doctor/knowledge.go", '"Python.h":                "python3-dev",', '"Python.h":                "python3-venv",',
     "knowledge: Python.h comes from the wrong package"),
    ("internal/doctor/rules.go", 'f.changes = []Change{{Op: "add-service", Key: "redis", Value: base.DistroRedis}}',
     'f.changes = []Change{{Op: "add-service", Key: "redis", Value: "8"}}', "rules: Redis is added at a version Ubuntu doesn't ship"),
    ("internal/doctor/rules.go", "if c.spec != nil && contains(c.spec.Secrets, name) {", "if false && c.spec != nil && contains(c.spec.Secrets, name) {",
     "rules: a secret that isn't set is read as a missing variable"),
    ("internal/doctor/mask.go", "`([A-Za-z][A-Za-z0-9+.-]*://[^:/@\\s]+:)[^@\\s/]+@`", "`([A-Za-z][A-Za-z0-9+.-]*://[^:/@\\s]+:)[^@\\s/]{20,}@`",
     "mask: a short password in a URL is shown"),
    ("internal/doctor/fix.go", 'func quote(v string) string { return `"` + v + `"` }', "func quote(v string) string { return v }",
     "fix: versions go into the spec unquoted, so 3.10 reads as 3.1"),
    ("internal/doctor/rules.go", "\t\t\tcase \">=\":\n\t\t\t\tif v < n {", "\t\t\tcase \">=\":\n\t\t\t\tif v <= n {",
     "versions: >=3.13 leaves out 3.13 itself"),
    ("cmd/preconfig/doctor.go", "\tcase d.Outcome == doctor.Passed:\n\t\tcode = doctorNothing", "\tcase d.Outcome == doctor.Passed:\n\t\tcode = doctorExplained",
     "command: a passing log exits 1"),
]


def run_tests():
    start = time.time()
    p = subprocess.run(["go", "test", "-count=1", "./internal/doctor/", "./cmd/preconfig/"], cwd=ROOT,
                       capture_output=True, text=True, timeout=900)
    out = p.stdout + p.stderr
    failed = sorted({l.split()[2] for l in out.splitlines() if l.strip().startswith("--- FAIL:")})
    build = "build failed" in out or "[build failed]" in out
    return p.returncode, failed, build, time.time() - start


def main():
    pick = {int(a) for a in sys.argv[1:]}
    rc, failed, _, secs = run_tests()
    if rc != 0:
        print("the tests fail before any bug is planted:", failed)
        sys.exit(2)
    print(f"baseline: all tests pass ({secs:.1f} s)\n")
    results = []
    for i, (path, old, new, what) in enumerate(BUGS, 1):
        if pick and i not in pick:
            continue
        full = os.path.join(ROOT, path)
        src = open(full, encoding="utf-8").read()
        if src.count(old) != 1:
            print(f"{i:2}. SKIPPED  {what}: the text to change appears {src.count(old)} times in {path}")
            results.append({"n": i, "bug": what, "file": path, "result": "skipped"})
            continue
        try:
            open(full, "w", encoding="utf-8").write(src.replace(old, new, 1))
            rc, failed, build, secs = run_tests()
        finally:
            open(full, "w", encoding="utf-8").write(src)
        result = "caught by the compiler" if build else ("caught" if rc != 0 else "SURVIVED")
        shown = ", ".join(failed[:3]) + (f" and {len(failed) - 3} more" if len(failed) > 3 else "")
        print(f"{i:2}. {result:8} {what}" + (f"  [{shown}]" if failed else ""))
        results.append({"n": i, "bug": what, "file": path, "result": result, "failed_tests": failed})
    caught = sum(1 for r in results if r["result"].startswith("caught"))
    tried = sum(1 for r in results if r["result"] != "skipped")
    print(f"\n{caught} of {tried} planted bugs caught")
    json.dump(results, open(os.path.join(ROOT, "tools", "doctor", "mutate-results.json"), "w"), indent=2)
    sys.exit(0 if caught == tried else 1)


if __name__ == "__main__":
    main()
