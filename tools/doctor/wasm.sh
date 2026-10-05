#!/usr/bin/env bash
# Builds the engine for the browser with TinyGo and with Go, runs Doctor in
# both under Node.js on every corpus log, and checks that each diagnosis, each
# fixed spec and each rebuilt file is the same as the command line's.
#
#   bash tools/doctor/wasm.sh
#
# Needs Go 1.24, TinyGo 0.39 and Node.js. WORK=DIR keeps the builds and answers.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=${WORK:-$(mktemp -d)}
mkdir -p "$work"
cd "$root"
go build -o "$work/preconfig" ./cmd/preconfig
tinygo build -o "$work/tiny.wasm" -target wasm -no-debug -opt=z -stack-size="${STACK:-512KB}" ./cmd/wasm
GOOS=js GOARCH=wasm go build -trimpath -o "$work/std.wasm" ./cmd/wasm
for f in tiny std; do
  echo "$f.wasm: $(wc -c < "$work/$f.wasm") bytes, $(gzip -9 -c "$work/$f.wasm" | wc -c) gzipped"
done
python3 "$here/wasm-native.py" "$work/preconfig" "$work/corpus.json"
status=0
echo "TinyGo build:"
node "$here/wasm-run.cjs" "$work/tiny.wasm" "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" "$work/corpus.json" "$work/tiny.json" || status=1
echo "Go build:"
node "$here/wasm-run.cjs" "$work/std.wasm" "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$work/corpus.json" "$work/std.json" || status=1
exit $status
