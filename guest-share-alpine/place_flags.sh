#!/usr/bin/env sh
set -eu

VMRUNNER_BIN="${VMRUNNER_BIN:-/usr/local/bin/vmrunner}"
SEED_PATH="${SEED_PATH:-/run/vmrunner/seed}"
FLAG_ROOT="${FLAG_ROOT:-/var/lib/vmrunner/challenges}"
BASE_IMAGE="${BASE_IMAGE:-/opt/ctf/abc_base.jpg}"
TEXT_FLAG_OFFSET="${TEXT_FLAG_OFFSET:-80}"

if [ ! -x "$VMRUNNER_BIN" ]; then
  echo "vmrunner binary not found or not executable: $VMRUNNER_BIN" >&2
  exit 1
fi

if [ ! -f "$SEED_PATH" ]; then
  echo "seed file not found: $SEED_PATH" >&2
  exit 1
fi

seed=$(tr -d '\n\r' < "$SEED_PATH")

if [ -z "$seed" ]; then
  echo "seed file is empty: $SEED_PATH" >&2
  exit 1
fi

if [ "$TEXT_FLAG_OFFSET" -lt 1 ]; then
  echo "TEXT_FLAG_OFFSET must be >= 1" >&2
  exit 1
fi

umask 027
mkdir -p "$FLAG_ROOT"

txt_flag="$($VMRUNNER_BIN -seed "$seed" -template 'flag{<hmac>_txt}' 1)"
img_flag="$($VMRUNNER_BIN -seed "$seed" -template 'flag{<hmac>_img}' 2)"

txt_file="$FLAG_ROOT/abc.txt"
img_file="$FLAG_ROOT/abc.jpg"
padding_count=$((TEXT_FLAG_OFFSET - 1))

printf "%${padding_count}s%s\n" '' "$txt_flag" > "$txt_file"

if [ -f "$BASE_IMAGE" ]; then
  cp "$BASE_IMAGE" "$img_file"
  chmod u+w "$img_file"
else
  : > "$img_file"
fi

printf '\nFLAG:%s\n' "$img_flag" >> "$img_file"

chmod 0640 "$txt_file" "$img_file"

echo "wrote $txt_file and $img_file"
