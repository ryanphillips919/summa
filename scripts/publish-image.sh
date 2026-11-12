#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/aws-common.sh"
require_aws
command -v docker >/dev/null || { echo "Docker is required." >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "Start Docker and enable WSL integration if necessary." >&2; exit 1; }
aws ecr describe-repositories --repository-names "$REPOSITORY" >/dev/null || {
  echo "Create the ECR repository first; see docs/aws-deployment.md." >&2; exit 1;
}
registry="$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com"
aws ecr get-login-password | docker login --username AWS --password-stdin "$registry" >/dev/null
docker buildx build --platform linux/amd64 --push --tag "$registry/$REPOSITORY:demo" "$ROOT_DIR"
echo "Published $registry/$REPOSITORY:demo"
