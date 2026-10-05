#!/usr/bin/env bash
# Builds preconfig for the five platforms, and the browser engine for the demo.
#
#   bash tools/build.sh OUT_DIR
#
# Needs Go 1.24. The browser engine also needs TinyGo 0.39; copy the
# engine.v2.js it writes to the demo folders to update the demos.
# -buildvcs=false keeps git's state out of the binaries, so the same source
# gives the same bytes in a git checkout or outside one.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
out=${1:?usage: build.sh OUT_DIR}
mkdir -p "$out" && out=$(cd "$out" && pwd)
cd "$root"
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os=${t%/*}
  arch=${t#*/}
  ext=""
  [ "$os" = windows ] && ext=.exe
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$out/preconfig-$os-$arch$ext" ./cmd/preconfig
  echo "preconfig-$os-$arch$ext: $(wc -c < "$out/preconfig-$os-$arch$ext") bytes"
done
if command -v tinygo > /dev/null; then
  tinygo build -o "$out/engine.wasm" -target wasm -no-debug -opt=z -stack-size=512KB ./cmd/wasm
  echo "engine.wasm: $(wc -c < "$out/engine.wasm") bytes"
  version=$(go run ./cmd/preconfig version | awk '{print $2}')
  python3 tools/demo/engine.py "$out/engine.wasm" "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" "$out/engine.v2.js" "$version"
else
  echo "TinyGo isn't installed, so the browser engine wasn't built."
fi
