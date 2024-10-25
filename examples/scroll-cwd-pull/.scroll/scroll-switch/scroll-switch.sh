NEWVERSION=$1

#wget -O forge-installer-new.jar https://s3.eu-central-1.wasabisys.com/druid-scroll-artifacts/minecraft/forge/forge-$NEWVERSION.jar

#java -jar forge-installer-new.jar --installServer
#rm forge-installer-new.jar

# from forge version 1.20.3 on, we have to provide our own run.sh and overwrite the exiting one
# we need to check if we switch from a version before 1.20.3 to a version after 1.20.3
# we can use `druid scroll app_version ge 1.20.3` to check this
# we can use `druid scroll semver $NEWVERSION ge 1.20.3` to check this

if druid app_version ge 1.20.3; then
  echo "Already using forge version 1.20.3 or newer, run.sh is up to date"
  exit 0
fi

if druid app_version $NEWVERSION ge 1.20.3; then
  echo "$SCROLL_DIR/scroll-switch/run.sh ./run.sh"
  cp $SCROLL_DIR/scroll-switch/run.sh ./run.sh
fi