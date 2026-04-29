#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="$ROOT/build"
CTF_DIR="$ROOT/data/ctfs/demo-ctf"
CLOUD_IMG="$BUILD_DIR/noble.img"
OUTPUT="$CTF_DIR/base.qcow2"
TMP_OUTPUT="$CTF_DIR/base.qcow2.tmp"
VMRUNNER_BIN="$BUILD_DIR/vmrunner"
UBUNTU_CLOUD_URL="${UBUNTU_CLOUD_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_cmd go
require_cmd qemu-img
require_cmd virt-customize
require_cmd wget

mkdir -p "$BUILD_DIR" "$CTF_DIR"

if [ ! -s "$CLOUD_IMG" ]; then
  echo "downloading Ubuntu cloud image..."
  wget -O "$CLOUD_IMG" "$UBUNTU_CLOUD_URL"
fi

echo "building guest vmrunner helper..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$VMRUNNER_BIN" "$ROOT/cmd/vmrunner"

echo "creating fresh qcow2 at $TMP_OUTPUT..."
rm -f "$TMP_OUTPUT"
qemu-img convert -O qcow2 "$CLOUD_IMG" "$TMP_OUTPUT"
qemu-img resize "$TMP_OUTPUT" 8G

customize=(virt-customize)
if [ ! -r "/boot/vmlinuz-$(uname -r)" ] || [ ! -r "/boot/initrd.img-$(uname -r)" ]; then
  if command -v sudo >/dev/null 2>&1; then
    echo "host kernel/initrd are not readable by this user; using sudo for virt-customize..."
    customize=(sudo env LIBGUESTFS_BACKEND=direct virt-customize)
  else
    echo "host kernel/initrd are not readable and sudo is unavailable" >&2
    echo "try: sudo chmod 0644 /boot/vmlinuz-$(uname -r) /boot/initrd.img-$(uname -r)" >&2
    exit 1
  fi
else
  export LIBGUESTFS_BACKEND=direct
fi

echo "customizing guest image..."
"${customize[@]}" -a "$TMP_OUTPUT" \
  --run-command 'groupadd -f ctf' \
  --run-command 'id -u vmrunner >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin vmrunner' \
  --run-command 'id -u ctf >/dev/null 2>&1 || useradd -m -s /bin/bash -g ctf ctf' \
  --run-command 'usermod -aG ctf vmrunner' \
  --run-command 'printf "%s\n" "ctf:ctf" | /usr/sbin/chpasswd' \
  --install binutils \
  --copy-in "$VMRUNNER_BIN:/usr/local/bin" \
  --copy-in "$ROOT/examples/guest/place_flags.sh:/usr/local/bin" \
  --copy-in "$ROOT/examples/guest/vmrunner.service:/etc/systemd/system" \
  --run-command 'chown vmrunner:vmrunner /usr/local/bin/vmrunner /usr/local/bin/place_flags.sh' \
  --run-command 'chmod 0500 /usr/local/bin/vmrunner /usr/local/bin/place_flags.sh' \
  --run-command 'install -d -o vmrunner -g ctf -m 2750 /var/lib/vmrunner/challenges' \
  --run-command 'mkdir -p /opt/ctf && printf "fake jpg base\n" > /opt/ctf/abc_base.jpg && chmod 0444 /opt/ctf/abc_base.jpg' \
  --run-command 'touch /etc/cloud/cloud-init.disabled' \
  --run-command 'systemctl disable systemd-networkd-wait-online.service || true' \
  --run-command 'mkdir -p /etc/systemd/system/serial-getty@ttyS0.service.d' \
  --run-command 'printf "%s\n" "[Service]" "ExecStart=" "ExecStart=-/sbin/agetty --autologin ctf --keep-baud 115200,38400,9600 %I \$TERM" > /etc/systemd/system/serial-getty@ttyS0.service.d/autologin.conf' \
  --run-command 'systemctl enable vmrunner.service' \
  --run-command 'systemctl enable serial-getty@ttyS0.service'

if [ "$(id -u)" -ne 0 ] && [ -f "$TMP_OUTPUT" ]; then
  sudo chown "$(id -u):$(id -g)" "$TMP_OUTPUT" 2>/dev/null || true
fi

mv "$TMP_OUTPUT" "$OUTPUT"
echo "wrote $OUTPUT"
