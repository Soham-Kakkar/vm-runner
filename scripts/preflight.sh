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
  systemd_service="$image_root/etc/systemd/system/vmrunner.service"
  openrc_service="$image_root/etc/init.d/vmrunner"

  if [ ! -e "$systemd_service" ] && [ ! -e "$openrc_service" ]; then
    echo "missing vmrunner service in image (systemd or OpenRC)" >&2
    exit 1
  fi

  if [ -e "$openrc_service" ] && [ ! -x "$openrc_service" ]; then
    echo "OpenRC vmrunner service is not executable" >&2
    exit 1
  fi
}

check_loader() {
  if [ ! -e "$image_root/usr/local/bin/place_flags.sh" ]; then
    echo "missing place_flags.sh in image" >&2
    exit 1
  fi
}

check_private_exec() {
  path="$1"
  label="$2"
  mode=$(stat -c '%a' "$path" 2>/dev/null || echo "")
  owner_digit=${mode%??}
  other_digit=${mode#${mode%?}}

  case "$owner_digit" in
    5|7) ;;
    *)
      echo "unexpected $label owner mode: $mode" >&2
      exit 1
      ;;
  esac

  if [ "$other_digit" != "0" ]; then
    echo "$label must not be world-readable/executable: mode $mode" >&2
    exit 1
  fi
}

check_user
check_binary
check_loader
check_service
check_private_exec "$image_root/usr/local/bin/vmrunner" "vmrunner"
check_private_exec "$image_root/usr/local/bin/place_flags.sh" "place_flags.sh"

echo "preflight passed for $image_root"
