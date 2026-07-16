#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
callback_url="${DRUID_WORKER_CALLBACK_URL:-}"

if [[ -z "$callback_url" ]]; then
    host_services_ip="${DRUID_HOST_SERVICES_IP:-}"
    if [[ -z "$host_services_ip" ]]; then
        host_services_ip="$({
            ip -4 route get 1.1.1.1 2>/dev/null \
                | sed -n 's/.* src \([^ ]*\).*/\1/p' \
                | head -n 1
        } || true)"
    fi

    if [[ -z "$host_services_ip" ]]; then
        echo "Unable to determine the WSL callback address; set DRUID_WORKER_CALLBACK_URL explicitly." >&2
        exit 1
    fi

    callback_url="http://${host_services_ip}:8083"
fi

exec make -C "$ROOT" watch DRUID_WORKER_CALLBACK_URL="$callback_url"
