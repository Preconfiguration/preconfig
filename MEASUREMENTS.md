# Measurements

The Alpha's figures, as measured on September 29 and 30, 2026 (dates in UTC).
The checks ran twice on the final code: a first round on September 29, and a
second on September 30, after the fixes from a review of the code, the
documents and the site.

**Test machine:** a cloud virtual machine with two CPUs, 7 GB of memory,
Linux 6.18, Docker 29.4.3 with the vfs storage driver, Go 1.24.7, TinyGo
0.39.0 and Node.js 22. Up to the first round it ran on an Intel Xeon at
2.1 GHz; after a restart it ran on a Xeon at 2.8 GHz, where the second round
ran. It reaches the internet through a proxy that inspects TLS, so verify ran
with `--network host` and `--ca-file`. It could reach Ubuntu's archive, PyPI
and npm, but not NodeSource, the PostgreSQL project's archive, Redis's
packages or Go's downloads.

## Tests

The second round, on the final code:

| What | Result |
|---|---|
| `go test ./...` | 110 test functions (plus 6 fuzz targets and 4 benchmarks), all passing |
| Coverage, `go test -cover ./cmd/preconfig ./internal/...` | 90.5% of statements |
| Planted bugs, `tools/mutate.py` | 32 of 32 caught (tools/mutate-results.json) |
| Fuzzing | 25 s per target, 2,436,185 inputs, no failures: FuzzLoad 480,510; yaml FuzzParse 256,722; FuzzYamlStr 697,814; FuzzDetect 46,933; jsonc FuzzParse 948,261; FuzzCheck 5,945 |
| Quoting, `tools/quoting/run.sh` | 64,686 strings, 1,083 of them written without quotes; 0 changed in PyYAML 6.0.3, ruamel.yaml 0.19.1, yaml 2.9.1 (YAML 1.2 and 1.1 modes) and js-yaml 4.3.2 |
| Platform checks, `tools/validate-all.sh` | 7 specs: 28 JSON schema checks, 0 failed; actionlint 7 of 7; shellcheck 7 of 7; docker compose config 5 of 5; devcontainers CLI 7 of 7; cloud-init 26.1 `schema -c` 7 of 7 |
| Browser build, `tools/wasm/run.sh` | 39 of 39 answers identical between the TinyGo and Go builds; 21 of 21 generated files identical to the command line's |

The recorded fuzz runs came to about 25 million inputs, with no failures:
19.2 million in two-minute runs of each target during development, 3.1
million in the first round and 2.4 million in the second. Before them, an
early run of FuzzYamlStr found one value the quoting got wrong, `.0`, which
read back as a number; it was fixed, and the input is kept as a test in
internal/detect/testdata/fuzz.

## verify on a clean machine

The orders-api sample (Python 3.12, PostgreSQL 16, Redis 7) on `ubuntu:24.04`.
The six runs of September 29 were recorded the way tools/demo/record-verify.sh
records them, and the demo replays the first of each kind, kept in
testdata/verify.

| Run | Result | Time |
|---|---|---|
| Full spec, 1 to 3 | READY, exit 0; Python 3.12.3, PostgreSQL 16.15, Redis 7.0.15; 4 passed | 49.6 s, 68.7 s, 73.0 s |
| Redis left out, 1 to 3 | NOT READY, exit 1 at the ready step; 3 failed, 1 passed | 55.1 s, 64.4 s, 71.0 s |
| First round, TestRealDocker | READY | 51.3 s |
| First round, record-verify.sh | READY; and NOT READY without Redis | 56.3 s; 50.5 s |
| First round, the demo kit's `run-demo.sh --verify` | READY; and NOT READY without Redis | 52.8 s; 52.9 s |
| Second round, TestRealDocker | READY | 62.2 s |
| Second round, the demo kit's `run-demo.sh --verify` | READY; and NOT READY without Redis | 55.8 s; 54.0 s |

Step times in the fastest run: system packages 14.8 s, Python 6.8 s,
PostgreSQL 9.9 s, Redis 3.6 s, services 2.6 s, project 6.9 s, tests 0.5 s,
then about 3.6 s for Docker to remove the container. Most of the spread
between the six recorded runs is downloads: the system packages step took
14.8 to 24.0 s.

The Cursor Dockerfile for orders-api built in 45.4 s during development, then
42.4 s and 52.3 s in the two rounds; after each of those two builds, the image
started PostgreSQL and Redis with the environment's start command.

## Speed and size

Go benchmarks, three runs each, on the first round's code at 2.1 GHz and the
final code at 2.8 GHz:

| What | 2.1 GHz | 2.8 GHz, final code |
|---|---|---|
| build, orders-api, seven files | 0.216 to 0.225 ms | 0.192 to 0.218 ms |
| check, orders-api, clean | 0.372 to 0.417 ms | 0.375 to 0.430 ms |
| check, the hand-written files | 0.292 to 0.305 ms | 0.305 to 0.342 ms |
| detect, orders-api | 0.043 to 0.049 ms | 0.049 to 0.052 ms |

| What | Result |
|---|---|
| The commands, including program start | About 30 ms, where starting any program takes about 15 ms (2.1 GHz) |
| Binaries (`-trimpath -ldflags "-s -w"`, no cgo), final code | linux/amd64 3,272,888; linux/arm64 3,080,376; darwin/amd64 3,279,232; darwin/arm64 3,119,442; windows/amd64 3,398,144 bytes |
| Browser engine, TinyGo (`-opt=z -no-debug -stack-size=512KB`), final code | 1,024,611 bytes, 372,889 gzipped |
| Browser engine, standard Go, final code | 4,817,129 bytes, 1,311,479 gzipped |
| Browser engine in Node.js 22, medians of 50 calls | Five runs at 2.1 GHz: start 14 to 21 ms (median 17); build 1.2 to 1.5 ms; check 2.2 to 3.1 ms; detect 0.8 to 0.9 ms. One run at 2.8 GHz, final code: start 19.0 ms; build 1.47 ms; check 2.98 ms; detect 0.85 ms |
| The demo's engine.v1.js (the WebAssembly as text, with its loader), final code | 1,384,397 bytes, 495,584 gzipped |
