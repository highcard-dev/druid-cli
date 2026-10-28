#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DOCKERFILE="$SCRIPT_DIR/../Dockerfile"

python3 - "$DOCKERFILE" <<'PY'
from pathlib import Path
import sys

dockerfile = Path(sys.argv[1]).read_text(encoding="utf-8")

if not dockerfile.startswith("# syntax=docker/dockerfile:1\n"):
    raise SystemExit("Dockerfile does not opt into BuildKit cache mounts")
if "WORKDIR /src" not in dockerfile:
    raise SystemExit("Go module build must run outside GOPATH in /src")
if "COPY --from=builder /src/bin/druid* /usr/bin/" not in dockerfile:
    raise SystemExit("Runtime stage does not copy binaries from /src")

dependency_copy = dockerfile.find("COPY go.mod go.sum ./")
source_copy = dockerfile.find("COPY . .")
module_download = dockerfile.find("go mod download")
generator_install = dockerfile.find(
    "go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1"
)
version_env = dockerfile.find("ENV VERSION=${VERSION}")

if min(dependency_copy, source_copy, module_download, generator_install, version_env) < 0:
    raise SystemExit("Dockerfile is missing the dependency-first Go build layer")
if not dependency_copy < module_download < source_copy:
    raise SystemExit("Go modules are not prepared before the full source copy")
if not generator_install < source_copy:
    raise SystemExit("oapi-codegen is reinstalled after every source change")
if not generator_install < version_env < source_copy:
    raise SystemExit("VERSION invalidates the stable dependency/tool layer")

source_build = dockerfile[source_copy:]
for target in ("target=/go/pkg/mod", "target=/root/.cache/go-build"):
    if target not in source_build:
        raise SystemExit(f"Source build is missing cache mount {target}")
if "make build" not in source_build:
    raise SystemExit("Source build no longer runs the required make build target")
PY

echo "Docker build keeps required work while reusing Go dependency and compiler caches."
