"""Writes the corpus that tools/wasm/compare.cjs runs through both browser
builds of the engine: every sample spec, the specs from the spec tests, and the
files of every sample repository for check and detect."""
import json
import os
import sys

root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
known = [
    "preconfig.yaml", ".github/workflows/copilot-setup-steps.yml", ".github/copilot-setup-steps.yml",
    ".github/copilot-setup-steps.yaml", ".github/workflows/copilot-setup-steps.yaml", ".cursor/environment.json",
    ".cursor/Dockerfile", ".devcontainer/devcontainer.json", ".devcontainer.json", ".devcontainer/compose.yaml",
    "cloud-init.yaml", ".preconfig/setup.sh",
]
detect_paths = [
    ".nvmrc", ".node-version", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock",
    ".python-version", "pyproject.toml", "requirements.txt", "requirements-dev.txt", "uv.lock", "poetry.lock", "Pipfile",
    "go.mod", "compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml",
    ".env.example", ".env.sample", ".env.template", ".env.dist", ".tool-versions",
]
repos = {
    "orders-api": "testdata/repos/orders-api", "web-shop": "testdata/repos/web-shop",
    "ingest-worker": "testdata/repos/ingest-worker", "broken": "testdata/check/broken", "no-redis": "testdata/verify/no-redis",
}


def read(d, paths):
    out = {}
    for p in paths:
        f = os.path.join(root, d, p)
        if os.path.isfile(f):
            out[p] = open(f, encoding="utf-8").read()
    return out


specs = {name: read(d, ["preconfig.yaml"])["preconfig.yaml"] for name, d in repos.items()}
extra = {
    "node-pnpm-uv": 'version: 1\nname: mixed\nruntimes: {node: "24", python: "3.13"}\ntools: [pnpm@10, uv]\nsetup: [pnpm install --frozen-lockfile, uv sync --frozen]\nready: [pnpm test, uv run pytest -q]\n',
    "go-redis8": 'version: 1\nruntimes:\n  go: "1.25.3"\nservices:\n  redis: "8"\nready: [go test ./...]\n',
    "postgres18-script-only": 'version: 1\nservices:\n  postgres: "18"\ntargets: [script, cloud-init]\nrepo: https://github.com/acme/app.git\nready: ["pg_isready -h localhost"]\n',
    "packages-env": 'version: 1\npackages: [libpq-dev, build-essential]\nenv:\n  A: "1"\n  QUOTE: "it\'s \\"x\\""\n  URL: https://x.y/z?a=b&c=d\nsecrets: [NPM_TOKEN]\nready: ["true"]\n',
    "yarn-poetry": 'version: 1\nruntimes: {node: "20", python: "3.11"}\ntools: [yarn, poetry]\nsetup: [yarn install --immutable, poetry install --no-interaction]\nready: [yarn test]\n',
    "unknown-key": "version: 1\nruntime:\n  node: \"22\"\n",
    "bad-yaml": "version: 1\nname: [x\n",
    "empty": "",
    "no-version": "name: x\n",
    "node-odd": "version: 1\nruntimes:\n  node: \"21\"\n",
    "tool-typo": "version: 1\nruntimes: {node: \"22\"}\ntools: [pnmp]\n",
    "service-unknown": "version: 1\nservices:\n  mysql: \"8\"\n",
    "env-secret": "version: 1\nenv:\n  NPM_TOKEN: abc\n",
    "target-typo": "version: 1\ntargets: [copliot]\n",
    "no-ready": "version: 1\n",
    "redis-url-without-redis": "version: 1\nenv:\n  REDIS_URL: redis://localhost:6379/0\nready: [\"true\"]\n",
}
specs.update(extra)
# The deepest nesting the readers accept, to prove the browser build's stack.
specs["deep-flow"] = "version: 1\nx: " + "[" * 198 + "]" * 198 + "\n"
specs["deep-block"] = "version: 1\nx:\n" + "".join("  " * i + "k%d:\n" % i for i in range(1, 198)) + "  " * 198 + "v: 1\n"
specs["too-deep"] = "version: 1\nx: " + "[" * 400 + "]" * 400 + "\n"
checks = {name: read(d, known) for name, d in repos.items()}
checks["deep-json"] = {".devcontainer/devcontainer.json": '{"image": "x", "a": ' + "[" * 198 + "]" * 198 + "}"}
checks["too-deep-json"] = {".devcontainer/devcontainer.json": '{"image": "x", "a": ' + "[" * 5000 + "]" * 5000 + "}"}
cursor_only = {".cursor/environment.json": '{\n  "update": "npm ci",\n  "build": {"dockerfile": "Dockerfile",}\n}\n'}
checks["cursor-trailing-comma"] = cursor_only
checks["cloud-init-no-header"] = {"cloud-init.yaml": "packages: [git]\n"}
detects = {name: read(d, detect_paths) for name, d in repos.items()}
detects["pnpm-bun-mix"] = {"package.json": '{"name": "@acme/app", "packageManager": "yarn@1.22.22", "scripts": {"build": "tsc"}}', "bun.lock": "x", ".env.example": "API_KEY=\nPORT=3000\n"}
json.dump({"build": specs, "check": checks, "detect": detects}, open(sys.argv[1], "w"), indent=1)
print(len(specs), "specs,", len(checks), "check inputs,", len(detects), "detect inputs")
