#!/usr/bin/env sh
set -eu

image_root="${1:-}"

if [ -z "$image_root" ]; then
  echo "usage: $0 <mounted-image-root>" >&2
  exit 1
fi

check_user() {
  if ! grep -q '^vmrunner:' "$image_root/etc/passwd" 2>/dev/null; then
    echo "missing vmrunner user in $image_root/etc/passwd" >&2
    exit 1
  fi
}

check_binary() {
  if [ ! -e "$image_root/usr/local/bin/vmrunner" ]; then
    echo "missing vmrunner binary in image" >&2
    exit 1
  fi
}

check_service() {
  if [ ! -e "$image_root/etc/systemd/system/vmrunner.service" ]; then
    echo "missing vmrunner.service in image" >&2
    exit 1
  fi
}

check_perms() {
  mode=$(stat -c '%a' "$image_root/usr/local/bin/vmrunner" 2>/dev/null || echo "")
  if [ "$mode" != "500" ] && [ "$mode" != "550" ] && [ "$mode" != "700" ]; then
    echo "unexpected vmrunner mode: $mode" >&2
    exit 1
  fi
}

check_user
check_binary
check_service
check_perms

echo "preflight passed for $image_root"