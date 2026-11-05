#!/bin/sh
# Container conformance for the dork_discovery engine runtime image.
set -eu

engine_binary="${1:-/opt/lunafox-engine/bin/dork-discovery-engine}"

test -x "$engine_binary" || { echo "engine binary is not executable: $engine_binary" >&2; exit 1; }
test -f /etc/ssl/certs/ca-certificates.crt || { echo "CA bundle is missing" >&2; exit 1; }
test "$(id -u)" = "0" || { echo "runtime user must be root for this engine image" >&2; exit 1; }
"$engine_binary" >/dev/null 2>&1 && exit 1 || true
echo "conformance-passed"
