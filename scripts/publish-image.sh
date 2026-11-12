#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/aws-common.sh"
require_aws
command -v docker >/dev/null || { echo "Docker is required." >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "Start Docker and enable WSL integration if necessary." >&2; exit 1; }
aws ecr describe-repositories --repository-names "$REPOSITORY" >/dev/null || {
  echo "Create the ECR repository first; see docs/aws-deployment.md." >&2; exit 1;
}
# Use a private temporary login so Windows credential helpers do not interfere.
docker_config=$(mktemp -d)
chmod 700 "$docker_config"
trap 'rm -rf "$docker_config"' EXIT
export DOCKER_CONFIG="$docker_config"
registry="$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com"
aws ecr get-login-password | docker login --username AWS --password-stdin "$registry" >/dev/null
docker buildx build --platform linux/amd64 --push --tag "$registry/$REPOSITORY:demo" "$ROOT_DIR"
echo "Published $registry/$REPOSITORY:demo"
