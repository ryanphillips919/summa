"""Exercise destructive and connector safety checks without AWS or Docker."""
import os
import base64
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[2]


class DeploymentScriptsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.log = self.directory / "calls"
        aws = self.directory / "aws"
        aws.write_text("""#!/usr/bin/env bash
echo "$*" >> "$MOCK_CALLS"
case "$*" in
  'sts get-caller-identity'*) echo 123456789012 ;;
  *'describe-stacks'*'Tags'*) echo disposable-demo ;;
  *'describe-stacks'*'StackId'*) echo arn:aws:cloudformation:us-east-1:123456789012:stack/summa-demo/unique-id ;;
  *'describe-stacks'*) exit 0 ;;
  *'describe-subnets'*) echo vpc-example ;;
  *'describe-parameters'*) echo SecureString ;;
  *'describe-images'*) printf 'sha256:'; printf 'a%.0s' {1..64}; echo ;;
  *'get-login-password'*) echo mock-password ;;
esac
""")
        aws.chmod(0o755)
        python = self.directory / "python3"
        python.write_text("#!/bin/sh\necho 1\n")
        python.chmod(0o755)
        self.env = dict(os.environ, PATH=f"{self.directory}:{os.environ['PATH']}",
                        AWS_REGION="us-east-1", MOCK_CALLS=str(self.log),
                        SUMMA_SUBNET_ID="subnet-example", CLOUDFLARE_ACCOUNT_ID="account",
                        CLOUDFLARE_TUNNEL_ID="tunnel", CLOUDFLARE_API_TOKEN="test-token")
        self.env.pop("SUMMA_CONNECTOR_VERIFIED_CLEAR", None)
        self.env.pop("SUMMA_ALLOW_UNVERIFIED_CONNECTORS", None)

    def run_script(self, name, input_text=""):
        return subprocess.run([str(ROOT / "scripts" / name)], input=input_text,
                              text=True, capture_output=True, env=self.env)

    def test_destroy_requires_exact_confirmation(self):
        result = self.run_script("destroy.sh", "yes\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("delete-stack", self.log.read_text())

    def test_destroy_deletes_only_identified_stack(self):
        result = self.run_script("destroy.sh", "summa-demo\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text()
        self.assertIn("delete-stack --stack-name arn:aws:cloudformation:us-east-1:123456789012:stack/summa-demo/unique-id", calls)
        self.assertIn("wait stack-delete-complete", calls)

    def test_destroy_rejects_unrecognized_stack(self):
        aws = self.directory / "aws"
        aws.write_text(aws.read_text().replace("echo disposable-demo", "echo unrelated"))
        result = self.run_script("destroy.sh", "summa-demo\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("delete-stack", self.log.read_text())

    def test_deploy_does_not_treat_access_denied_as_missing_stack(self):
        aws = self.directory / "aws"
        aws.write_text(aws.read_text().replace("*'describe-stacks'*) exit 0 ;;",
                                               "*'describe-stacks'*) echo 'AccessDenied' >&2; exit 1 ;;"))
        result = self.run_script("deploy.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("create-stack", self.log.read_text())

    def test_deploy_refuses_active_connector(self):
        aws = self.directory / "aws"
        script = aws.read_text().replace("*'describe-stacks'*) exit 0 ;;",
                                         "*'describe-stacks'*) echo 'ValidationError: Stack does not exist' >&2; exit 1 ;;")
        aws.write_text(script)
        result = self.run_script("deploy.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("active connector", result.stderr)
        self.assertNotIn("create-stack", self.log.read_text())

    def test_deploy_without_api_requires_manual_connector_check(self):
        self.env.pop("CLOUDFLARE_API_TOKEN")
        aws = self.directory / "aws"
        aws.write_text(aws.read_text().replace("*'describe-stacks'*) exit 0 ;;",
                                               "*'describe-stacks'*) echo 'ValidationError: Stack does not exist' >&2; exit 1 ;;"))
        result = self.run_script("deploy.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no active connectors", result.stderr)
        self.assertNotIn("create-stack", self.log.read_text())

    def test_manual_connector_confirmation_allows_deployment(self):
        self.env.pop("CLOUDFLARE_API_TOKEN")
        self.env["SUMMA_CONNECTOR_VERIFIED_CLEAR"] = "yes"
        aws = self.directory / "aws"
        script = aws.read_text().replace("*'describe-stacks'*) exit 0 ;;",
                                         "*'describe-stacks'*) echo 'ValidationError: Stack does not exist' >&2; exit 1 ;;")
        script = script.replace("  *'describe-subnets'*)", "  *'create-stack'*) exit 42 ;;\n  *'describe-subnets'*)")
        aws.write_text(script)
        result = self.run_script("deploy.sh")
        self.assertEqual(result.returncode, 42)
        self.assertIn("create-stack --stack-name summa-demo", self.log.read_text())

    def test_bootstrap_extracts_one_token_from_wrapped_parameter(self):
        template = (ROOT / "deploy/cloudformation.yaml").read_text()
        match = re.search(r"\| python3 -c '\n(.*?)\n\s*' > /etc/cloudflared/token", template, re.S)
        self.assertIsNotNone(match)
        script = textwrap.dedent(match.group(1))
        token = base64.b64encode(json.dumps({"a": "account", "t": "tunnel", "s": "example-secret"}).encode()).decode()
        for value in (token, "cloudflared service install " + token):
            result = subprocess.run([sys.executable, "-c", script], input=value,
                                    text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, token)
        result = subprocess.run([sys.executable, "-c", script], input=token + " " + token,
                                text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(token, result.stderr)

    def test_publish_uses_and_removes_private_docker_config(self):
        docker = self.directory / "docker"
        docker.write_text("""#!/usr/bin/env bash
if [[ "$1" == login ]]; then
  read -r ignored
  printf '%s' "$DOCKER_CONFIG" > "$MOCK_DOCKER_CONFIG"
fi
exit 0
""")
        docker.chmod(0o755)
        self.env["MOCK_DOCKER_CONFIG"] = str(self.directory / "docker-config-path")
        result = self.run_script("publish-image.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        config_dir = Path((self.directory / "docker-config-path").read_text())
        self.assertFalse(config_dir.exists())

    def test_authorized_unverified_connector_mode_allows_deployment(self):
        self.env.pop("CLOUDFLARE_API_TOKEN")
        self.env["SUMMA_ALLOW_UNVERIFIED_CONNECTORS"] = "yes"
        aws = self.directory / "aws"
        script = aws.read_text().replace("*'describe-stacks'*) exit 0 ;;",
                                         "*'describe-stacks'*) echo 'ValidationError: Stack does not exist' >&2; exit 1 ;;")
        script = script.replace("  *'describe-subnets'*)", "  *'create-stack'*) exit 42 ;;\n  *'describe-subnets'*)")
        aws.write_text(script)
        result = self.run_script("deploy.sh")
        self.assertEqual(result.returncode, 42)
        self.assertIn("without checking", result.stdout)
        self.assertIn("create-stack --stack-name summa-demo", self.log.read_text())


if __name__ == "__main__":
    unittest.main()
