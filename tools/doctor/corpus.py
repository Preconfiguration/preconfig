#!/usr/bin/env python3
"""Records Doctor's corpus: setup failures made on purpose on this machine.

    python3 tools/doctor/corpus.py OUT_DIR --preconfig BIN [flags]

Each case copies one of the sample repositories, changes it the way the case
says, and runs `preconfig verify` on a clean machine, or builds the repository's
Cursor Dockerfile. It keeps everything the run printed. case.json records how
the case was made and the cause that was planted, written from this file before
Doctor ever reads the log, so the cause is known independently of Doctor.

Flags:
  --preconfig BIN   the preconfig binary that runs verify and build
  --network NET     docker --network for verify and docker build (e.g. host)
  --ca-file FILE    a CA bundle for networks that inspect TLS
  --only ID,...     record only these cases
  --jobs N          cases run side by side (default 1). With --network host,
                    runs side by side share ports 5432 and 6379, so keep 1
  --list            print the cases and exit
  --specs-only      write each recorded case's spec.yaml, and record nothing
  --holdout         the holdout cases, recorded after Doctor's rules were frozen

Every case dir gets: case.json; spec.yaml (the case's preconfig.yaml); log.txt (the machine's own output, from
verify --log) and out.txt (verify's terminal output), or build.txt (docker
build's output); result.json (exit code and seconds).
"""
import argparse
import concurrent.futures
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
REPOS = os.path.join(ROOT, "testdata", "repos")
FIXTURES = os.path.join(ROOT, "tools", "doctor", "fixtures")

# Pieces of the orders-api spec that cases remove or change.
REDIS_LINE = '  redis: "7"\n'
POSTGRES_BLOCK = '  postgres:\n    version: "16"\n    user: orders\n    password: orders\n    database: orders\n'
DB_URL = 'postgres://orders:orders@localhost:5432/orders'
DB_URL_LINE = '  DATABASE_URL: ' + DB_URL + '\n'
RUNTIMES_BLOCK = 'runtimes:\n  python: "3.12"\n\n'
PIP_STEP = '  - .venv/bin/pip install -r requirements.txt\n'

# The psycopg2 build needs three packages that a clean Ubuntu doesn't have;
# each one missing gives its own error, so the chain is three cases.
PSYCOPG2 = "psycopg2==2.9.10\n"

MAGIC_TEST = '''import magic


def test_detects_plain_text():
    assert magic.from_buffer(b"hello", mime=True) == "text/plain"
'''

LOCALE_TEST = '''import locale


def test_german_number_format():
    locale.setlocale(locale.LC_ALL, "de_DE.UTF-8")
    assert locale.format_string("%.2f", 1234.5, grouping=True) == "1.234,50"
'''

CLI_TEST = '''import subprocess


def test_exports_open_in_sqlite():
    out = subprocess.run(["sqlite3", ":memory:", "select 1+1"], check=True, capture_output=True, text=True)
    assert out.stdout.strip() == "2"
'''

SECRET_TEST = '''import os


def test_payments_key_is_configured():
    assert os.environ["PAYMENTS_API_KEY"].startswith("pk_")
'''

NEWLIB_PYPROJECT = '''[build-system]
requires = ["setuptools>=68"]
build-backend = "setuptools.build_meta"

[project]
name = "newlib"
version = "1.0.0"
requires-python = ">=3.13"
'''

UV_PYPROJECT = '''[project]
name = "orders-api"
version = "0.1.0"
description = "Orders service: PostgreSQL for orders, Redis for cached counts."
requires-python = ">=3.12"
dependencies = [
    "psycopg[binary]==3.3.6",
    "redis==8.1.0",
    "pytest==9.1.1",
    "requests==2.32.5",
]

[tool.pytest.ini_options]
testpaths = ["tests"]
pythonpath = ["."]
'''


def case(id, repo, what, edits, expect, kind="verify", note="", blocker=0):
    c = {"id": id, "repo": repo, "kind": kind, "made": what, "edits": edits, "expect": expect, "blocker": blocker}
    if note:
        c["note"] = note
    return c


def spec(old, new):
    return ["replace", "preconfig.yaml", old, new]


def failed(phase, category, cause, change=None, **extra):
    e = {"outcome": "failed", "phase": phase, "category": category, "cause": cause, "change": change}
    e.update(extra)
    return e


PASSED = {"outcome": "passed"}

CASES = [
    case("pass-orders-api", "orders-api", "The sample service as it is.", [], PASSED),
    case("no-redis", "orders-api", "Redis left out of the spec's services.",
         [spec(REDIS_LINE, "")],
         failed("ready", "spec", "Redis isn't running: the tests can't connect to localhost:6379",
                {"op": "add-service", "name": "redis", "version": "7"})),
    case("no-postgres", "orders-api", "PostgreSQL left out of the spec's services; DATABASE_URL still points at it.",
         [spec(POSTGRES_BLOCK, "")],
         failed("ready", "spec", "PostgreSQL isn't running: the tests can't connect to localhost:5432",
                {"op": "add-service", "name": "postgres", "version": "16", "user": "orders", "password": "orders", "database": "orders"})),
    case("no-database-url", "orders-api", "DATABASE_URL left out of the spec's env; the code reads it.",
         [spec(DB_URL_LINE, "")],
         failed("ready", "spec", "The tests read DATABASE_URL, and the spec doesn't set it",
                {"op": "add-env", "key": "DATABASE_URL", "value": DB_URL})),
    case("typo-package", "orders-api", "A system package misspelled in the spec: libpq-devv.",
         [spec("secrets:\n", "packages:\n  - libpq-devv\n\nsecrets:\n")],
         failed("machine", "spec", "Ubuntu 24.04 has no package named libpq-devv",
                {"op": "replace-package", "old": "libpq-devv", "value": "libpq-dev"})),
    case("psycopg2-1", "orders-api", "psycopg2 built from source, on a machine with no compiler, no Python headers and no libpq-dev.",
         [["append", "requirements.txt", PSYCOPG2]],
         failed("project", "spec", "The build needs a C compiler, from build-essential",
                {"op": "add-package", "value": "build-essential"}),
         note="Designed expecting pg_config to be missing first. postgresql-common, installed with the PostgreSQL "
              "service, provides a pg_config, so the build got as far as the compiler. Label set from the log "
              "before Doctor read it."),
    case("psycopg2-2", "orders-api", "psycopg2 from source, with libpq-dev and a compiler but no Python headers.",
         [["append", "requirements.txt", PSYCOPG2], spec("secrets:\n", "packages:\n  - libpq-dev\n  - build-essential\n\nsecrets:\n")],
         failed("project", "spec", "The build needs Python.h, from python3-dev",
                {"op": "add-package", "value": "python3-dev"})),
    case("python-magic", "orders-api", "python-magic added, and a test that uses it; libmagic isn't installed.",
         [["append", "requirements.txt", "python-magic==0.4.27\n"], ["file", "tests/test_magic.py", MAGIC_TEST]],
         failed("ready", "spec", "python-magic needs the libmagic library",
                {"op": "add-package", "value": "libmagic1t64"})),
    case("locale", "orders-api", "A test that needs the de_DE.UTF-8 locale, which a clean Ubuntu doesn't have.",
         [["file", "tests/test_locale.py", LOCALE_TEST]],
         failed("ready", "spec", "The machine has no de_DE.UTF-8 locale",
                {"op": "add-package", "value": "locales-all"})),
    case("sqlite3-cli", "orders-api", "A test that runs the sqlite3 command, which isn't installed.",
         [["file", "tests/test_cli.py", CLI_TEST]],
         failed("ready", "spec", "The tests run sqlite3, which isn't installed",
                {"op": "add-package", "value": "sqlite3"})),
    case("uv-missing", "orders-api", "The setup installs with uv, and the spec doesn't list uv under tools.",
         [spec(PIP_STEP, "  - uv pip install --python .venv/bin/python -r requirements.txt\n")],
         failed("project", "spec", "The setup runs uv, and uv isn't installed",
                {"op": "add-tool", "value": "uv"})),
    case("no-python", "orders-api", "The python runtime left out of the spec.",
         [spec(RUNTIMES_BLOCK, "")],
         failed("project", "spec", "The setup runs python3, and no Python is installed",
                {"op": "set-runtime", "name": "python", "value": "3.12"})),
    case("needs-python-3.13", "orders-api", "A local package in requirements.txt that requires Python 3.13.",
         [["append", "requirements.txt", "./pkgs/newlib\n"], ["file", "pkgs/newlib/pyproject.toml", NEWLIB_PYPROJECT],
          ["file", "pkgs/newlib/newlib/__init__.py", ""]],
         failed("project", "spec", "newlib requires Python 3.13 or later; the spec installs 3.12",
                {"op": "set-runtime", "name": "python", "value": "3.13"})),
    case("bad-pin", "orders-api", "requirements.txt pins a version of requests that doesn't exist.",
         [["append", "requirements.txt", "requests==99.0.0\n"]],
         failed("project", "repository", "requirements.txt asks for requests 99.0.0, which PyPI doesn't have")),
    case("missing-requirements-file", "orders-api", "requirements.txt includes a file that isn't in the repository.",
         [["append", "requirements.txt", "-r requirements-dev.txt\n"]],
         failed("project", "repository", "requirements.txt includes requirements-dev.txt, which doesn't exist")),
    case("stale-uv-lock", "orders-api", "uv.lock written before requests was added to pyproject.toml; the setup runs uv sync --locked.",
         [["file", "pyproject.toml", UV_PYPROJECT], ["copy", "uv/uv.lock", "uv.lock"],
          spec("secrets:\n", "tools:\n  - uv\n\nsecrets:\n"),
          spec("setup:\n  - python3 -m venv .venv\n" + PIP_STEP, "setup:\n  - uv sync --locked\n"),
          spec("  - .venv/bin/pytest -q\n", "  - uv run --locked pytest -q\n")],
         failed("project", "repository", "uv.lock is out of date with pyproject.toml")),
    case("missing-secret", "orders-api", "A test that needs PAYMENTS_API_KEY, a secret the spec names, which isn't set.",
         [["file", "tests/test_payments.py", SECRET_TEST]],
         failed("ready", "secret", "PAYMENTS_API_KEY is a secret, and it isn't set where the setup runs")),
    case("code-failure", "orders-api", "A test changed to expect the wrong count: the code is fine, the test is wrong.",
         [["replace", "tests/test_orders.py", "assert orders.order_count(conn, r, c) == 1\n    assert r.get",
           "assert orders.order_count(conn, r, c) == 2\n    assert r.get"]],
         failed("ready", "code", "A test failed on its own assertion; the setup worked")),
    case("wrong-db-password", "orders-api", "DATABASE_URL has a password that doesn't match the spec's postgres service.",
         [spec(DB_URL_LINE, "  DATABASE_URL: postgres://orders:secret@localhost:5432/orders\n")],
         failed("ready", "spec", "DATABASE_URL's password doesn't match the postgres service's",
                {"op": "set-env", "key": "DATABASE_URL", "value": DB_URL})),
    case("port-5432-taken", "orders-api", "Another program already listening on port 5432 when the setup starts PostgreSQL.",
         [], failed("services", "platform", "Another program holds port 5432, so PostgreSQL can't start"), blocker=5432),
    case("python-3.13", "orders-api", "Python 3.13, which the setup installs with uv from GitHub's release downloads.",
         [spec('  python: "3.12"\n', '  python: "3.13"\n')], PASSED,
         note="Designed expecting GitHub's downloads to be out of reach, as they were for the Alpha. uv downloaded "
              "Python 3.13.15 and the setup reached READY, so the case is a passing log. Label set from the log "
              "before Doctor read it."),
    case("postgres-17-blocked", "orders-api", "PostgreSQL 17, from the PostgreSQL project's archive, which this machine can't reach.",
         [spec('    version: "16"\n', '    version: "17"\n')],
         failed("machine", "network", "The machine can't reach the PostgreSQL project's archive", host="postgresql.org")),
    case("redis-8-blocked", "orders-api", "Redis 8, from Redis's own packages, which this machine can't reach.",
         [spec(REDIS_LINE, '  redis: "8"\n')],
         failed("machine", "network", "The machine can't reach Redis's packages", host="packages.redis.io")),
    case("node-blocked", "web-shop", "The web-shop sample as it is: Node.js 22 comes from NodeSource, which this machine can't reach.", [],
         failed("machine", "network", "The machine can't reach NodeSource", host="deb.nodesource.com")),
    case("go-blocked", "ingest-worker", "The ingest-worker sample as it is: Go 1.25 is downloaded by the go command, and this machine can't reach Go's downloads.", [],
         failed("machine", "network", "The machine can't download the Go 1.25 toolchain", host="golang.org")),
    case("docker-pass", "orders-api", "Cursor's Dockerfile for the sample service as it is.", [], PASSED, kind="docker"),
    case("docker-typo-package", "orders-api", "Cursor's Dockerfile, with the system package misspelled in the spec: libpq-devv.",
         [spec("secrets:\n", "packages:\n  - libpq-devv\n\nsecrets:\n")],
         failed("machine", "spec", "Ubuntu 24.04 has no package named libpq-devv",
                {"op": "replace-package", "old": "libpq-devv", "value": "libpq-dev"}), kind="docker"),
    case("docker-node-blocked", "web-shop", "Cursor's Dockerfile for web-shop; NodeSource can't be reached.", [],
         failed("machine", "network", "The machine can't reach NodeSource", host="deb.nodesource.com"), kind="docker"),
]


# The holdout corpus: designed with the tuning corpus, and recorded only after
# Doctor's rules were frozen, so it measures the rules on logs they were never
# adjusted to.
UNZIP_TEST = '''import subprocess


def test_exports_are_zip_files():
    out = subprocess.run(["unzip", "-v"], check=True, capture_output=True, text=True)
    assert "UnZip" in out.stdout
'''

OLDLIB_PYPROJECT = NEWLIB_PYPROJECT.replace('name = "newlib"', 'name = "oldlib"').replace('">=3.13"', '"<3.12,>=3.9"')

HOLDOUT = [
    case("lxml-source", "orders-api", "lxml built from source, with a compiler in the spec but no libxml2 headers.",
         [["append", "requirements.txt", "--no-binary lxml\nlxml==5.4.0\n"],
          spec("secrets:\n", "packages:\n  - build-essential\n\nsecrets:\n")],
         failed("project", "spec", "lxml's build needs the development packages of libxml2 and libxslt: libxml2-dev and libxslt1-dev",
                [{"op": "add-package", "value": "libxml2-dev"}, {"op": "add-package", "value": "libxslt1-dev"}]),
         note="Designed expecting only libxml2's headers to be named. lxml's build asks for the development "
              "packages of both libxml2 and libxslt, so the right fix adds both. Label set from the log."),
    case("unzip-cli", "orders-api", "A test that runs unzip, which isn't installed.",
         [["file", "tests/test_unzip.py", UNZIP_TEST]],
         failed("ready", "spec", "The tests run unzip, which isn't installed",
                {"op": "add-package", "value": "unzip"})),
    case("pillow-source", "orders-api", "Pillow built from source, with a compiler and Python headers in the spec but no image libraries.",
         [["append", "requirements.txt", "--no-binary pillow\npillow==11.3.0\n"],
          spec("secrets:\n", "packages:\n  - build-essential\n  - python3-dev\n\nsecrets:\n")],
         failed("project", "spec", "Pillow's build needs libjpeg's headers, from libjpeg-dev",
                {"op": "add-package", "value": ["libjpeg-dev", "libjpeg8-dev", "libjpeg-turbo8-dev"]}),
         note="Designed expecting zlib's headers to be missing first. python3-dev brings zlib1g-dev with it, so "
              "Pillow's build stopped on libjpeg instead; any of Ubuntu's three names for its headers is right. "
              "Label set from the log."),
    case("missing-redis-url", "orders-api", "REDIS_URL left out of the spec's env; the code reads it.",
         [spec("  REDIS_URL: redis://localhost:6379/0\n", "")],
         failed("ready", "spec", "The tests read REDIS_URL, and the spec doesn't set it",
                {"op": "add-env", "key": "REDIS_URL", "value": "redis://localhost:6379/0"})),
    case("make-missing", "orders-api", "The ready check runs make test, and make isn't installed.",
         [["file", "Makefile", "test:\n\t.venv/bin/pytest -q\n"], spec("  - .venv/bin/pytest -q\n", "  - make test\n")],
         failed("ready", "spec", "The ready check runs make, which isn't installed",
                {"op": "add-package", "value": "make"})),
    case("private-index", "orders-api", "requirements.txt installs from a company index this machine can't reach.",
         [["replace", "requirements.txt", "psycopg[binary]", "--index-url https://pypi.internal.example/simple\npsycopg[binary]"]],
         failed("project", "network", "The machine can't reach the package index pypi.internal.example", host="pypi.internal.example")),
    case("github-requirement", "orders-api", "requirements.txt installs a package straight from GitHub, which this machine can't reach.",
         [["append", "requirements.txt", "requests @ git+https://github.com/psf/requests@v2.32.5\n"]],
         PASSED,
         note="Designed expecting github.com to be out of reach. pip cloned the repository from GitHub and the "
              "setup reached READY, so the case is a passing log. Label set from the log."),
    case("db-port-mismatch", "orders-api", "DATABASE_URL points at port 5433; the spec's PostgreSQL listens on 5432.",
         [spec("localhost:5432/orders", "localhost:5433/orders")],
         failed("ready", "spec", "DATABASE_URL points at the wrong port",
                {"op": "set-env", "key": "DATABASE_URL", "value": DB_URL})),
    case("wrong-db-name", "orders-api", "DATABASE_URL names a database the spec's service doesn't create.",
         [spec("localhost:5432/orders", "localhost:5432/shop")],
         failed("ready", "spec", "DATABASE_URL names the database shop; the service creates orders",
                {"op": "set-env", "key": "DATABASE_URL", "value": DB_URL})),
    case("spec-error", "orders-api", "A tool misspelled in the spec: pnmp. verify refuses to start.",
         [spec("secrets:\n", "tools:\n  - pnmp\n\nsecrets:\n")],
         failed("", "spec", "preconfig.yaml has errors")),
    case("needs-python-below-3.12", "orders-api", "A local package in requirements.txt that needs Python older than 3.12.",
         [["append", "requirements.txt", "./pkgs/oldlib\n"], ["file", "pkgs/oldlib/pyproject.toml", OLDLIB_PYPROJECT],
          ["file", "pkgs/oldlib/oldlib/__init__.py", ""]],
         failed("project", "spec", "oldlib needs Python below 3.12; the spec installs 3.12",
                {"op": "set-runtime", "name": "python", "value": "3.10"})),
]


def make_variant(c, dest):
    shutil.copytree(os.path.join(REPOS, c["repo"]), dest, symlinks=True)
    for e in c["edits"]:
        op = e[0]
        if op == "replace":
            p = os.path.join(dest, e[1])
            s = open(p).read()
            if e[2] not in s:
                raise SystemExit(f"{c['id']}: {e[1]} doesn't contain {e[2]!r}")
            open(p, "w").write(s.replace(e[2], e[3], 1))
        elif op == "append":
            with open(os.path.join(dest, e[1]), "a") as f:
                f.write(e[2])
        elif op == "file":
            p = os.path.join(dest, e[1])
            os.makedirs(os.path.dirname(p), exist_ok=True)
            open(p, "w").write(e[2])
        elif op == "copy":
            shutil.copy(os.path.join(FIXTURES, e[1]), os.path.join(dest, e[2]))
        else:
            raise SystemExit("unknown edit " + op)
    # Generated files that the sample carries are rebuilt from the changed spec,
    # so the setup script and Cursor's Dockerfile match it.
    return dest


def run(cmd, out, err=None, timeout=3600):
    t = time.time()
    with open(out, "w") as fo:
        fe = open(err, "w") if err else subprocess.STDOUT
        try:
            p = subprocess.run(cmd, stdout=fo, stderr=fe, timeout=timeout)
            code = p.returncode
        except subprocess.TimeoutExpired:
            code = -1
        finally:
            if err:
                fe.close()
    return code, round(time.time() - t, 1)


def record(c, args, out_root):
    d = os.path.join(out_root, c["id"])
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(d)
    meta = {k: c[k] for k in ("id", "repo", "kind", "made", "expect", "note") if k in c}
    meta["edits"] = [e[:2] + ([f"{len(e[2])} characters"] if e[0] in ("file",) else e[2:]) for e in c["edits"]]
    with tempfile.TemporaryDirectory(prefix="doctor-corpus-") as tmp:
        v = make_variant(c, os.path.join(tmp, c["repo"]))
        shutil.copy(os.path.join(v, "preconfig.yaml"), os.path.join(d, "spec.yaml"))
        code, _ = run([args.preconfig, "build", "--dir", v], os.path.join(tmp, "build.out"))
        if code != 0 and c["expect"].get("phase") != "":
            # Only a case planted in the spec itself may fail to build; verify
            # then refuses to start, and what it prints is the log.
            print(open(os.path.join(tmp, "build.out")).read())
            raise SystemExit(f"{c['id']}: preconfig build failed with {code}")
        blocker = None
        if c.get("blocker"):
            # A program on this machine that holds the port first. verify runs
            # with the host's network here, so the clean machine sees it too.
            # It holds the port on the loopback addresses, as another database
            # would, and closes every connection at once, so nothing waits on it.
            blocker = subprocess.Popen([sys.executable, "-c",
                "import socket, threading\n"
                "def hold(family, addr):\n"
                "    try:\n"
                "        s = socket.socket(family)\n"
                "    except OSError:\n"
                "        return  # no IPv6 on this machine\n"
                "    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)\n"
                f"    s.bind((addr, {c['blocker']})); s.listen()\n"
                "    while True:\n"
                "        conn, _ = s.accept(); conn.close()\n"
                "threading.Thread(target=hold, args=(socket.AF_INET6, '::1'), daemon=True).start()\n"
                "hold(socket.AF_INET, '127.0.0.1')\n"])
            time.sleep(1)
        if c["kind"] == "verify":
            cmd = [args.preconfig, "verify", "--dir", v, "--log", os.path.join(d, "log.txt")]
            if args.network:
                cmd += ["--network", args.network]
            if args.ca_file:
                cmd += ["--ca-file", args.ca_file]
            code, secs = run(cmd, os.path.join(d, "out.txt"), os.path.join(d, "err.txt"))
            if blocker:
                blocker.kill()
        else:
            tag = "doctor-corpus-" + c["id"]
            cmd = ["docker", "build", "--no-cache", "--progress=plain", "-t", tag, "-f", os.path.join(v, ".cursor", "Dockerfile")]
            if args.network:
                cmd += ["--network", args.network]
            cmd.append(v)
            code, secs = run(cmd, os.path.join(d, "build.txt"))
            subprocess.run(["docker", "rmi", "-f", tag], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    meta["recorded"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    json.dump(meta, open(os.path.join(d, "case.json"), "w"), indent=2)
    json.dump({"exit_code": code, "seconds": secs}, open(os.path.join(d, "result.json"), "w"), indent=2)
    return c["id"], code, secs


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("out")
    ap.add_argument("--preconfig", default="preconfig")
    ap.add_argument("--network", default="")
    ap.add_argument("--ca-file", default="")
    ap.add_argument("--only", default="")
    ap.add_argument("--jobs", type=int, default=1)
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--specs-only", action="store_true")
    ap.add_argument("--holdout", action="store_true", help="the holdout cases instead of the tuning corpus")
    args = ap.parse_args()
    cases = HOLDOUT if args.holdout else CASES
    if args.only:
        want = set(args.only.split(","))
        cases = [c for c in cases if c["id"] in want]
    if args.list:
        for c in cases:
            print(f"{c['id']:28} {c['kind']:7} {c['made']}")
        return
    if args.specs_only:
        with tempfile.TemporaryDirectory(prefix="doctor-corpus-") as tmp:
            for c in cases:
                d = os.path.join(args.out, c["id"])
                if os.path.isdir(d):
                    v = make_variant(c, os.path.join(tmp, c["id"]))
                    shutil.copy(os.path.join(v, "preconfig.yaml"), os.path.join(d, "spec.yaml"))
        return
    os.makedirs(args.out, exist_ok=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.jobs) as ex:
        for id, code, secs in ex.map(lambda c: record(c, args, args.out), cases):
            print(f"{id:28} exit {code:4}  {secs:6.1f} s", flush=True)


if __name__ == "__main__":
    main()
