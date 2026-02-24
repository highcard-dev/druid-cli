#!/bin/bash

MAX=${DRUID_MAX_MEMORY%?}
if [ -z "${MAX}" ];
then
    MAX=1024M
fi

java -Xmx$MAX -Xms1024M -jar forge-*-shim.jar nogui