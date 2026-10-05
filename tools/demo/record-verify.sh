#!/usr/bin/env bash
# Records the clean-machine runs that the demo replays: preconfig verify on the
# orders-api sample, and on the same repository with Redis left out of the spec
# (testdata/verify/no-redis), each as JSON Lines events, the machine's own output
# and verify's standard error. Needs Docker and the ubuntu:24.04 image.
#
#   bash tools/demo/record-verify.sh OUT_DIR [verify flags ...]
#
# RUNS sets how many times each kind runs (3 by default). The Alpha's test
# machine reached the internet through a proxy that inspects TLS, so its runs
# used --network host --ca-file <the proxy's CA bundle>.
#
# To replay new runs in the demo, copy the first run of each kind into place,
# then run tools/demo/record.py:
#   OUT_DIR/pass-1.jsonl -> testdata/verify/pass-orders-api.jsonl
#   OUT_DIR/fail-1.jsonl -> testdata/verify/fail-no-redis.jsonl
#   OUT_DIR/pass-1.log   -> internal/verify/testdata/pass.log
#   OUT_DIR/fail-1.log   -> internal/verify/testdata/fail.log
set -uo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd) || exit 1
out=${1:?usage: record-verify.sh OUT_DIR [verify flags ...]}
shift
mkdir -p "$out" || exit 1
out=$(cd "$out" && pwd) || exit 1
cd "$root" || exit 1
go build -o "$out/preconfig" ./cmd/preconfig || exit 1
: > "$out/status.txt"
for i in $(seq 1 "${RUNS:-3}"); do
  for kind in pass fail; do
    dir=testdata/repos/orders-api
    [ "$kind" = fail ] && dir=testdata/verify/no-redis
    start=$SECONDS
    "$out/preconfig" verify --dir "$dir" "$@" --json --log "$out/$kind-$i.log" \
      > "$out/$kind-$i.jsonl" 2> "$out/$kind-$i.err"
    rc=$?
    echo "$kind $i exit $rc wall_s $((SECONDS - start))" >> "$out/status.txt"
  done
done
cat "$out/status.txt"
