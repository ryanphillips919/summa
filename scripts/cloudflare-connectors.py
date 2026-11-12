#!/usr/bin/env python3
"""Check the existing named tunnel without exposing the API token."""
import json
import os
import base64
import subprocess
import sys
import urllib.error
import urllib.request

account = os.environ.get("CLOUDFLARE_ACCOUNT_ID", "")
tunnel = os.environ.get("CLOUDFLARE_TUNNEL_ID", "")
token = os.environ.get("CLOUDFLARE_API_TOKEN", "")
if not all((account, tunnel, token)) or not all(
    c in "0123456789abcdefABCDEF-" for c in account + tunnel
):
    sys.exit("Set CLOUDFLARE_ACCOUNT_ID, CLOUDFLARE_TUNNEL_ID and CLOUDFLARE_API_TOKEN; see docs/aws-deployment.md.")


def get(path):
    request = urllib.request.Request(
        "https://api.cloudflare.com/client/v4/accounts/" + account + path,
        headers={"Authorization": "Bearer " + token},
    )
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            body = json.load(response)
    except (urllib.error.URLError, ValueError, OSError):
        sys.exit("Cloudflare API request failed; check network, account, tunnel ID and token permissions.")
    if not body.get("success"):
        sys.exit("Cloudflare API rejected the request; check the read-only token permissions.")
    return body["result"]


details = get("/cfd_tunnel/" + tunnel)
if details.get("name") != "summa-ec2" or details.get("deleted_at"):
    sys.exit("The tunnel ID does not identify the existing summa-ec2 tunnel.")
if sys.argv[1:] == ["--verify-stored-token"]:
    try:
        stored = subprocess.run(
            ["aws", "ssm", "get-parameter", "--name", "/summa/cloudflare/tunnel-token",
             "--with-decryption", "--query", "Parameter.Value", "--output", "text"],
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        decoded = json.loads(base64.b64decode(stored))
        if decoded.get("a") != account or decoded.get("t") != tunnel:
            sys.exit("The stored tunnel token does not match the specified Cloudflare account and tunnel.")
    except (subprocess.SubprocessError, ValueError, OSError):
        sys.exit("Could not verify the stored tunnel token. Check Parameter Store permissions and token format.")
connectors = get("/cfd_tunnel/" + tunnel + "/connections")
print(sum(1 for connector in connectors if connector.get("conns")))
