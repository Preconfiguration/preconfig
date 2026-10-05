#!/usr/bin/env python3
"""Records what the command-line engine prints on the sample repositories, for
the demo page, and writes recordings.v1.js.

It builds preconfig, runs detect, build and check on a copy of the orders-api
sample, check on the broken sample, and the change of step 6 (PostgreSQL 16 to
17), and captures every command's output and exit code. The verify runs come
from testdata/verify: they were recorded on a clean Ubuntu 24.04 container
with tools/demo/record-verify.sh, and are rendered here with the command's own
text format (tools/demo/events).

    python3 tools/demo/record.py OUT.js
"""
import datetime
import json
import os
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
GENERATED = [
    ".devcontainer/devcontainer.json", ".devcontainer/compose.yaml", ".github/workflows/copilot-setup-steps.yml",
    ".cursor/environment.json", ".cursor/Dockerfile", "cloud-init.yaml", ".preconfig/setup.sh",
]


def sh(args, cwd=None, env=None, check=True):
    p = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True)
    if check and p.returncode != 0:
        sys.exit(f"{' '.join(args)} failed: {p.stderr}")
    return p


def copy_repo(src, dst, skip=()):
    for base, dirs, files in os.walk(src):
        rel = os.path.relpath(base, src)
        for f in files:
            r = os.path.normpath(os.path.join(rel, f)).replace(os.sep, "/")
            if r in skip:
                continue
            os.makedirs(os.path.join(dst, rel), exist_ok=True)
            shutil.copy2(os.path.join(base, f), os.path.join(dst, r))


def read_tree(d, only=None):
    out = {}
    for base, _, files in os.walk(d):
        for f in files:
            p = os.path.join(base, f)
            r = os.path.relpath(p, d).replace(os.sep, "/")
            if only is not None and r not in only:
                continue
            out[r] = open(p, encoding="utf-8").read()
    return dict(sorted(out.items()))


class Recorder:
    def __init__(self, binary):
        self.bin = binary

    def run(self, cwd, *args, shown=None, env=None):
        p = subprocess.run([self.bin, *args], cwd=cwd, capture_output=True, text=True, env=env)
        return {"cmd": shown or "preconfig " + " ".join(args), "out": p.stdout, "err": p.stderr, "code": p.returncode}


def main(out_path):
    work = tempfile.mkdtemp(prefix="preconfig-demo-")
    try:
        binary = os.path.join(work, "preconfig")
        sh(["go", "build", "-trimpath", "-o", binary, "./cmd/preconfig"], cwd=ROOT)
        rec = Recorder(binary)
        version = sh([binary, "version"]).stdout.strip()

        # Steps 1 and 2: detect, then build, on orders-api without its spec.
        orders = os.path.join(work, "orders-api")
        copy_repo(os.path.join(ROOT, "testdata/repos/orders-api"), orders, skip=set(GENERATED) | {"preconfig.yaml"})
        repo_files = read_tree(orders)
        detect = rec.run(orders, "detect")
        detect_json = json.loads(rec.run(orders, "detect", "--json")["out"])
        write = rec.run(orders, "detect", "--write")
        build = rec.run(orders, "build")
        check_clean = rec.run(orders, "check")
        built = read_tree(orders, only=set(GENERATED))
        spec_text = open(os.path.join(orders, "preconfig.yaml"), encoding="utf-8").read()
        team_spec = open(os.path.join(ROOT, "testdata/repos/orders-api/preconfig.yaml"), encoding="utf-8").read()

        # Step 3: check on hand-written setup files.
        broken = os.path.join(work, "broken")
        copy_repo(os.path.join(ROOT, "testdata/check/broken"), broken)
        broken_files = read_tree(broken, only={
            "preconfig.yaml", ".github/workflows/copilot-setup-steps.yml", ".cursor/environment.json", ".devcontainer/devcontainer.json"})
        check_broken = rec.run(broken, "check")
        check_broken_json = json.loads(rec.run(broken, "check", "--json")["out"])

        # Step 6: PostgreSQL 16 to 17, in one line.
        before = spec_text
        sed = ["sed", "-i", 's/version: "16"/version: "17"/', "preconfig.yaml"]
        sh(sed, cwd=orders)
        after = open(os.path.join(orders, "preconfig.yaml"), encoding="utf-8").read()
        edit = {"cmd": "sed -i 's/version: \"16\"/version: \"17\"/' preconfig.yaml", "out": "", "err": "", "code": 0}
        check_old = rec.run(orders, "check")
        check_old_json = json.loads(rec.run(orders, "check", "--json")["out"])
        rebuild = rec.run(orders, "build")
        check_new = rec.run(orders, "check")
        rebuilt = read_tree(orders, only=set(GENERATED))

        # Steps 4 and 5: the verify runs recorded on a clean machine.
        def events(name):
            p = subprocess.run(["go", "run", "./tools/demo/events"], cwd=ROOT, input=open(os.path.join(ROOT, "testdata/verify", name)).read(),
                               capture_output=True, text=True, check=True)
            return json.loads(p.stdout)

        def machine_log(name):
            return open(os.path.join(ROOT, "internal/verify/testdata", name), encoding="utf-8").read().splitlines()

        # The lines the command prints after a failed run come from the same
        # recorded output, replayed through verify with a stand-in for docker.
        fake_bin = os.path.join(work, "fakebin")
        os.makedirs(fake_bin)
        shutil.copy2(os.path.join(ROOT, "internal/verify/testdata/fake-docker"), os.path.join(fake_bin, "docker"))
        no_redis = os.path.join(work, "no-redis")
        copy_repo(os.path.join(ROOT, "testdata/verify/no-redis"), no_redis)
        env = dict(os.environ, PATH=fake_bin + os.pathsep + os.environ["PATH"], FAKE_DIR=work, FAKE_EXIT="1",
                   FAKE_OUTPUT=os.path.join(ROOT, "internal/verify/testdata/fail.log"), FAKE_SLEEP="", FAKE_DOCKER_DOWN="")
        replay = rec.run(no_redis, "verify", env=env)
        tail = replay["out"][replay["out"].index("               the last lines it printed:"):]
        no_redis_spec = open(os.path.join(no_redis, "preconfig.yaml"), encoding="utf-8").read()

        samples = {
            "web-shop": open(os.path.join(ROOT, "testdata/repos/web-shop/preconfig.yaml"), encoding="utf-8").read(),
            "ingest-worker": open(os.path.join(ROOT, "testdata/repos/ingest-worker/preconfig.yaml"), encoding="utf-8").read(),
            "mixed-node-python": open(os.path.join(ROOT, "testdata/specs/mixed-node-python.yaml"), encoding="utf-8").read(),
        }
        recorded = datetime.datetime.fromtimestamp(os.path.getmtime(os.path.join(ROOT, "testdata/verify/pass-orders-api.jsonl")), datetime.timezone.utc)
        data = {
            "version": version,
            "samples": samples,
            "recordedAt": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d"),
            "verifyRecordedAt": recorded.strftime("%Y-%m-%d"),
            "repo": {"name": "orders-api", "files": repo_files},
            "detect": {"run": detect, "json": detect_json, "write": write, "teamSpec": team_spec},
            "build": {"spec": spec_text, "run": build, "check": check_clean, "files": built},
            "check": {"files": broken_files, "run": check_broken, "report": check_broken_json},
            "change": {
                "before": before, "after": after, "edit": edit, "check": check_old, "report": check_old_json,
                "build": rebuild, "checkAfter": check_new, "files": rebuilt,
            },
            "verify": {
                "pass": {"cmd": "preconfig verify --network host --ca-file proxy-ca.crt", "err": "", "events": events("pass-orders-api.jsonl"),
                         "log": machine_log("pass.log"), "code": 0, "spec": spec_text},
                "fail": {"cmd": "preconfig verify --network host --ca-file proxy-ca.crt", "err": replay["err"], "events": events("fail-no-redis.jsonl"),
                         "log": machine_log("fail.log"), "tail": tail, "code": 1, "spec": no_redis_spec},
            },
        }
        with open(out_path, "w", encoding="utf-8") as f:
            f.write("/* Preconfiguration.com live demo: what the command-line engine printed on the sample\n")
            f.write(" * repositories, recorded by tools/demo/record.py, and two verify runs recorded on a\n")
            f.write(" * clean Ubuntu 24.04 container. */\n")
            f.write("window.PRECONFIG_DEMO = ")
            json.dump(data, f, ensure_ascii=False, separators=(",", ":"))
            f.write(";\n")
        print(f"wrote {out_path}: {os.path.getsize(out_path)} bytes")
        print(f"detect exit {detect['code']}, build exit {build['code']}, check {check_clean['code']}, broken check {check_broken['code']}, "
              f"change check {check_old['code']}, rebuild {rebuild['code']}, final check {check_new['code']}")
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    main(sys.argv[1])
