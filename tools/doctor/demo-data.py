#!/usr/bin/env python3
"""Writes doctor-cases.v1.js for the Doctor demo: the recorded logs it offers,
the spec each setup was built from, and the proof runs of their fixes.

    python3 tools/doctor/demo-data.py CORPUS_DIR PROOFS_DIR OUT.js

Every log is copied as the machine printed it, carriage returns included, so
the page reads exactly what the command line reads. The first preset is the
Alpha's own recorded run without Redis, from internal/verify/testdata.
"""
import json
import os
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

PRESETS = [
    # (case id, label, sub)
    ("alpha-no-redis", "Redis left out", "the Alpha's own run"),
    ("typo-package", "A misspelled package", "apt"),
    ("psycopg2-1", "psycopg2 from source", "a build with no compiler"),
    ("locale", "A locale the tests need", "pytest"),
    ("no-database-url", "DATABASE_URL left out", "pytest"),
    ("node-blocked", "NodeSource blocked", "the network"),
    ("missing-secret", "A secret that isn't set", "pytest"),
    ("code-failure", "A test that fails on the code", "pytest"),
    ("docker-typo-package", "Cursor's Dockerfile", "a Docker build"),
]


def main():
    corpus_dir, proofs_dir, out = sys.argv[1:4]
    proofs = {}
    p = os.path.join(proofs_dir, "proofs.json")
    if os.path.exists(p):
        for pr in json.load(open(p)):
            proofs[pr["id"]] = pr
    cases = []
    recorded = set()
    for cid, label, sub in PRESETS:
        if cid == "alpha-no-redis":
            log = open(os.path.join(ROOT, "internal", "verify", "testdata", "fail.log"), newline="").read()
            spec = open(os.path.join(ROOT, "testdata", "verify", "no-redis", "preconfig.yaml"), newline="").read()
            made = "Recorded on September 29, 2026, with Redis left out of the spec"
            proof = proofs.get("no-redis")
            recorded.add("2026-09-29")
        else:
            d = os.path.join(corpus_dir, cid)
            case = json.load(open(os.path.join(d, "case.json")))
            name = "build.txt" if case["kind"] == "docker" else "log.txt"
            log = open(os.path.join(d, name), newline="").read()
            spec = open(os.path.join(d, "spec.yaml"), newline="").read()
            made = case["made"].rstrip(".")
            proof = proofs.get(cid)
            recorded.add(case["recorded"][:10])
        pr = None
        if proof and proof.get("rounds"):
            rounds = []
            for r in proof["rounds"]:
                if not r.get("fixed"):
                    continue
                nxt = r.get("next") or ""
                if nxt == "passed":
                    after = {"passed": True}
                else:
                    step, _, rule = nxt.rpartition(": ")
                    after = {"passed": False, "step": step, "rule": rule, "same": step == r.get("step")}
                rounds.append({"changes": r.get("changes", []), "seconds": r.get("seconds"), "exit": r.get("exit"), "after": after})
            pr = {"ready": proof.get("ready", False), "rounds": rounds}
        kind = "verify" if cid == "alpha-no-redis" else case["kind"]
        cases.append({"id": cid, "label": label, "sub": sub, "kind": kind, "made": made, "log": log, "spec": spec, "proof": pr})
    days = sorted(recorded)
    long = {"2026-09-29": "September 29", "2026-09-30": "September 30", "2026-10-01": "October 1"}
    when = " and ".join(long.get(x, x) for x in days) + ", 2026"
    proven = ""
    if os.path.exists(p):
        day = time.strftime("%Y-%m-%d", time.gmtime(os.path.getmtime(p)))
        proven = long.get(day, day) + ", 2026"
    data = {"recordedAt": days[-1], "recordedLong": when, "provenLong": proven, "cases": cases}
    with open(out, "w") as f:
        f.write("/* Logs recorded on a clean Ubuntu 24.04 machine by preconfig verify, or by docker build,\n"
                " * for the Preconfig Doctor demo; written by tools/doctor/demo-data.py. */\n")
        f.write("window.DOCTOR_CASES = ")
        json.dump(data, f, separators=(",", ":"))
        f.write(";\n")
    print(f"{out}: {len(cases)} cases, {os.path.getsize(out)} bytes")


if __name__ == "__main__":
    main()
