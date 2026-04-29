#!/usr/bin/env sh
set -eu

VMRUNNER_BIN="${VMRUNNER_BIN:-/usr/local/bin/vmrunner}"
SEED_PATH="${SEED_PATH:-/mnt/vmrunner/seed}"
FLAG_ROOT="${FLAG_ROOT:-/var/lib/vmrunner/challenges}"

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

mkdir -p "$FLAG_ROOT"

txt_flag="$($VMRUNNER_BIN -seed "$seed" -template 'flag{<hmac>_txt}' 1)"
img_flag="$($VMRUNNER_BIN -seed "$seed" -template 'flag{<hmac>_img}' 2)"

txt_file="$FLAG_ROOT/abc.txt"
img_file="$FLAG_ROOT/abc.jpg"
base_image="${BASE_IMAGE:-/opt/ctf/abc_base.jpg}"

printf '%079s%s\n' '' "$txt_flag" > "$txt_file"

if [ -f "$base_image" ]; then
  cp "$base_image" "$img_file"
else
  : > "$img_file"
fi

printf '\nFLAG:%s\n' "$img_flag" >> "$img_file"

chmod 0640 "$txt_file" "$img_file"

echo "wrote $txt_file and $img_file"