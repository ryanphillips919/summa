#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/aws-common.sh"
started=$SECONDS
require_aws
if ! stack_exists; then
  echo "No $STACK_NAME stack exists in account $ACCOUNT_ID, region $AWS_REGION."
  exit 0
fi
check_stack_tag
stack_id=$(aws cloudformation describe-stacks --stack-name "$STACK_NAME" --query 'Stacks[0].StackId' --output text)
echo "This will delete only stack $STACK_NAME in account $ACCOUNT_ID, region $AWS_REGION, including its instance and demo data."
read -r -p "Type $STACK_NAME to confirm: " confirmation
[[ "$confirmation" == "$STACK_NAME" ]] || { echo "Cancelled."; exit 0; }
aws cloudformation delete-stack --stack-name "$stack_id"
aws cloudformation wait stack-delete-complete --stack-name "$stack_id"
echo "Demo stack deleted in $((SECONDS - started)) seconds. ECR, the tunnel and the stored token were preserved."
