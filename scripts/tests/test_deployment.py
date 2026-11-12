"""Exercise destructive and connector safety checks without AWS or Docker."""
import os
from pathlib import Path
import subprocess
import tempfile
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
esac
""")
        aws.chmod(0o755)
        python = self.directory / "python3"
        python.write_text("#!/bin/sh\necho 1\n")
        python.chmod(0o755)
        self.env = dict(os.environ, PATH=f"{self.directory}:{os.environ['PATH']}",
                        AWS_REGION="us-east-1", MOCK_CALLS=str(self.log),
                        SUMMA_SUBNET_ID="subnet-example")

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


if __name__ == "__main__":
    unittest.main()
