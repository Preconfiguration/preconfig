#!/bin/sh
# Runs a built preconfig binary through its main commands, the way the release
# workflow checks each binary before anything is published.
#
#   sh tools/smoke.sh BINARY [VERSION]
#
# With VERSION, the binary has to report that version. It works in a folder of
# its own under the current one and removes it at the end; on Windows it runs
# under Git Bash, so every path it hands the binary is relative.
set -eu

bin=$1
want=${2:-}
case "$bin" in
/*) ;;
*) bin=$(pwd)/$bin ;;
esac
root=$(cd "$(dirname "$0")/.." && pwd)
work=smoke-work
rm -rf "$work"
mkdir "$work"
cd "$work"

fail() {
  echo "smoke: $*" >&2
  exit 1
}

# 1. version
v=$("$bin" version)
echo "$v"
case "$v" in
"preconfig $want "*) ;;
*) [ -z "$want" ] || fail "expected preconfig $want, got: $v" ;;
esac

# 2. targets
"$bin" targets > /dev/null || fail "targets failed"

# 3. check, detect, build and check again, on a sample repository
cp -R "$root/testdata/repos/orders-api" repo
(cd repo && "$bin" check) || fail "check failed on orders-api as shipped"
cp repo/preconfig.yaml spec.yaml
rm repo/preconfig.yaml
(cd repo && "$bin" detect --write) || fail "detect --write failed"
[ -s repo/preconfig.yaml ] || fail "detect --write wrote no preconfig.yaml"
cp spec.yaml repo/preconfig.yaml
(cd repo && "$bin" build) > /dev/null || fail "build failed"
out=$(cd repo && "$bin" check) || fail "check failed after build"
case "$out" in
*" 0 errors, 0 warnings"*) ;;
*) fail "check after build: $out" ;;
esac

# 4. doctor reads a failed setup log, names the cause and fixes the spec
mkdir doc
cp "$root/testdata/doctor/corpus/typo-package/spec.yaml" doc/preconfig.yaml
cp "$root/testdata/doctor/corpus/typo-package/log.txt" doc/log.txt
set +e
out=$(cd doc && "$bin" doctor log.txt)
code=$?
set -e
[ "$code" -eq 1 ] || fail "doctor exited $code, expected 1"
case "$out" in
*"libpq-dev is the likely name"*) ;;
*) fail "doctor didn't name the cause: $out" ;;
esac
(cd doc && "$bin" doctor --fix log.txt) > /dev/null || fail "doctor --fix failed"
grep -q 'libpq-devv' doc/preconfig.yaml && fail "doctor --fix left libpq-devv in the spec"
grep -q 'libpq-dev' doc/preconfig.yaml || fail "doctor --fix removed the package"

cd ..
rm -rf "$work"
echo "smoke: passed"
