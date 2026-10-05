#!/usr/bin/env python3
"""Validate generated files against the platforms' own JSON schemas.

usage: validate.py DIR [DIR...]

Each DIR is the output of `preconfig build`. The schemas live in tools/schemas;
tools/schemas/fetch.sh downloads them (the Alpha used the versions fetched on
2026-09-29): devContainer.base.schema.json (devcontainers/spec),
cursor.environment.schema.json (cursor.com), cloudinit.v1.schema.json
(canonical/cloud-init) and github-workflow.json (SchemaStore).
Exit status 1 if any file fails.
"""
import json, os, re, sys
import yaml
import jsonschema

HERE = os.path.dirname(os.path.abspath(__file__))
S = lambda name: json.load(open(os.path.join(HERE, "schemas", name)))

def strip_jsonc(text):
    # Remove // line comments (only whole-line comments are generated).
    return "\n".join(l for l in text.splitlines() if not l.lstrip().startswith("//"))

class Loader(yaml.SafeLoader):
    pass
# Keep "on" as a string key, as GitHub does (YAML 1.2), not True (YAML 1.1).
Loader.yaml_implicit_resolvers = {k: [r for r in v if r[0] != 'tag:yaml.org,2002:bool'] for k, v in yaml.SafeLoader.yaml_implicit_resolvers.items()}
Loader.add_implicit_resolver('tag:yaml.org,2002:bool', re.compile(r'^(?:true|True|TRUE|false|False|FALSE)$'), list('tTfF'))

CHECKS = [
    (".devcontainer/devcontainer.json", "devContainer.base.schema.json", lambda t: json.loads(strip_jsonc(t))),
    (".cursor/environment.json", "cursor.environment.schema.json", json.loads),
    ("cloud-init.yaml", "cloudinit.v1.schema.json", lambda t: yaml.load(t, Loader=Loader)),
    (".github/workflows/copilot-setup-steps.yml", "github-workflow.json", lambda t: yaml.load(t, Loader=Loader)),
]

def main(dirs):
    failed = 0
    checked = 0
    for d in dirs:
        for rel, schema_name, load in CHECKS:
            p = os.path.join(d, rel)
            if not os.path.exists(p):
                continue
            text = open(p).read()
            if rel == "cloud-init.yaml" and not text.startswith("#cloud-config\n"):
                print(f"FAIL {p}: the first line must be #cloud-config"); failed += 1; continue
            doc = load(text)
            schema = S(schema_name)
            # Each schema names its own draft; use that one.
            v = jsonschema.validators.validator_for(schema)(schema)
            errs = sorted(v.iter_errors(doc), key=lambda e: list(e.path))
            checked += 1
            if errs:
                failed += 1
                for e in errs[:5]:
                    print(f"FAIL {p}: {'/'.join(map(str, e.path))}: {e.message[:200]}")
            else:
                print(f"ok   {p}")
    print(f"{checked} files checked, {failed} failed")
    return 1 if failed else 0

if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
