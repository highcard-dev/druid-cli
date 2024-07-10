#!/bin/bash

#check if first argument is there, set CHANNEL to it, otherwise default to latest
if [ -z "$1" ]; then
    CHANNEL="$CHANNEL/download"
else
    CHANNEL=download/$1
fi

wget -O /app/resources/druid https://github.com/highcard-dev/druid-cli/releases/$CHANNEL/druid
wget -O /app/resources/druid_rcon https://github.com/highcard-dev/druid-cli/releases/$CHANNEL/druid_rcon 
wget -O /app/resources/druid_rcon_web https://github.com/highcard-dev/druid-cli/releases/$CHANNEL/druid_rcon_web
wget -O /app/resources/entrypoint.sh https://github.com/highcard-dev/druid-cli/releases/$CHANNEL/entrypoint.sh 
chmod +x /app/resources/druid /app/resources/druid_rcon /app/resources/druid_rcon_web

# Modify the PATH variable to prioritize /app/resources
export PATH=/app/resources:$PATH

bash /app/resources/entrypoint.sh

#https://github.com/highcard-dev/druid-cli/releases/download/0.1.8-prerelease3/druid