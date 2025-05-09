#!/bin/bash

#check if first argument is there, set CHANNEL to it, otherwise default to latest
if [ -z "$CHANNEL" ]; then
    URL_PATH="latest/download"
else
    URL_PATH=download/$CHANNEL
fi

BASEDIR=$(dirname "$0")

wget --show-progress -q -O $BASEDIR/druid https://github.com/highcard-dev/druid-cli/releases/$URL_PATH/druid
wget --show-progress -q -O $BASEDIR/druid_rcon https://github.com/highcard-dev/druid-cli/releases/$URL_PATH/druid_rcon 
wget --show-progress -q -O $BASEDIR/druid_rcon_web_rust https://github.com/highcard-dev/druid-cli/releases/$URL_PATH/druid_rcon_web_rust
wget --show-progress -q -O $BASEDIR/entrypoint.sh https://github.com/highcard-dev/druid-cli/releases/$URL_PATH/entrypoint.sh 
chmod +x $BASEDIR/druid $BASEDIR/druid_rcon $BASEDIR/druid_rcon_web_rust

# Modify the PATH variable to prioritize /app/resources
export PATH=$BASEDIR:$PATH

exec bash $BASEDIR/entrypoint.sh $@
