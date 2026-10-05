#!/usr/bin/env bash
# Downloads the platforms' own JSON schemas that tools/validate.py checks the
# generated files against. They belong to their publishers and are not
# shipped with the prototype. The Alpha used the versions fetched on
# 2026-09-29; a newer download may differ.
set -euo pipefail
cd "$(dirname "$0")"
get() {
  echo "fetching $1"
  curl -fsSL --max-time 60 -o "$1.tmp" "$2"
  mv "$1.tmp" "$1"
}
get devContainer.base.schema.json https://raw.githubusercontent.com/devcontainers/spec/main/schemas/devContainer.base.schema.json
get cursor.environment.schema.json https://cursor.com/schemas/environment.schema.json
get cloudinit.v1.schema.json https://raw.githubusercontent.com/canonical/cloud-init/main/cloudinit/config/schemas/schema-cloud-config-v1.json
get github-workflow.json https://json.schemastore.org/github-workflow.json
