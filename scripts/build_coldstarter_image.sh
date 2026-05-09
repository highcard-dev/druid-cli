#!/usr/bin/env bash

set -euo pipefail

IMAGE="${IMAGE:-druid-coldstarter:local}"
VERSION="${VERSION:-local}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "Building local coldstarter image: ${IMAGE}"
docker build \
  --file "${ROOT_DIR}/Dockerfile.coldstarter" \
  --build-arg "VERSION=${VERSION}" \
  --tag "${IMAGE}" \
  "${ROOT_DIR}"

echo "Built ${IMAGE}"
