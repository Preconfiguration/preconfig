#!/usr/bin/env python3
"""Runs preconfig doctor on every case of a corpus and scores it against the
cause each case was made with.

    python3 tools/doctor/evaluate.py CORPUS_DIR --preconfig BIN [--json OUT] [--set NAME]

For each case it rebuilds the case's repository (so Doctor gets the spec the
setup was built from), runs doctor on the full log and, for verify cases, on
verify's terminal output too, and compares:

  right       the outcome and the category match the case, and Doctor's changes
              are exactly the ones the case calls for
  partial     the category matches and every change Doctor makes is one the case
              calls for, but one or more are missing: an incomplete fix, never a
              wrong one
  unknown     Doctor said it doesn't know (never a wrong fix)
  wrong       anything else: a wrong category, or a change the case doesn't call for

A case's "change" is one change, or a list of changes that are all needed; a
value may be a list of alternatives, any of which is right.

A diagnosis is also timed: the run of the whole command, and Doctor's own time
from the Go benchmark are reported separately (see tools/doctor/README.md).
"""
import argparse
import json
import os
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import corpus  # noqa: E402


def load_cases(root):
    cases = []
    for name in sorted(os.listdir(root)):
        p = os.path.join(root, name, "case.json")
        if os.path.exists(p):
            cases.append(json.load(open(p)))
    return cases


def needed(expect):
    """The changes a case calls for: none, one, or several that are all needed."""
    ch = expect.get("change")
    if ch is None:
        return []
    return ch if isinstance(ch, list) else [ch]


def one_matches(want, g):
    """Doctor's change g is the change want. Cases name a service or runtime
    "name" and a service's "version"; Doctor's changes call them key and value."""
    if g.get("op") != want.get("op"):
        return False
    rename = {"name": "key", "version": "value"}
    for k, v in want.items():
        if k == "op":
            continue
        got = str(g.get(rename.get(k, k), ""))
        if got not in ([str(x) for x in v] if isinstance(v, list) else [str(v)]):
            return False
    return True


def change_verdict(expect, got):
    """right, partial or wrong, for Doctor's changes against the case's."""
    want = needed(expect)
    if any(not any(one_matches(w, g) for w in want) for g in got):
        return "wrong"
    found = sum(1 for w in want if any(one_matches(w, g) for g in got))
    return "right" if found == len(want) else "partial"


def score(expect, d):
    """right, partial, unknown or wrong, with a reason."""
    if expect["outcome"] == "passed":
        if d["outcome"] == "passed":
            return "right", ""
        return "wrong", "a passing log got a diagnosis: " + d.get("cause", "")
    if d["outcome"] == "passed":
        return "wrong", "a failing log was read as passing"
    if d.get("category") == "unknown":
        return "unknown", d.get("cause", "")
    reasons = []
    if d.get("category") != expect["category"]:
        reasons.append(f"category {d.get('category')} not {expect['category']}")
    cv = change_verdict(expect, d.get("changes") or [])
    if cv == "wrong":
        reasons.append(f"changes {d.get('changes', [])} not {expect.get('change')}")
    host = expect.get("host")
    if host and host not in (d.get("cause", "") + " " + d.get("advice", "")):
        reasons.append(f"host {host} not named")
    phase = expect.get("phase")
    if phase and d.get("phase") and d.get("phase") != phase:
        reasons.append(f"phase {d.get('phase')} not {phase}")
    if reasons:
        return "wrong", "; ".join(reasons)
    if cv == "partial":
        return "partial", f"changes {d.get('changes', [])}, the case needs {expect.get('change')}"
    return "right", ""


def run_doctor(binary, log, spec_path):
    cmd = [binary, "doctor", "--json"]
    if spec_path:
        cmd += ["--spec", spec_path]
    cmd.append(log)
    t = time.perf_counter()
    p = subprocess.run(cmd, capture_output=True, text=True)
    ms = (time.perf_counter() - t) * 1000
    if p.stdout.strip() == "":
        raise SystemExit(f"doctor printed nothing for {log}: {p.stderr}")
    return json.loads(p.stdout)["diagnosis"], ms, p.returncode


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("corpus")
    ap.add_argument("--preconfig", required=True)
    ap.add_argument("--json", default="")
    ap.add_argument("--set", default="")
    args = ap.parse_args()
    defs = {c["id"]: c for c in corpus.CASES + getattr(corpus, "HOLDOUT", [])}
    rows = []
    with tempfile.TemporaryDirectory(prefix="doctor-eval-") as tmp:
        for case in load_cases(args.corpus):
            d = os.path.join(args.corpus, case["id"])
            spec_path = ""
            if case["id"] in defs:
                v = corpus.make_variant(defs[case["id"]], os.path.join(tmp, case["id"]))
                spec_path = os.path.join(v, "preconfig.yaml")
            inputs = []
            if os.path.exists(os.path.join(d, "log.txt")):
                inputs.append(("log", os.path.join(d, "log.txt")))
            if os.path.exists(os.path.join(d, "out.txt")):
                inputs.append(("terminal", os.path.join(d, "out.txt")))
            if os.path.exists(os.path.join(d, "build.txt")):
                inputs.append(("docker", os.path.join(d, "build.txt")))
            err = os.path.join(d, "err.txt")
            if os.path.exists(err) and ": error S" in open(err).read():
                # verify refused to start: what it printed is the spec's errors.
                inputs = [("stderr", err)]
            for form, path in inputs:
                diag, ms, code = run_doctor(args.preconfig, path, spec_path)
                verdict, why = score(case["expect"], diag)
                lines = sum(1 for _ in open(path, errors="replace"))
                rows.append({"set": args.set, "id": case["id"], "form": form, "lines": lines,
                             "expect": case["expect"], "verdict": verdict, "why": why, "ms": round(ms, 1),
                             "exit": code, "diagnosis": diag})
                print(f"{case['id']:28} {form:9} {lines:5} lines  {verdict:8} {diag.get('rule') or '-':5} "
                      f"{diag.get('category') or diag['outcome']:10} {why[:90]}")
    total = {}
    for r in rows:
        k = r["form"]
        total.setdefault(k, {"right": 0, "partial": 0, "unknown": 0, "wrong": 0})[r["verdict"]] += 1
    print()
    for k, v in total.items():
        n = sum(v.values())
        print(f"{k:9} {n:3} logs: {v['right']} right, {v['partial']} partial, {v['unknown']} unknown, {v['wrong']} wrong")
    if args.json:
        json.dump(rows, open(args.json, "w"), indent=1)


if __name__ == "__main__":
    main()
