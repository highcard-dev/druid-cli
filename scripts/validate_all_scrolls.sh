#!/bin/bash

set -e

ALL_SCROLL_DIRS=$(find . -type f -name "scroll.yaml" -exec dirname {} \; | sort | uniq)

for SCROLL_DIR in $ALL_SCROLL_DIRS; do
    echo "Validating $SCROLL_DIR"
    go run main.go scroll validate $SCROLL_DIR
done