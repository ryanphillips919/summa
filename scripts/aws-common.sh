#!/usr/bin/env bash
# Shared settings for the disposable demo stack.
set -euo pipefail
STACK_NAME=summa-demo
PARAMETER_NAME=/summa/cloudflare/tunnel-token
REPOSITORY=summa
ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
export AWS_PAGER=""

require_aws() {
  command -v aws >/dev/null || { echo "Install AWS CLI v2 first." >&2; exit 1; }
  AWS_REGION=${AWS_REGION:-${AWS_DEFAULT_REGION:-$(aws configure get region || true)}}
  [[ -n "$AWS_REGION" ]] || { echo "Set AWS_REGION or configure a default AWS region." >&2; exit 1; }
  export AWS_REGION
  ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text) || {
    echo "AWS authentication failed. Run aws configure sso and aws sso login, or configure your existing profile." >&2; exit 1;
  }
  [[ "$ACCOUNT_ID" =~ ^[0-9]{12}$ ]] || { echo "Could not identify the AWS account." >&2; exit 1; }
}

# Distinguish a missing stack from authentication, permissions, and network errors.
stack_exists() {
  local error_file
  error_file=$(mktemp)
  if aws cloudformation describe-stacks --stack-name "$STACK_NAME" >/dev/null 2>"$error_file"; then
    rm -f "$error_file"
    return 0
  fi
  if grep -q 'ValidationError.*does not exist' "$error_file"; then
    rm -f "$error_file"
    return 1
  fi
  cat "$error_file" >&2
  rm -f "$error_file"
  exit 1
}

check_stack_tag() {
  local tag
  tag=$(aws cloudformation describe-stacks --stack-name "$STACK_NAME" --query "Stacks[0].Tags[?Key=='summa:purpose'].Value | [0]" --output text)
  [[ "$tag" == disposable-demo ]] || { echo "Stack exists but does not have the expected Summa demo tag; refusing to operate on it." >&2; exit 1; }
}

connector_count() {
  python3 "$ROOT_DIR/scripts/cloudflare-connectors.py"
}
