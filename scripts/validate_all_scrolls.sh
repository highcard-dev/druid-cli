#!/bin/bash

set -e

for SCROLL_FILE in examples/*/scroll.yaml; do
    SCROLL_DIR=$(dirname "$SCROLL_FILE")
    echo "Validating $SCROLL_DIR"
    go run ./apps/druid validate --strict "$SCROLL_DIR"
done
