#!/usr/bin/env bash
# Checks that every value preconfig writes into YAML reads back as the same
# string in five YAML readers. Needs python3 with PyYAML and ruamel.yaml, and
# node with the yaml and js-yaml packages (npm install yaml@2 js-yaml@4).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
python3 "$here/strings.py" "$work/in.json"
(cd "$root" && QUOTING_IN="$work/in.json" QUOTING_OUT="$work/out.json" go test ./internal/tree -run TestDumpYAMLStrings -count=1 >/dev/null)
python3 "$here/check.py" "$work/out.json"
if [ ! -d "$here/node_modules/yaml" ] || [ ! -d "$here/node_modules/js-yaml" ]; then
  (cd "$here" && npm install --no-save --no-package-lock --silent yaml@2 js-yaml@4)
fi
node "$here/check.mjs" "$work/out.json"
