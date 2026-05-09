#!/usr/bin/env bash

set -e
input=$@

# Global args derived from envs that apply to multiple commands
global_args=()
if [ ! -z "${DRUID_CWD}" ];
then
    global_args+=("--cwd=$DRUID_CWD")
fi

if [ ! -z "${DRUID_CONFIG}" ];
then
    global_args+=("--config=$DRUID_CONFIG")
fi

echo "Druid Version: $(druid version)"

if [ ! -z "${DRUID_REGISTRY_HOST}" ] && [ ! -z "${DRUID_REGISTRY_USER}" ] && [ ! -z "${DRUID_REGISTRY_PASSWORD}" ];
then
    echo "Logging into registry ${DRUID_REGISTRY_HOST}"
    druid login --host "${DRUID_REGISTRY_HOST}" -u "${DRUID_REGISTRY_USER}" -p "${DRUID_REGISTRY_PASSWORD}"
fi

# Serve as default when no command is provided.
if [ -z "$input" ]; then
    args=(serve)

    if [ ! -z "${DRUID_PORT}" ];
    then
        args+=("--tcp")
        args+=("--port" "${DRUID_PORT}")
    fi

    # Reuse global args (cwd/config) for serve as well
    args+=("${global_args[@]}")
        
    echo "Running druid with args from env: ${args[@]}"
    exec druid "${args[@]}"
else
    echo "Running druid with args: $@"

    # Start with user-provided args
    args=("$@")

    # Append global args unless explicitly specified by the user
    for g in "${global_args[@]}"; do
        key="${g%%=*}" # e.g. --cwd or --config
        skip=false
        for a in "${args[@]}"; do
            if [[ "$a" == "$key" || "$a" == "$key="* ]]; then
                skip=true
                break
            fi
        done
        $skip || args+=("$g")
    done

    exec druid "${args[@]}"
fi
