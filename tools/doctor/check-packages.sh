#!/usr/bin/env bash
# Checks that every package name Doctor can write into a spec exists in
# Ubuntu 24.04's archive: a clean ubuntu:24.04 container runs apt-get update,
# then apt-cache policy for each name. Needs Docker, and Ubuntu's archive.
#
#   bash tools/doctor/check-packages.sh [--network NET]
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
net=()
[ "${1:-}" = "--network" ] && net=(--network "$2")
names=$(cd "$root" && go run ./tools/doctor/packages)
docker run --rm -i "${net[@]}" ubuntu:24.04 bash -s <<SCRIPT
apt-get update -qq >/dev/null 2>&1
ok=0; missing=0
for p in $(echo $names); do
  # A real package has a candidate version; a virtual name has none.
  if apt-cache policy "\$p" 2>/dev/null | grep -q "Candidate: [0-9]"; then ok=\$((ok+1)); else echo "MISSING \$p"; missing=\$((missing+1)); fi
done
echo "\$ok of \$((ok+missing)) package names found in Ubuntu 24.04's archive"
[ "\$missing" -eq 0 ]
SCRIPT
