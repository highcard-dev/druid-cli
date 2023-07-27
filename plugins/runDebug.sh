#echo "Running plugin in debug mode $1 $2 $3 $4 $5 $6 $7 $8 $9"

#set magicCookie=magicValue environemnt  variables from HandshakeConfig
#export TEST_PLUGIN=cookie_value
#set plugin vars
export PLUGIN_MIN_PORT=10000
export PLUGIN_MAX_PORT=25000
export PLUGIN_PROTOCOL_VERSIONS=1

#make sure plugin output is "original" without debugger messages by passing log-dest & tty arguments
dlv --listen=:40000 --headless=true --api-version=2 --accept-multiclient \
  --log-dest "dlv.log"  \
  --tty="" \
    exec $3 -- "$@"