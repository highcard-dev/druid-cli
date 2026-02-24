#default update script

SCRIPTPATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"

if [ ! -f "$SCRIPTPATH/scroll-lock.json" ]; then
	echo "Scroll lock not found. Skipping update"
	exit 0
fi

if [ -z "$(ls $SCRIPTPATH/update)" ]; then
   echo "Update directory is empty. Skipping update"
else
	versionsDirs=$(find $SCRIPTPATH/update/* -maxdepth 0 -type d | sort --version-sort)
	current=$(cat $SCRIPTPATH/scroll-lock.json | jq -r .scroll_version)

	for versionsDir in $versionsDirs
	do
		version=$(basename $versionsDir)
		if [ ! "$(printf '%s\n' "$version" "$current" | sort -V | head -n1)" = "$version" ] ;
		then
			echo "$versionsDir/update.sh"
			if [ -f "$versionsDir/update.sh" ]; then
				sh $versionsDir/update.sh
			else
				echo "Warning: update $version has no update.sh... skipping"
			fi
		fi
	done
fi



LATEST_VERSION=$(cat $SCRIPTPATH/scroll.yaml | yq -r .version)
jq --arg LV "$LATEST_VERSION" -r '.scroll_version = $LV' $SCRIPTPATH/scroll-lock.json | sponge $SCRIPTPATH/scroll-lock.json 