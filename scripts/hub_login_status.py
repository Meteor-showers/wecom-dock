import json
import os
import urllib.error
import urllib.request

req = urllib.request.Request(
    "https://hub.docker.com/v2/users/login/",
    data=json.dumps({"username": os.environ["U"], "password": os.environ["T"]}).encode(),
    headers={"Content-Type": "application/json"},
    method="POST",
)
try:
    with urllib.request.urlopen(req, timeout=30) as resp:
        print(resp.status)
except urllib.error.HTTPError as err:
    print(err.code)
