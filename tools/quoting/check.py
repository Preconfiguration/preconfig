"""Reads every value preconfig would write into YAML back with PyYAML (YAML
1.1, what cloud-init uses) and ruamel.yaml (YAML 1.2), in four positions, and
reports any that don't come back as the same string."""
import json
import sys

import yaml
from ruamel.yaml import YAML

r12 = YAML(typ="safe", pure=True)
m = json.load(open(sys.argv[1]))
contexts = ("k: {}\n", "- {}\n", "k:\n  - {}\n", "a:\n  b: {}\n")


def first(d):
    while isinstance(d, (dict, list)):
        d = list(d.values())[0] if isinstance(d, dict) else d[0]
    return d


bad = {"PyYAML": [], "ruamel.yaml": []}
for s, out in m.items():
    for ctx in contexts:
        doc = ctx.format(out)
        for name, load in (("PyYAML", yaml.safe_load), ("ruamel.yaml", r12.load)):
            try:
                v = first(load(doc))
                if v != s or not isinstance(v, str):
                    bad[name].append((s, out, repr(v)))
            except Exception as e:  # noqa: BLE001
                bad[name].append((s, out, "error: " + str(e).splitlines()[0]))
plain = sum(1 for s, o in m.items() if s == o)
print(f"{len(m)} strings, {plain} written without quotes")
for name, b in bad.items():
    print(f"{name}: {len(b)} failures", b[:5])
sys.exit(1 if any(bad.values()) else 0)
