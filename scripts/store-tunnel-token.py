#!/usr/bin/env python3
"""Store the existing tunnel token without placing it in shell history or argv."""
import getpass
import json
import subprocess
import sys

print("Paste the existing summa-ec2 tunnel token. Input is hidden.")
token = getpass.getpass("Tunnel token: ").strip()
if not token:
    sys.exit("No token supplied.")
request = json.dumps({
    "Name": "/summa/cloudflare/tunnel-token",
    "Value": token,
    "Type": "SecureString",
})
try:
    subprocess.run(["aws", "ssm", "put-parameter", "--cli-input-json", "file:///dev/stdin",
                    "--query", "Version", "--output", "text"], input=request, text=True, check=True)
except (OSError, subprocess.CalledProcessError):
    sys.exit("Could not store the parameter. Check AWS authentication, region, and permissions.")
print("Stored the token as a SecureString. Existing parameters are not overwritten.")
