#!/usr/bin/env python3
"""Writes the corpus that tools/doctor/wasm-run.cjs runs through Doctor's
browser builds, with the command line's answers to compare them with: every
log of the tuning and holdout corpora and the spec each was recorded with, the
diagnosis preconfig doctor --json gives, and, where Doctor writes a change, the
spec and files preconfig doctor --fix writes.

    python3 tools/doctor/wasm-native.py PRECONFIG OUT.json
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
LOGS = ("log.txt", "out.txt", "build.txt", "err.txt")


def entries():
    for group in ("corpus", "holdout"):
        base = os.path.join(ROOT, "testdata", "doctor", group)
        if not os.path.isdir(base):
            continue
        for cid in sorted(os.listdir(base)):
            d = os.path.join(base, cid)
            spec = os.path.join(d, "spec.yaml")
            if not os.path.isfile(spec):
                continue
            for name in LOGS:
                if os.path.isfile(os.path.join(d, name)):
                    yield f"{group}/{cid}/{name}", os.path.join(d, name), spec
    alpha = os.path.join(ROOT, "internal", "verify", "testdata")
    spec = os.path.join(ROOT, "testdata", "verify", "no-redis", "preconfig.yaml")
    for name in ("fail.log", "pass.log"):
        yield f"alpha/{name}", os.path.join(alpha, name), spec


def main():
    pc, out = sys.argv[1:3]
    pc = os.path.abspath(pc)
    corpus = []
    for key, log, spec in entries():
        p = subprocess.run([pc, "doctor", "--json", "--spec", spec, log], capture_output=True, text=True)
        d = json.loads(p.stdout)["diagnosis"]
        # newline="" keeps the log's carriage returns, as the command line reads them.
        item = {"key": key, "log": open(log, encoding="utf-8", newline="").read(),
                "spec": open(spec, encoding="utf-8", newline="").read(),
                "native": {"diagnosis": d, "exit": p.returncode}}
        if d.get("changes"):
            tmp = tempfile.mkdtemp()
            try:
                shutil.copy(spec, os.path.join(tmp, "preconfig.yaml"))
                f = subprocess.run([pc, "doctor", "--json", "--fix", "--dir", tmp, log], capture_output=True, text=True)
                fixed = json.loads(f.stdout)
                files = {}
                for dp, _, names in os.walk(tmp):
                    for n in names:
                        rel = os.path.relpath(os.path.join(dp, n), tmp)
                        if rel != "preconfig.yaml":
                            files[rel] = open(os.path.join(dp, n), encoding="utf-8").read()
                item["native"]["fix"] = {"exit": f.returncode, "diff": fixed.get("diff", ""),
                                         "spec": open(os.path.join(tmp, "preconfig.yaml"), encoding="utf-8").read(),
                                         "files": files}
            finally:
                shutil.rmtree(tmp)
        corpus.append(item)
    json.dump(corpus, open(out, "w"))
    fixes = sum(1 for c in corpus if "fix" in c["native"])
    print(f"{out}: {len(corpus)} logs, {fixes} with a change the command line applied")


if __name__ == "__main__":
    main()
