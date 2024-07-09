wget -O /app/resources/druid https://github.com/highcard-dev/druid-cli/releases/latest/download/druid
wget -O /app/resources/druid_rcon https://github.com/highcard-dev/druid-cli/releases/latest/download/druid_rcon 
wget -O /app/resources/druid_rcon_web https://github.com/highcard-dev/druid-cli/releases/latest/download/druid_rcon_web
wget -O /app/resources/entrypoint.sh https://github.com/highcard-dev/druid-cli/releases/latest/download/entrypoint.sh 
chmod +x /app/resources/druid /app/resources/druid_rcon /app/resources/druid_rcon_web

# Modify the PATH variable to prioritize /app/resources
export PATH=/app/resources:$PATH

bash /app/resources/entrypoint.sh