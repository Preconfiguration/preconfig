#!/usr/bin/env python3
"""Plants one bug at a time in the engine and runs the tests, to see whether
the tests notice. Each bug is a small, realistic mistake: a wrong comparison,
a check left out, a value typed wrong. A bug the tests don't catch "survives"
and points at a missing test.

    python3 tools/mutate.py            # every bug
    python3 tools/mutate.py 3 7        # only bugs 3 and 7

The source is restored after each run, even when the run is interrupted.
"""
import json
import os
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# (file, text to replace, replacement, what the bug is)
BUGS = [
    ("internal/spec/spec.go", "nameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)",
     "nameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,63}$`)", "spec: names may contain spaces"),
    ("internal/spec/spec.go", 'if secretKey.MatchString(k.Value) && v != "" {',
     'if secretKey.MatchString(k.Value) && v == "x" {', "spec: a secret's value in env is let through"),
    ("internal/spec/spec.go", "r == '\\u2028' || ", "", "spec: the line separator U+2028 is let through"),
    ("internal/kb/kb.go", "var NodeMajors = []int{20, 22, 24}", "var NodeMajors = []int{20, 21, 22, 24}",
     "knowledge: Node.js 21, an odd release, counts as installable"),
    ("internal/kb/kb.go", 'var Targets = []string{"devcontainer", "copilot", "cursor", "cloud-init", "script"}',
     'var Targets = []string{"devcontainer", "copilot", "cursor", "cloud-init"}', "knowledge: the script target is left out of the defaults"),
    ("internal/kb/kb.go", "CopilotMaxTimeout   = 59", "CopilotMaxTimeout   = 60", "knowledge: Copilot's timeout limit is off by one"),
    ("internal/kb/kb.go", 'CopilotJobName      = "copilot-setup-steps"', 'CopilotJobName      = "copilot-setup"',
     "knowledge: the Copilot job has the wrong name"),
    ("internal/yaml/yaml.go", "if !p.opts.AllowDuplicateKeys {\n\t\t\t\treturn nil, p.errf(ln, indent+1,",
     "if false && !p.opts.AllowDuplicateKeys {\n\t\t\t\treturn nil, p.errf(ln, indent+1,", "YAML: a key written twice is accepted"),
    ("internal/yaml/yaml.go", "if t[0] != '.' && (t[0] < '0' || t[0] > '9') {", "if t[0] < '0' || t[0] > '9' {",
     "YAML: .5 is read as text, not a number"),
    ("internal/yaml/yaml.go", 'b.WriteString("\\u0085")', 'b.WriteString("\\u00a0")', "YAML: the \\N escape gives the wrong character"),
    ("internal/yaml/yaml.go", 'text += strings.Repeat("\\n", trailing)', "_ = trailing", "YAML: |+ drops the trailing blank lines"),
    ("internal/jsonc/jsonc.go", "if r.i < len(r.s) && r.s[r.i] == '}' && !r.opts.TrailingCommas {",
     "if r.i < len(r.s) && r.s[r.i] == '}' && false {", "JSON: a trailing comma in an object is accepted everywhere"),
    ("internal/jsonc/jsonc.go", "if !r.opts.Comments {", "if false {", "JSON: comments are accepted everywhere"),
    ("internal/gen/script.go", "\tb.line(`trap on_error ERR`)\n", "", "script: no trap, so a failed step isn't named"),
    ("internal/gen/copilot.go", 'for _, ev := range []string{"push", "pull_request"} {', 'for _, ev := range []string{"push"} {',
     "Copilot: pull requests don't run the setup"),
    ("internal/gen/cursor.go", 'build.Set("context", tree.NewStr(".."))', 'build.Set("context", tree.NewStr("."))',
     "Cursor: the build context is .cursor, not the repository"),
    ("internal/gen/devcontainer.go", 'f := tree.NewMap().Set("version", tree.NewStr(s.Node))', 'f := tree.NewMap().Set("version", tree.NewStr("lts"))',
     "dev container: the Node.js feature ignores the version"),
    ("internal/gen/cloudinit.go", 'b.line("#cloud-config")', 'b.line("# cloud-config")', "cloud-init: the header has a space"),
    ("internal/tree/tree.go", "case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_', c == '/':",
     "case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_', c == '/', c >= '0' && c <= '9':", "quoting: values starting with a digit go unquoted"),
    ("internal/gen/gen.go", "var safeShell = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)",
     "var safeShell = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./$-]+$`)", "shell: $ goes unquoted"),
    ("internal/check/check.go", "if v > float64(kb.CopilotMaxTimeout) {", "if v >= float64(kb.CopilotMaxTimeout) {",
     "check: a timeout of exactly 59 is flagged"),
    ("internal/check/check.go", "jsonc.Parse(text, jsonc.Options{Comments: true, TrailingCommas: false})",
     "jsonc.Parse(text, jsonc.Options{Comments: true, TrailingCommas: true})", "check: Cursor's trailing commas pass"),
    ("internal/check/facts.go", "for i := range w {", "for i := range w[:1] {", "check: only the major version is compared"),
    ("internal/check/facts.go", "distinct[normalize(t, v.Version)] = true", "distinct[v.Version] = true",
     "check: 22 and 22.11 count as different versions"),
    ("internal/check/facts.go", "if actual == f.Content {", "if strings.Contains(actual, generatedTag) {",
     "check: a file with preconfig's header is trusted without comparing"),
    ("internal/detect/detect.go", 'd.setup = append(d.setup, item{"pnpm install --frozen-lockfile", "pnpm-lock.yaml"})',
     'd.setup = append(d.setup, item{"pnpm install", "pnpm-lock.yaml"})', "detect: pnpm installs ignore the lockfile"),
    ("internal/detect/detect.go", "secretKey = regexp.MustCompile(`(?i)(TOKEN|SECRET|",
     "secretKey = regexp.MustCompile(`(?i)(SECRET|", "detect: names ending in TOKEN aren't treated as secrets"),
    ("internal/detect/detect.go", '"iron": "20"', '"iron": "22"', "detect: lts/iron maps to the wrong Node.js"),
    ("internal/verify/verify.go", 'if strings.HasPrefix(r.res.FailedStep, "ready") {\n\t\t\tr.res.Code = NotReady',
     'if strings.HasPrefix(r.res.FailedStep, "readyX") {\n\t\t\tr.res.Code = NotReady', "verify: a failed ready check counts as a failed setup"),
    ("internal/verify/verify.go", 'if strings.HasPrefix(t, "E ") && !seen[t] {', 'if strings.HasPrefix(t, "E:") && !seen[t] {',
     "verify: the test runner's error lines aren't picked for the summary"),
    ("internal/textdiff/textdiff.go", 'fmt.Fprintf(&out, "@@ -%s +%s @@\\n", rng(aStart, aLen), rng(bStart, bLen))',
     'fmt.Fprintf(&out, "@@ -%s +%s @@\\n", rng(bStart, bLen), rng(aStart, aLen))', "diff: the hunk header swaps old and new"),
    ("cmd/preconfig/main.go", "\t\t\t\treturn exitSpec\n", "\t\t\t\treturn exitFindings\n", "command: check exits 1, not 4, on a broken spec"),
]


def run_tests():
    start = time.time()
    p = subprocess.run(["go", "test", "-count=1", "./..."], cwd=ROOT, capture_output=True, text=True, timeout=900)
    out = p.stdout + p.stderr
    failed = [l.split()[2] for l in out.splitlines() if l.startswith("--- FAIL:")]
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
        if build:
            result = "caught by the compiler"
        elif rc != 0:
            result = "caught"
        else:
            result = "SURVIVED"
        shown = ", ".join(failed[:3]) + (f" and {len(failed) - 3} more" if len(failed) > 3 else "")
        print(f"{i:2}. {result:8} {what}" + (f"  [{shown}]" if failed else ""))
        results.append({"n": i, "bug": what, "file": path, "result": result, "failed_tests": failed})
    caught = sum(1 for r in results if r["result"].startswith("caught"))
    tried = sum(1 for r in results if r["result"] != "skipped")
    print(f"\n{caught} of {tried} planted bugs caught")
    json.dump(results, open(os.path.join(ROOT, "tools", "mutate-results.json"), "w"), indent=2)
    sys.exit(0 if caught == tried else 1)


if __name__ == "__main__":
    main()
