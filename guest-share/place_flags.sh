#!/usr/bin/env sh
set -eu

VMRUNNER_BIN="${VMRUNNER_BIN:-/usr/local/bin/vmrunner}"
SEED_PATH="${SEED_PATH:-/run/vmrunner/seed}"
FLAG_ROOT="${FLAG_ROOT:-/home/ctf}"
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

umask 027
mkdir -p "$FLAG_ROOT"

txt_flag="$($VMRUNNER_BIN -seed "$seed" -template 'flag{txt+<hmac>_txt}' 1)"
img_flag="$($VMRUNNER_BIN -seed "$seed" -template 'flag{lol--<hmac>_img}' 2)"

txt_file="$FLAG_ROOT/nope.txt"
img_file="$FLAG_ROOT/yep.jpg"

printf '%s\n' "$txt_flag" > "$txt_file"

: > "$img_file"

printf '%s\n' "$img_flag" >> "$img_file"

chmod 0640 "$txt_file" "$img_file"

echo "wrote $txt_file and $img_file"
