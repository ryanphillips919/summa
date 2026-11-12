#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/aws-common.sh"
started=$SECONDS
require_aws
command -v python3 >/dev/null || { echo "Python 3 is required." >&2; exit 1; }

if stack_exists; then
  check_stack_tag
  status=$(aws cloudformation describe-stacks --stack-name "$STACK_NAME" --query 'Stacks[0].StackStatus' --output text)
  if [[ "$status" != CREATE_COMPLETE && "$status" != UPDATE_COMPLETE ]]; then
    echo "The existing demo stack is $status. Check CloudFormation before proceeding." >&2; exit 1
  fi
  echo "The demo stack already exists. Checking its current health."
else
  : "${SUMMA_SUBNET_ID:?Set SUMMA_SUBNET_ID to a public subnet ID; see docs/aws-deployment.md}"
  subnet=$(aws ec2 describe-subnets --subnet-ids "$SUMMA_SUBNET_ID" --query 'Subnets[0].VpcId' --output text)
  [[ "$subnet" == vpc-* ]] || { echo "Could not determine the subnet's VPC." >&2; exit 1; }
  aws ssm describe-parameters --parameter-filters "Key=Name,Option=Equals,Values=$PARAMETER_NAME" --query 'Parameters[0].Type' --output text | grep -qx SecureString || {
    echo "The required SecureString parameter is missing; see docs/aws-deployment.md." >&2; exit 1;
  }
  digest=$(aws ecr describe-images --repository-name "$REPOSITORY" --image-ids imageTag=demo --query 'imageDetails[0].imageDigest' --output text)
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "Publish the demo image first; see docs/aws-deployment.md." >&2; exit 1; }
  count=$(python3 "$ROOT_DIR/scripts/cloudflare-connectors.py" --verify-stored-token)
  [[ "$count" == 0 ]] || { echo "The existing summa-ec2 tunnel has $count active connector(s). Stop and investigate them before deploying." >&2; exit 1; }
  echo "Existing running or starting EC2 instances in account $ACCOUNT_ID, region $AWS_REGION:"
  aws ec2 describe-instances --filters 'Name=instance-state-name,Values=pending,running' --query 'Reservations[].Instances[].[InstanceId,State.Name,Tags[?Key==`Name`].Value|[0]]' --output table
  image="$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/$REPOSITORY@$digest"
  echo "Creating $STACK_NAME. First boot can take several minutes."
  aws cloudformation create-stack --stack-name "$STACK_NAME" --template-body "file://$ROOT_DIR/deploy/cloudformation.yaml" \
    --parameters "ParameterKey=SubnetId,ParameterValue=$SUMMA_SUBNET_ID" "ParameterKey=VpcId,ParameterValue=$subnet" "ParameterKey=ImageUri,ParameterValue=$image" \
    --capabilities CAPABILITY_IAM --tags Key=summa:purpose,Value=disposable-demo >/dev/null
  aws cloudformation wait stack-create-complete --stack-name "$STACK_NAME" || {
    echo "Stack creation failed. Inspect CloudFormation events; see docs/aws-deployment.md." >&2; exit 1;
  }
fi

instance=$(aws cloudformation describe-stacks --stack-name "$STACK_NAME" --query "Stacks[0].Outputs[?OutputKey=='InstanceId'].OutputValue | [0]" --output text)
[[ "$instance" == i-* ]] || { echo "Stack did not return an instance ID." >&2; exit 1; }
for attempt in $(seq 1 60); do
  managed=$(aws ssm describe-instance-information --filters "Key=InstanceIds,Values=$instance" --query 'InstanceInformationList[0].PingStatus' --output text)
  [[ "$managed" == Online ]] && break
  sleep 2
done
[[ "$managed" == Online ]] || { echo "Instance did not become available in Systems Manager." >&2; exit 1; }
command_id=$(aws ssm send-command --instance-ids "$instance" --document-name AWS-RunShellScript \
  --parameters 'commands=["systemctl is-active --quiet cloudflared && curl --fail --silent http://127.0.0.1:8080/health >/dev/null"]' \
  --query 'Command.CommandId' --output text)
command_status=Pending
for attempt in $(seq 1 60); do
  # Command invocations can take a moment to appear in Systems Manager.
  command_status=$(aws ssm get-command-invocation --command-id "$command_id" --instance-id "$instance" --query Status --output text 2>/dev/null || true)
  case "$command_status" in
    Success) break ;;
    Failed|Cancelled|TimedOut|Cancelling) break ;;
  esac
  sleep 2
done
[[ "$command_status" == Success ]] || { echo "Instance health check did not succeed (status: $command_status)." >&2; exit 1; }
count=$(connector_count)
[[ "$count" == 1 ]] || { echo "Expected one active summa-ec2 connector; found $count. Check the tunnel before using the app." >&2; exit 1; }
echo "Summa is healthy at https://app.summabooks.app (Cloudflare Access sign-in may be required)."
echo "Elapsed: $((SECONDS - started)) seconds."
