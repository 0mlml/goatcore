#!/usr/bin/env bash
# build-image-ec2.sh — bake a goat host AMI for a cloud-resident host site.
#
# THE POINT. A site bringup should be: provision an instance from an image that
# already has everything, then run ansible. Not: provision a bare OS, apt-install
# an engine over the internet, pull two dozen container images over the internet,
# and only then run ansible. The second shape is what makes a bringup take an
# afternoon instead of the hour docs/design/rapid-site-bringup.md commits to, and
# it fails in the exact conditions you build a standby site for — degraded links,
# a rate-limited registry, an upstream that has moved.
#
# So this is the EC2 sibling of build-image-orin.sh / build-image-karbon.sh. It
# produces an AMI carrying:
#   - docker + containerd + compose plugin, with data-root already pointed at
#     /opt/docker-data and the host-wide log bound set
#   - the netbird client
#   - chrony
#   - EVERY container image pinned for the target goatnet's host group,
#     pre-pulled BY DIGEST into the image's docker storage
#
# A node launched from it boots, mounts its data volume, enrols, and is ready
# for ansible. No registry round-trip on the critical path.
#
# WHERE THE IMAGES COME FROM. ops/pins/fleet-pins.yaml is the one home for every
# governed image pin; it renders group_vars/<group>/pins.yml. This script reads
# the RENDERED file, because that is what the host will actually run — reading
# the table instead would let a stale render bake images nobody deploys. Pulls
# go through the OCI substrate mirror when one is reachable
# (docs/design/oci-goat-substrate.md: T1A Cloudflare R2 -> T1B ECR -> T0
# upstream); --registry-mirror points at it, and without one the pull falls
# through to the upstream ref in the pin, which is the honest default rather
# than a hard failure.
#
# Spec: docs/design/rapid-site-bringup.md §4 (pre-baked image)
#       docs/design/zagros-cloud-host-site.md §"Rapid bringup"
#       docs/design/oci-goat-substrate.md §3 (T1 mirrors)
#
# Usage:
#   ops/bootstrap/build-image-ec2.sh --role core --group zagros_cloud_hosts \
#       [--region eu-central-1] [--tag auto] [--registry-mirror <url>] \
#       [--builder-type c7i.xlarge] [--keep-builder]
#
# Output: an AMI tagged GoatImageClass=<role>, which terraform-zagros's host-node
# module picks up automatically as the newest self-owned bake for that role.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

ROLE=""
GROUP=""
REGION="eu-central-1"
TAG="auto"
REGISTRY_MIRROR=""
BUILDER_TYPE="c7i.xlarge"
KEEP_BUILDER=0
AWS_PROFILE_NAME="usareur-af"

usage() {
  sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --role)             ROLE=$2; shift 2;;
    --group)            GROUP=$2; shift 2;;
    --region)           REGION=$2; shift 2;;
    --tag)              TAG=$2; shift 2;;
    --registry-mirror)  REGISTRY_MIRROR=$2; shift 2;;
    --builder-type)     BUILDER_TYPE=$2; shift 2;;
    --profile)          AWS_PROFILE_NAME=$2; shift 2;;
    --keep-builder)     KEEP_BUILDER=1; shift;;
    -h|--help)          usage;;
    *) echo "unknown arg: $1" >&2; usage;;
  esac
done

[[ -n "$ROLE" && -n "$GROUP" ]] || usage
case "$ROLE" in core|obs) ;; *) echo "--role must be core or obs" >&2; exit 64;; esac

PINS="$REPO_ROOT/ops/ansible/group_vars/$GROUP/pins.yml"
[[ -f "$PINS" ]] || { echo "no rendered pins at $PINS — run ops/pins/generate-pins.py first" >&2; exit 65; }

if [[ "$TAG" == "auto" ]]; then
  TAG="goat-$ROLE-$(date -u +%Y%m%d-%H%M)"
fi

aws_() { aws-vault exec "$AWS_PROFILE_NAME" --no-session -- aws "$@"; }

# ---------------------------------------------------------------------------
# 1. Resolve the image list from the RENDERED pins.
#
# Every governed pin is a `<something>_image: "<ref>"` key. `ghcr_org` is
# templated per-goatnet (reference: one pin, two namespaces, resolved from the
# checkout's origin) so it is resolved here from the group's vars rather than
# guessed — an unresolved `{{ ghcr_org }}` would bake nothing and the AMI would
# look fine until a node tried to start a container.
# ---------------------------------------------------------------------------
GHCR_ORG="$(grep -rhoE '^ghcr_org:[[:space:]]*"?([A-Za-z0-9._/-]+)"?' \
              "$REPO_ROOT/ops/ansible/group_vars/$GROUP/" \
              "$REPO_ROOT/ops/ansible/group_vars/all.yml" 2>/dev/null \
            | head -1 | sed -E 's/^ghcr_org:[[:space:]]*"?//; s/"?$//')"
GHCR_ORG="${GHCR_ORG:-goat-maintainer}"

# while-read, not mapfile: operator-run on macOS /bin/bash 3.2 (#227).
IMAGES=()
while IFS= read -r img || [ -n "$img" ]; do
  IMAGES+=("$img")
done < <(
  grep -E '^[a-z0-9_]+_image:[[:space:]]*"' "$PINS" \
    | sed -E 's/^[a-z0-9_]+_image:[[:space:]]*"//; s/"[[:space:]]*$//' \
    | sed "s|{{ ghcr_org }}|$GHCR_ORG|g" \
    | grep -v '{{' \
    | sort -u
)

if [[ ${#IMAGES[@]} -eq 0 ]]; then
  echo "resolved zero images from $PINS — refusing to bake an empty image" >&2
  exit 66
fi

echo "bake plan"
echo "  role       : $ROLE"
echo "  group      : $GROUP"
echo "  region     : $REGION"
echo "  tag        : $TAG"
echo "  ghcr_org   : $GHCR_ORG"
echo "  mirror     : ${REGISTRY_MIRROR:-<none — pulling upstream refs>}"
echo "  images     : ${#IMAGES[@]}"
printf '    %s\n' "${IMAGES[@]}"
echo

# ---------------------------------------------------------------------------
# 2. Builder user-data. Installs the engine, pre-pulls every pinned image, then
#    writes a completion marker the poller below waits on. It deliberately does
#    NOT enrol on any mesh: an AMI carrying a live peer identity would clone
#    that identity onto every node launched from it.
# ---------------------------------------------------------------------------
MIRROR_JSON=""
if [[ -n "$REGISTRY_MIRROR" ]]; then
  MIRROR_JSON="\"registry-mirrors\": [\"$REGISTRY_MIRROR\"],"
fi

USER_DATA=$(cat <<EOF
#!/bin/bash
set -euo pipefail
exec > >(tee /var/log/goat-image-bake.log) 2>&1
echo "[\$(date -Is)] bake $TAG starting"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq ca-certificates curl gnupg jq nvme-cli chrony

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg
echo "deb [arch=\$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \$(. /etc/os-release && echo \$VERSION_CODENAME) stable" \
  > /etc/apt/sources.list.d/docker.list
apt-get update -qq
apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin

# data-root + log bound baked in, so first boot does not have to restart the
# engine to get them.
mkdir -p /opt/docker-data/docker /opt/docker-data/containerd
cat > /etc/docker/daemon.json <<'JSON'
{
  "data-root": "/opt/docker-data/docker",
  $MIRROR_JSON
  "log-driver": "json-file",
  "log-opts": { "max-size": "50m", "max-file": "5" }
}
JSON
containerd config default > /etc/containerd/config.toml
sed -i 's#^root = .*#root = "/opt/docker-data/containerd"#' /etc/containerd/config.toml
systemctl restart containerd
systemctl enable --now docker

curl -fsSL https://pkgs.netbird.io/debian/public.key | gpg --dearmor -o /usr/share/keyrings/netbird-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/netbird-archive-keyring.gpg] https://pkgs.netbird.io/debian stable main" \
  > /etc/apt/sources.list.d/netbird.list
apt-get update -qq
apt-get install -y -qq netbird
systemctl stop netbird || true
systemctl disable netbird || true

# ---- pre-pull every pinned image ----
FAILED=0
$(for img in "${IMAGES[@]}"; do
    printf 'echo "[\$(date -Is)] pulling %s"; docker pull -q "%s" || { echo "PULL-FAILED %s"; FAILED=1; }\n' "$img" "$img" "$img"
  done)

docker images --format '{{.Repository}}:{{.Tag}} {{.Size}}' | sort > /var/log/goat-image-manifest.txt

# ---- de-identify so the AMI is a template, not a clone of this box ----
systemctl stop docker || true
rm -rf /var/lib/netbird/* /etc/netbird/* 2>/dev/null || true
truncate -s 0 /etc/machine-id
rm -f /var/lib/dbus/machine-id
rm -f /etc/ssh/ssh_host_*
cloud-init clean --logs || true

echo "\$FAILED" > /var/log/goat-image-bake.failed
echo "[\$(date -Is)] bake complete (failed=\$FAILED)"
EOF
)

# ---------------------------------------------------------------------------
# 3. Launch the builder.
# ---------------------------------------------------------------------------
# The builder must be SSM-reachable — that is how the poller below reads the
# completion marker, and it is the same port-22-closed posture as every other
# cloud host in this fleet. Created idempotently rather than assumed: a bake
# script that requires a hand-made IAM role is not a turnkey bake script.
BUILDER_PROFILE="goat-image-builder"
if ! aws_ iam get-instance-profile --instance-profile-name "$BUILDER_PROFILE" >/dev/null 2>&1; then
  echo "creating IAM role + instance profile $BUILDER_PROFILE (one-time)"
  aws_ iam create-role --role-name "$BUILDER_PROFILE" \
    --assume-role-policy-document '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}' >/dev/null
  aws_ iam attach-role-policy --role-name "$BUILDER_PROFILE" \
    --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore >/dev/null
  aws_ iam create-instance-profile --instance-profile-name "$BUILDER_PROFILE" >/dev/null
  aws_ iam add-role-to-instance-profile --instance-profile-name "$BUILDER_PROFILE" \
    --role-name "$BUILDER_PROFILE" >/dev/null
  echo "waiting for IAM propagation…"; sleep 20
fi

UBUNTU_AMI=$(aws_ ec2 describe-images --region "$REGION" --owners 099720109477 \
  --filters 'Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*' \
            'Name=state,Values=available' \
  --query 'sort_by(Images,&CreationDate)[-1].ImageId' --output text)
echo "builder base AMI: $UBUNTU_AMI"

BUILDER_ID=$(aws_ ec2 run-instances --region "$REGION" \
  --image-id "$UBUNTU_AMI" --instance-type "$BUILDER_TYPE" \
  --iam-instance-profile "Name=$BUILDER_PROFILE" \
  --block-device-mappings 'DeviceName=/dev/sda1,Ebs={VolumeSize=60,VolumeType=gp3,Encrypted=true}' \
  --user-data "$USER_DATA" \
  --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=goat-image-builder-$TAG},{Key=Lifecycle,Value=ephemeral}]" \
  --query 'Instances[0].InstanceId' --output text)
echo "builder: $BUILDER_ID"

cleanup() {
  if [[ "$KEEP_BUILDER" -eq 0 && -n "${BUILDER_ID:-}" ]]; then
    echo "terminating builder $BUILDER_ID"
    aws_ ec2 terminate-instances --region "$REGION" --instance-ids "$BUILDER_ID" >/dev/null || true
  fi
}
trap cleanup EXIT

echo "waiting for bake to finish (this is the slow part — it is paid ONCE per"
echo "image release, not once per site bringup, which is the whole point)…"
DEADLINE=$(( $(date +%s) + 2400 ))
while :; do
  [[ $(date +%s) -lt $DEADLINE ]] || { echo "bake timed out after 40 min" >&2; exit 75; }
  sleep 30
  MARKER=$(aws_ ssm send-command --region "$REGION" --instance-ids "$BUILDER_ID" \
      --document-name AWS-RunShellScript \
      --parameters 'commands=["cat /var/log/goat-image-bake.failed 2>/dev/null || echo PENDING"]' \
      --query 'Command.CommandId' --output text 2>/dev/null) || continue
  sleep 6
  OUT=$(aws_ ssm get-command-invocation --region "$REGION" --command-id "$MARKER" \
        --instance-id "$BUILDER_ID" --query 'StandardOutputContent' --output text 2>/dev/null) || continue
  case "$(echo "$OUT" | tr -d '[:space:]')" in
    0) echo "bake OK"; break;;
    1) echo "bake finished but at least one image FAILED to pull — see /var/log/goat-image-bake.log on $BUILDER_ID" >&2
       echo "refusing to publish a half-populated AMI: a node launched from it would look healthy and pull the rest at boot, silently on the slow path" >&2
       KEEP_BUILDER=1; exit 76;;
    *) echo "  … still baking";;
  esac
done

# ---------------------------------------------------------------------------
# 4. Snapshot to an AMI. GoatImageClass is what the terraform module filters on.
# ---------------------------------------------------------------------------
aws_ ec2 stop-instances --region "$REGION" --instance-ids "$BUILDER_ID" >/dev/null
aws_ ec2 wait instance-stopped --region "$REGION" --instance-ids "$BUILDER_ID"

AMI_ID=$(aws_ ec2 create-image --region "$REGION" --instance-id "$BUILDER_ID" \
  --name "$TAG" \
  --description "goat host bake: role=$ROLE group=$GROUP images=${#IMAGES[@]}" \
  --tag-specifications "ResourceType=image,Tags=[{Key=Name,Value=$TAG},{Key=GoatImageClass,Value=$ROLE},{Key=GoatImageGroup,Value=$GROUP},{Key=GoatImageTag,Value=$TAG}]" \
  --query 'ImageId' --output text)

echo "creating AMI $AMI_ID …"
aws_ ec2 wait image-available --region "$REGION" --image-ids "$AMI_ID"

cat <<EOF

AMI published
  id     : $AMI_ID
  name   : $TAG
  class  : GoatImageClass=$ROLE   (terraform-zagros/modules/host-node picks the newest of these automatically)
  images : ${#IMAGES[@]} pre-pulled by digest

Next: terraform apply in terraform-zagros/ — nodes of role '$ROLE' will launch from
this bake, and their module output image_path will read 'prebaked'. If it reads
'stock-ubuntu-slow-path', this AMI was not found and the bringup is on the slow
path; check the region and the GoatImageClass tag before blaming the network.
EOF
