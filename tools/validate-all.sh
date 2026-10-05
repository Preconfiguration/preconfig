#!/usr/bin/env bash
# Builds every sample spec and checks each generated file with the platforms'
# own tools: the JSON schemas (tools/validate.py), actionlint for the Copilot
# workflow, shellcheck for the setup script, docker compose for the compose
# file, and the dev container CLI for devcontainer.json. Each tool is skipped
# with a note when it isn't installed.
set -uo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$root"
go build -o "$work/preconfig" ./cmd/preconfig || exit 1
dirs=()
for r in testdata/repos/*/; do
  name=$(basename "$r")
  mkdir -p "$work/out/$name" && cp "$r/preconfig.yaml" "$work/out/$name/"
  dirs+=("$work/out/$name")
done
for s in testdata/specs/*.yaml; do
  name=$(basename "$s" .yaml)
  mkdir -p "$work/out/$name" && cp "$s" "$work/out/$name/preconfig.yaml"
  dirs+=("$work/out/$name")
done
fail=0
for d in "${dirs[@]}"; do
  "$work/preconfig" build --dir "$d" > /dev/null || { echo "FAIL build $d"; fail=1; }
done
echo "== JSON schemas"
python3 tools/validate.py "${dirs[@]}" | tail -1 || fail=1
python3 tools/validate.py "${dirs[@]}" > "$work/schemas.txt" || { grep FAIL "$work/schemas.txt"; fail=1; }
count() { echo "$1: $2 files, $3 failed"; }
run() { # tool label files...
  local tool=$1 label=$2; shift 2
  if ! command -v "$tool" > /dev/null; then echo "$label: skipped, $tool is not installed"; return; fi
  local n=0 bad=0
  for f in "$@"; do
    [ -f "$f" ] || continue
    n=$((n + 1))
    if ! out=$(check_one "$tool" "$f" 2>&1); then bad=$((bad + 1)); echo "FAIL $f"; echo "$out" | head -20; fi
  done
  count "$label" "$n" "$bad"
  [ "$bad" = 0 ] || fail=1
}
check_one() {
  case "$1" in
    actionlint) actionlint -shellcheck= "$2" ;;
    shellcheck) shellcheck -S style "$2" ;;
    docker) (cd "$(dirname "$2")" && docker compose -f "$(basename "$2")" config -q) ;;
    devcontainer) devcontainer read-configuration --workspace-folder "$(dirname "$(dirname "$2")")" > /dev/null ;;
  esac
}
echo "== platform tools"
run actionlint "actionlint (Copilot workflow)" $(for d in "${dirs[@]}"; do echo "$d/.github/workflows/copilot-setup-steps.yml"; done)
run shellcheck "shellcheck (setup script)" $(for d in "${dirs[@]}"; do echo "$d/.preconfig/setup.sh"; done)
run docker "docker compose config (dev container services)" $(for d in "${dirs[@]}"; do echo "$d/.devcontainer/compose.yaml"; done)
run devcontainer "dev container CLI read-configuration" $(for d in "${dirs[@]}"; do echo "$d/.devcontainer/devcontainer.json"; done)
echo "== cloud-init's own schema check"
if command -v cloud-init > /dev/null; then
  n=0; bad=0
  for d in "${dirs[@]}"; do
    n=$((n + 1))
    cloud-init schema -c "$d/cloud-init.yaml" > /dev/null || { echo "FAIL $d/cloud-init.yaml"; bad=$((bad + 1)); fail=1; }
  done
  echo "cloud-init schema -c: $n files, $bad failed"
else
  echo "skipped, cloud-init is not installed (the JSON schema above covers it)"
fi
exit $fail
