curl -L -o /app/resources/druid https://github.com/highcard-dev/druid-cli/releases/latest/download/druid
curl -L -o /app/resources/druid_rcon https://github.com/highcard-dev/druid-cli/releases/latest/download/druid_rcon 
curl -L -o /app/resources/druid_rcon_web https://github.com/highcard-dev/druid-cli/releases/latest/download/druid_rcon_web
curl -L -o /app/resources/entrypoint.sh https://github.com/highcard-dev/druid-cli/releases/latest/download/entrypoint.sh 
chmod +x /app/resources/druid /app/resources/druid_rcon /app/resources/druid_rcon_web

export PATH=$PATH:/app/resources

sh /app/resources/entrypoint.sh