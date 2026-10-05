#!/usr/bin/env python3
"""Proves Doctor's fixes: for every case where Doctor writes a change into the
spec, it rebuilds the case's repository, applies the change with
`preconfig doctor --fix`, and runs the setup again on a clean machine. When the
new run fails on something else, Doctor reads that log too, and the loop goes
on, up to four rounds, until the machine is READY or Doctor has no change to
make.

    python3 tools/doctor/prove.py CORPUS_DIR OUT_DIR --preconfig BIN [--network NET] [--ca-file FILE] [--only ID,...]

A fix counts as proven when the failure it named is gone: the next run is READY,
gets past the step that failed, or stops in that step on another cause. Each round keeps its log, the diagnosis
and the diff of preconfig.yaml, and OUT_DIR/proofs.json has the summary.
Runs go one at a time: with --network host, two setups at once share ports.
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import corpus  # noqa: E402

ORDER = ["machine", "services", "project", "ready"]


def step_rank(step):
    """How far a run got: the phase and the step number within it."""
    if not step:
        return (len(ORDER), 0)
    phase = step.split(" ")[0]
    try:
        n = int(step.split(" ")[1].split("/")[0])
    except (IndexError, ValueError):
        n = 0
    return (ORDER.index(phase) if phase in ORDER else -1, n)


def doctor(binary, log, variant, fix=False):
    cmd = [binary, "doctor", "--json", "--dir", variant]
    if fix:
        cmd.append("--fix")
    cmd.append(log)
    p = subprocess.run(cmd, capture_output=True, text=True)
    out = json.loads(p.stdout) if p.stdout.strip() else {}
    return out, p.returncode, p.stderr


def verify(args, variant, out_dir):
    cmd = [args.preconfig, "verify", "--dir", variant, "--log", os.path.join(out_dir, "log.txt")]
    if args.network:
        cmd += ["--network", args.network]
    if args.ca_file:
        cmd += ["--ca-file", args.ca_file]
    t = time.time()
    with open(os.path.join(out_dir, "out.txt"), "w") as fo:
        p = subprocess.run(cmd, stdout=fo, stderr=subprocess.DEVNULL)
    return p.returncode, round(time.time() - t, 1)


def docker_build(args, variant, out_dir, tag):
    cmd = ["docker", "build", "--no-cache", "--progress=plain", "-t", tag, "-f", os.path.join(variant, ".cursor", "Dockerfile")]
    if args.network:
        cmd += ["--network", args.network]
    cmd.append(variant)
    t = time.time()
    with open(os.path.join(out_dir, "build.txt"), "w") as fo:
        p = subprocess.run(cmd, stdout=fo, stderr=subprocess.STDOUT)
    subprocess.run(["docker", "rmi", "-f", tag], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return p.returncode, round(time.time() - t, 1)


def failed_step(diag):
    return diag.get("step", "")


def prove(case, cdef, args, tmp):
    kind = case["kind"]
    first_log = os.path.join(args.corpus, case["id"], "build.txt" if kind == "docker" else "log.txt")
    variant = corpus.make_variant(cdef, os.path.join(tmp, case["id"]))
    subprocess.run([args.preconfig, "build", "--dir", variant], stdout=subprocess.DEVNULL)
    rounds = []
    log = first_log
    for n in range(1, 5):
        rdir = os.path.join(args.out, case["id"], f"round-{n}")
        os.makedirs(rdir, exist_ok=True)
        before = open(os.path.join(variant, "preconfig.yaml")).read()
        res, code, err = doctor(args.preconfig, log, variant, fix=True)
        diag = res.get("diagnosis", {})
        json.dump(res, open(os.path.join(rdir, "diagnosis.json"), "w"), indent=1)
        if not res.get("fixed"):
            rounds.append({"round": n, "step": failed_step(diag), "category": diag.get("category"),
                           "rule": diag.get("rule"), "changes": diag.get("changes", []), "fixed": False,
                           "note": err.strip()})
            break
        open(os.path.join(rdir, "spec.diff"), "w").write(res.get("diff", ""))
        if kind == "docker":
            rc, secs = docker_build(args, variant, rdir, "doctor-prove-" + case["id"])
            new_log = os.path.join(rdir, "build.txt")
        else:
            rc, secs = verify(args, variant, rdir)
            new_log = os.path.join(rdir, "log.txt")
        nd, _, _ = doctor(args.preconfig, new_log, variant)
        nd = nd.get("diagnosis", {})
        # The failure is gone when the run is READY, gets further, or stops in
        # the same step on another cause: another rule, or the same rule with
        # another fix (a build that needed Python's headers now needs libpq's).
        gone = nd.get("outcome") == "passed" or step_rank(nd.get("step")) > step_rank(failed_step(diag)) or \
            (nd.get("step") == failed_step(diag) and (nd.get("rule") != diag.get("rule") or
                                                      (nd.get("changes") or []) != (diag.get("changes") or [])))
        rounds.append({"round": n, "step": failed_step(diag), "category": diag.get("category"), "rule": diag.get("rule"),
                       "changes": diag.get("changes", []), "fixed": True, "exit": rc, "seconds": secs,
                       "next": nd.get("outcome") if nd.get("outcome") == "passed" else f"{nd.get('step')}: {nd.get('rule') or nd.get('category')}",
                       "failure_gone": gone})
        print(f"  round {n}: {', '.join(c.get('op') + ' ' + (c.get('value') or c.get('key') or '') for c in diag.get('changes', []))}"
              f" -> exit {rc} in {secs} s; {'READY' if nd.get('outcome') == 'passed' else 'next: ' + str(nd.get('step')) + ' ' + str(nd.get('rule') or nd.get('category'))}", flush=True)
        if nd.get("outcome") == "passed" or not gone:
            break
        log = new_log
    final = rounds[-1] if rounds else {}
    return {"id": case["id"], "rounds": rounds,
            "ready": any(r.get("next") == "passed" for r in rounds),
            "first_fix_proven": bool(rounds) and rounds[0].get("fixed") and rounds[0].get("failure_gone", False)}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("corpus")
    ap.add_argument("out")
    ap.add_argument("--preconfig", required=True)
    ap.add_argument("--network", default="")
    ap.add_argument("--ca-file", default="")
    ap.add_argument("--only", default="")
    args = ap.parse_args()
    defs = {c["id"]: c for c in corpus.CASES + getattr(corpus, "HOLDOUT", [])}
    os.makedirs(args.out, exist_ok=True)
    summary = []
    with tempfile.TemporaryDirectory(prefix="doctor-prove-") as tmp:
        for name in sorted(os.listdir(args.corpus)):
            p = os.path.join(args.corpus, name, "case.json")
            if not os.path.exists(p):
                continue
            case = json.load(open(p))
            if args.only and name not in args.only.split(","):
                continue
            log = os.path.join(args.corpus, name, "build.txt" if case["kind"] == "docker" else "log.txt")
            p = subprocess.run([args.preconfig, "doctor", "--json", "--spec", os.path.join(args.corpus, name, "spec.yaml"), log],
                               capture_output=True, text=True)
            first = json.loads(p.stdout) if p.stdout.strip() else {}
            if not first.get("diagnosis", {}).get("changes"):
                continue
            if case["expect"].get("proof") == "unreachable":
                summary.append({"id": name, "rounds": [], "ready": False, "first_fix_proven": False,
                                "skipped": "the fix needs a source this machine can't reach"})
                print(f"{name}: skipped, the fix needs a source this machine can't reach", flush=True)
                continue
            print(f"{name}:", flush=True)
            summary.append(prove(case, defs[name], args, tmp))
    out = os.path.join(args.out, "proofs.json")
    if args.only and os.path.exists(out):
        # A run of a few cases keeps the others' results.
        mine = {s["id"] for s in summary}
        summary = sorted([s for s in json.load(open(out)) if s["id"] not in mine] + summary, key=lambda s: s["id"])
    json.dump(summary, open(out, "w"), indent=1)
    proven = sum(1 for s in summary if s["first_fix_proven"])
    ready = sum(1 for s in summary if s["ready"])
    print(f"\n{len(summary)} cases with a fix: {proven} first fixes proven, {ready} reached READY")


if __name__ == "__main__":
    main()
