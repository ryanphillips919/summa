# Disposable AWS demo

This creates one `t3.small` EC2 instance in the existing VPC and connects it to
the existing `summa-ec2` Cloudflare Tunnel. Nothing listens publicly on EC2:
the security group has no inbound rules, and Docker binds Summa to
`127.0.0.1:8080`. Cloudflare Access remains responsible for access to
`https://app.summabooks.app`.

EC2, its public IPv4 address, EBS storage, ECR storage and data transfer may
cost money. Startup and deletion may take longer than five minutes. SQLite and
NATS data are **deleted** with the instance; each new deployment seeds a fresh
Northstar demo. Stop using the environment before destroying it.

## One-time setup

1. Install AWS CLI v2, Python 3, Docker with Buildx, and Git. On Windows, use
   WSL 2 and run these commands inside the Linux shell. Install Docker Desktop,
   enable its WSL integration for your distribution, and confirm `docker info`
   succeeds. Use the AWS CLI installed in WSL; Windows and WSL may have
   separate AWS credential files.
2. Authenticate with your team's AWS account, for example:

   ```bash
   aws configure sso
   aws sso login
   export AWS_PROFILE=your-profile
   export AWS_REGION=us-east-1  # use your team's actual region
   aws sts get-caller-identity
   ```

   The deployer needs permissions for CloudFormation, EC2, IAM role creation
   and passing, ECR image reads, Systems Manager parameter reads and commands,
   and KMS decryption of the parameter. The setup steps also need ECR repository
   creation and image upload, plus SSM parameter creation. Ask your AWS admin
   for appropriate permissions; don't paste credentials into chat.
3. Choose an existing **public subnet** in a VPC with a route to an internet
   gateway. Its network ACLs and outbound routes must allow HTTPS, DNS and
   Cloudflare Tunnel traffic. The instance gets a public IPv4 address for
   outbound access, but the security group accepts no inbound traffic. Set:

   ```bash
   aws ec2 describe-subnets --query 'Subnets[].[SubnetId,VpcId,AvailabilityZone]' --output table
   export SUMMA_SUBNET_ID=subnet-replace-me
   ```

   Select a subnet after checking its route table; listing subnets alone does
   not establish that one is public. `SUMMA_SUBNET_ID`, `AWS_REGION` and
   `AWS_PROFILE` need to be set again in a new shell (or configured locally).
4. Check for the old instance and the existing tunnel connector before the
   first deployment. Do not terminate or change them without the owner's
   approval. For example, list instances in the selected account and region:

   ```bash
   aws ec2 describe-instances --filters Name=instance-state-name,Values=pending,running \
     --query 'Reservations[].Instances[].[InstanceId,State.Name,Tags[?Key==`Name`].Value|[0]]' --output table
   ```

   Also check Cloudflare Zero Trust > Networks > Tunnels > `summa-ec2` for
   active connectors. This deployment expects the existing **remotely managed**
   tunnel to route `app.summabooks.app` to `http://127.0.0.1:8080` and the
   existing Access application to protect it. It does not change either.
5. For automatic connector checks, use a separate, read-only API token with
   account **Cloudflare Tunnel: Read** permission. Set the account ID and the
   existing `summa-ec2` tunnel ID from the dashboard. Enter the token with
   hidden input so it does not go into shell history:

   ```bash
   export CLOUDFLARE_ACCOUNT_ID=your-account-id
   export CLOUDFLARE_TUNNEL_ID=your-tunnel-uuid
   read -r -s -p 'Cloudflare read-only API token: ' CLOUDFLARE_API_TOKEN; echo
   export CLOUDFLARE_API_TOKEN
   python3 scripts/cloudflare-connectors.py
   ```

   The last command prints the number of active connectors. Deploy refuses to
   create a new server unless that number is zero. Store the API token securely
   according to your team's practices; enter it again in each new shell.
   Alternatively, inspect `summa-ec2` in the Cloudflare dashboard and verify
   that it has no active connectors. Only after verifying this, set
   `SUMMA_CONNECTOR_VERIFIED_CLEAR=yes` in the deploying shell. The script then
   uses your confirmation instead of the API check; it cannot detect connectors
   started elsewhere or automatically confirm the expected tunnel after boot.
   If you intentionally accept the possibility of other active connectors, use
   `SUMMA_ALLOW_UNVERIFIED_CONNECTORS=yes` instead. Traffic could reach a
   different server; verify the public application after deployment.
6. Obtain the **existing tunnel token** from the Cloudflare dashboard using an
   authorized account. This is different from the read-only API token. Store it
   once in the same AWS region, using hidden input:

   ```bash
   python3 scripts/store-tunnel-token.py
   ```

   This creates `/summa/cloudflare/tunnel-token` as a SecureString with the AWS
   managed `aws/ssm` KMS key. It deliberately does not overwrite an existing
   parameter. Startup accepts the token alone or embedded in the Cloudflare
   install command, provided exactly one token is present. The instance role
   can read only this named parameter. With API credentials, deploy checks that
   the stored token identifies the specified account and tunnel.
   Never commit either token or put it in a command line. The parameter is
   preserved when the stack is deleted.
7. Create the reusable ECR repository once, after approval for any AWS charges:

   ```bash
   aws ecr create-repository --repository-name summa --image-scanning-configuration scanOnPush=true
   ```

   If it already exists in this account and region, use the existing one.

## Build and publish the image

From the repository root, run:

```bash
./scripts/publish-image.sh
```

This builds the existing Dockerfile for EC2's `linux/amd64` architecture and
pushes `summa:demo` to the selected account and region. Go compilation and
Python dependency installation happen during this build, not on EC2. Rebuild
and publish after changing application code. Deployment pins the current image
digest; it does not silently replace a running demo when the tag changes.

## Deploy and check

```bash
./scripts/deploy.sh
```

The script checks credentials, the stored token and ECR, and checks tunnel
connectors using the API or your explicit confirmation,
then creates the fixed `summa-demo` CloudFormation stack. EC2 installs Docker,
Compose and cloudflared, starts the image, and signals CloudFormation only
after localhost `/health` responds and cloudflared establishes a connection.
The script checks Systems Manager and, when API credentials are supplied, the
tunnel again before printing the URL.
Sign in through Cloudflare Access to visit `https://app.summabooks.app`.

If the stack already exists, the script checks its health without changing or
updating it. To use a newer image, destroy the demo and deploy again; this
deletes its data. The connector preflight and launch are separate operations,
so coordinate with teammates to avoid launching another connector at the same
time. The script reports unexpected connector counts but cannot prevent an
external operator from starting one later.

## Destroy

```bash
./scripts/destroy.sh
```

Read the account and region shown and type `summa-demo` to confirm. The script
checks the stack's identifying tag, deletes that stack by its unique ID, and
waits for deletion. The Cloudflare configuration, ECR repository and images,
and SSM parameter are separate and remain in place. ECR storage may still incur
charges. Do not use the AWS console to delete other resources as part of this
workflow.

## Troubleshooting

- **Credentials or access denied:** renew SSO with `aws sso login`, confirm the
  account and region with `aws sts get-caller-identity`, and ask your admin for
  the missing permission. Never share tokens in chat.
- **Connector already active:** check the tunnel dashboard and identify its
  owner before stopping anything. A tunnel may show several connections for a
  single connector; the script counts connectors, not individual connections.
- **Stack creation fails:** inspect CloudFormation events. If rollback leaves a
  stack behind, investigate and use `./scripts/destroy.sh` before retrying. The
  instance needs public outbound connectivity to AWS, ECR, GitHub, Cloudflare,
  and package repositories. A failed setup may take longer to roll back.
- **SSM check fails:** ensure the instance can reach Systems Manager endpoints
  and that your AWS identity may run `AWS-RunShellScript`. If it is online,
  start an SSM session with `aws ssm start-session --target INSTANCE_ID` (the
  Session Manager plugin is needed locally). Inspect
  `sudo journalctl -u cloudflared`, `sudo docker ps`,
  `sudo docker compose -f /opt/summa/compose.yaml logs`, and
  `sudo cat /var/log/cloud-init-output.log`. Do not share token files or
  unreviewed logs containing secrets.
- **Cloudflare login or routing fails:** check the existing Access policy,
  hostname and tunnel origin in Cloudflare. A successful local health check
  does not validate a user's Access permissions or browser session.
- **Docker unavailable on WSL:** start Docker Desktop and enable integration
  for the WSL distribution. Docker is needed for image publishing, not for
  subsequent deploy or destroy commands.

The CloudFormation template and scripts have not created any resources until
you run the setup and deployment commands yourself.
