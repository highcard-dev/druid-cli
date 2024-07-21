#!/bin/bash

#check if first argument is there, set CHANNEL to it, otherwise default to latest
if [ -z "$CHANNEL" ]; then
    URL_PATH="latest/download"
else
    URL_PATH=download/$CHANNEL
fi

wget -O /app/resources/druid https://github.com/highcard-dev/druid-cli/releases/$URL_PATH/druid
wget -O /app/resources/druid_rcon https://github.com/highcard-dev/druid-cli/releases/$URL_PATH/druid_rcon 
wget -O /app/resources/druid_rcon_web https://github.com/highcard-dev/druid-cli/releases/$URL_PATH/druid_rcon_web
wget -O /app/resources/entrypoint.sh https://github.com/highcard-dev/druid-cli/releases/$URL_PATH/entrypoint.sh 
chmod +x /app/resources/druid /app/resources/druid_rcon /app/resources/druid_rcon_web

# Modify the PATH variable to prioritize /app/resources
export PATH=/app/resources:$PATH

bash /app/resources/entrypoint.sh $@
