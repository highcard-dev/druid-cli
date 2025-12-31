#!/usr/bin/env bash

set -e
SD=".scroll"
input=$@

echo "Druid Version: $(druid version)"

if [ ! -z "$ENSURE_NIX" ];
then
    if [ ! -e "$HOME/.nix-profile" ];
    then
        echo "Installing Nix package manager"
        sh <(curl --proto '=https' --tlsv1.2 -L https://nixos.org/nix/install) --no-daemon
        echo "Nix installed"
    fi
fi

#Check if we should serve as default or when only artifact is specified
if [ -z "$input" ] || [[ $input =~ ([^/]+)/([^:]+):([^/]+) ]] &&  [[ $input != *" "* ]]; then    artifact="${input}"
    if [ -z "${artifact}" ];
    then
        artifact=$DRUID_SCROLL_ARTIFACT
    fi

    echo "Artifact: $artifact"

    

    #Update command
    if [ "${DRUID_AUTO_UPDATE}" = "true" ] && [ -f "${SD}/scroll.yaml" ];
    then

        echo "Updating artifact"
        druid update 
        echo "Updated artifact"
    fi


    #Run command
    if [ ! -z "${artifact}" ] && [ -f "${SD}/scroll.yaml" ];
    then
        if ! command -v yq &> /dev/null
        then
            echo "WARN: yq not installed, skipping artifact check"
        else
            current=$(cat ${SD}/scroll.yaml | yq .name):$(cat ${SD}/scroll.yaml | yq .app_version)
            #compare desired artifact with current installed

            if [ "$current" != "$artifact" ];
            then
                echo "Switching from $current to $artifact"
                druid run scroll-switch.$artifact
            else
                echo "Desired artifact $artifact already installed"
            fi
        fi
    fi
    

    #ignore-version-check otherwise we have missmatch after update
    args=(serve --ignore-version-check --additional-endpoints annotations)

    if [ ! -z "${artifact}" ];
    then
        args+=($artifact)
    fi

    #Map envs to args
    if [ ! -z "${DRUID_JWKS_SERVER}" ];
    then
        args+=("--jwks-server" "${DRUID_JWKS_SERVER}")
    fi
    
    if [ ! -z "${DRUID_USER_ID}" ];
    then
        args+=("--user-id" "${DRUID_USER_ID}")
    fi

    if [ ! -z "${DRUID_PORT}" ];
    then
        args+=("--port" "${DRUID_PORT}")
    fi

    if [ ! -z "${DRUID_IDLE}" ];
    then
        args+=("--idle=$DRUID_IDLE")
    fi
    if [ ! -z "${DRUID_WATCH_PORTS}" ];
    then
        args+=("--watch-ports=$DRUID_WATCH_PORTS")
    fi

    if [ ! -z "${DRUID_WATCH_PORTS_INTERFACES}" ];
    then
        args+=("--watch-ports-interfaces" "${DRUID_WATCH_PORTS_INTERFACES}")
    fi

    if [ ! -z "${DRUID_COLDSTARTER}" ];
    then
        args+=("--coldstarter=$DRUID_COLDSTARTER")
    fi

    if [ ! -z "${DRUID_INIT_SNAPSHOT_URL}" ];
    then
        args+=("--init-snapshot-url=$DRUID_INIT_SNAPSHOT_URL")
    fi

    if [ ! -z "${DRUID_SKIP_ARTIFACT_DOWNLOAD}" ];
    then
        args+=("--skip-artifact-download")
    fi

    if [ ! -z "${DRUID_CWD}" ];
    then
        args+=("--cwd=$DRUID_CWD")
    fi

    if [ ! -z "${PPROF_BIND}" ];
    then
        args+=("--pprof=$PPROF_BIND")
    fi
        
    echo "Running druid with args from env: ${args[@]}"
    exec druid "${args[@]}"
else
    echo "Running druid with args: $@"
    exec druid "$@"
fi