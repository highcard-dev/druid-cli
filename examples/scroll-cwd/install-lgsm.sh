set -e

wget https://codeload.github.com/GameServerManagers/LinuxGSM/zip/refs/heads/master -O lgsm.zip
unzip lgsm.zip

#recreate lgsm dir
rm -rf lgsm
mkdir lgsm


mv LinuxGSM-master/linuxgsm.sh .
mv LinuxGSM-master/lgsm/modules/ lgsm
chmod -R +x lgsm/modules/

rm -rf LinuxGSM-master
rm lgsm.zip

#self delete
rm -- "$0"