#!/bin/sh
# Run a declared Druid build command and wait; never upload artifacts.
set -eu
exec python3 - "$@" <<'PY'
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

if len(sys.argv) != 2:
    sys.exit("Usage: druid-build-ui.sh <buildcommand>")
command = sys.argv[1]
base = os.environ["DRUID_API_URL"].rstrip("/") + "/api/v1"

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None

opener = urllib.request.build_opener(NoRedirect)

def request(path, payload=None):
    headers = {"Content-Type": "application/json"}
    token_file = os.environ.get("DRUID_TOKEN_FILE")
    if token_file:
        with open(token_file) as credential:
            headers["Authorization"] = "Bearer " + credential.read().strip()
    data = None if payload is None else json.dumps(payload).encode()
    try:
        with opener.open(urllib.request.Request(base + path, data=data, headers=headers), timeout=40) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        sys.exit("Druid build request failed (HTTP %s)" % error.code)
    except urllib.error.URLError:
        sys.exit("Druid build request could not connect")

request("/command", {"command": command})
while True:
    result = request("/commands/" + urllib.parse.quote(command, safe="") + "/wait?timeout_seconds=30")
    if result["status"] == "done":
        break
    if result["status"] == "error":
        sys.exit("Druid build command failed; inspect its logs")
PY
