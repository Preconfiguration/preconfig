#!/usr/bin/env bash
# Builds the engine for the browser with TinyGo and with Go, runs both on the
# same corpus under Node.js, and checks that they give the same answers, and
# the same files as the command-line engine.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=${WORK:-$(mktemp -d)}
cd "$root"
tinygo build -o "$work/tiny.wasm" -target wasm -no-debug -opt=z -stack-size="${STACK:-512KB}" ./cmd/wasm
GOOS=js GOARCH=wasm go build -trimpath -o "$work/std.wasm" ./cmd/wasm
python3 "$here/corpus.py" "$work/corpus.json"
node "$here/run-engine.cjs" "$work/tiny.wasm" "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" "$work/corpus.json" "$work/tiny.json"
node "$here/run-engine.cjs" "$work/std.wasm" "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$work/corpus.json" "$work/std.json"
for f in tiny std; do
  echo "$f.wasm: $(wc -c < "$work/$f.wasm") bytes, $(gzip -9 -c "$work/$f.wasm" | wc -c) gzipped"
done
node "$here/compare.cjs" "$work/tiny.json" "$work/std.json" "$root"
