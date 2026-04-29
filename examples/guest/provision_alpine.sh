#!/bin/sh
set -eu

SHARE_DIR="${SHARE_DIR:-/mnt/share}"
FLAG_ROOT="${FLAG_ROOT:-/var/lib/vmrunner/challenges}"
BASE_IMAGE="${BASE_IMAGE:-/opt/ctf/abc_base.jpg}"

if [ "$(id -u)" != "0" ]; then
  echo "run as root inside the Alpine guest" >&2
  exit 1
fi

for file in vmrunner place_flags.sh vmrunner.openrc; do
  if [ ! -e "$SHARE_DIR/$file" ]; then
    echo "missing $SHARE_DIR/$file" >&2
    echo "mount the host share first: mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/share" >&2
    exit 1
  fi
done

addgroup -S ctf 2>/dev/null || true
if ! id ctf >/dev/null 2>&1; then
  adduser -D -G ctf -s /bin/sh ctf
fi
echo 'ctf:ctf' | chpasswd

addgroup -S vmrunner 2>/dev/null || true
if ! id vmrunner >/dev/null 2>&1; then
  adduser -S -D -H -s /sbin/nologin -G vmrunner vmrunner
fi
adduser vmrunner ctf 2>/dev/null || true

if ! command -v strings >/dev/null 2>&1; then
  apk update
  apk add binutils
fi

install -o vmrunner -g vmrunner -m 0500 "$SHARE_DIR/vmrunner" /usr/local/bin/vmrunner
install -o vmrunner -g vmrunner -m 0500 "$SHARE_DIR/place_flags.sh" /usr/local/bin/place_flags.sh
install -m 0755 "$SHARE_DIR/vmrunner.openrc" /etc/init.d/vmrunner

install -d -o vmrunner -g ctf -m 2750 "$FLAG_ROOT"
mkdir -p "$(dirname "$BASE_IMAGE")"
printf 'fake jpg base\n' > "$BASE_IMAGE"
chmod 0644 "$BASE_IMAGE"

rc-update add vmrunner default

if ! grep -q 'ttyS0::respawn:/sbin/getty' /etc/inittab; then
  echo 'ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100' >> /etc/inittab
fi

echo "provisioned Alpine guest:"
echo "  ctf user/password: ctf / ctf"
echo "  vmrunner service:  /etc/init.d/vmrunner"
echo "  challenge root:    $FLAG_ROOT"
