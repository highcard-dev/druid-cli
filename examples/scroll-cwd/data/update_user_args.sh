#!/usr/bin/env sh

MAX=${DRUID_MAX_MEMORY%?}
if [ -z "${MAX}" ];
then
    MAX=1024M
fi


echo -Xmx$MAX > user_jvm_args.txt