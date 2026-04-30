# ISO To Working CTF

This is the full path from an installer ISO to a VMRunner CTF. The qcow2 must
contain the student user, challenge files, `/usr/local/bin/vmrunner`,
`/usr/local/bin/place_flags.sh`, and a boot service. Creating only a `ctf` user
or only copying challenge files is not enough.

Use the Alpine/OpenRC path first. Use the Debian/Ubuntu/systemd path when the
guest is Debian based.

## 1. Build Host Artifacts

Run this on the host from the repository root:

```sh
mkdir -p guest-share
go build -o guest-share/vmrunner ./cmd/vmrunner
```

And ensure the following exist in `guest-share` directory:
```
place_flags.sh
provision_alpine.sh
vmrunner
vmrunner.openrc     # or vmrunner.service for systemd based OSs
```

If the guest later says `vmrunner binary not found or not executable`, this file was not installed into the qcow2, or it was installed with the wrong mode/path.

## 2. Install ISO Into QCOW2

Create the disk and boot the installer:

```sh
qemu-img create -f qcow2 alpine.qcow2 4G  # qemu-img create -f qcow2 <name>.qcow2 <size>
scripts/install-iso-qcow2.sh \
  --iso alpine.iso \
  --disk alpine.qcow2 \
  --size 4G \
  --display serial \    # use vnc for GUI systems

  --memory 1024 \
  --cpus 2 \
  --net user
```

Connect a VNC client to:

```text
127.0.0.1:5909
```

Install the OS to the qcow2 disk, not just to a live environment. Shut down the
guest from inside the installer when the install is done.

For a graphical or Puppy-style ISO, keep using `--display vnc`. For a text
installer with serial console support, `--display serial` is fine.

## 3. Boot Installed Disk With Host Share

Boot the installed qcow2 and expose `guest-share`:

```sh
scripts/install-iso-qcow2.sh \
  --disk alpine.qcow2 \
  --mode boot \
  --display vnc \
  --share guest-share \
  --net user \
  --memory 1024 \
  --cpus 2
```

Inside the guest, log in as root and mount the share:

```sh
mkdir -p /mnt/share
modprobe 9pnet_virtio 2>/dev/null || true
mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/share
ls -l /mnt/share
```

You must see at least:

```sh
vmrunner
place_flags.sh
vmrunner.openrc   # vmrunner.service
provision_alpine.sh
```

## 4. Provision Alpine Guest

For Alpine/OpenRC guests, run:

```sh
sh /mnt/share/provision_alpine.sh
```

This creates:

- user `ctf` with password `ctf`
- system user `vmrunner`
- group `ctf`
- `/usr/local/bin/vmrunner`
- `/usr/local/bin/place_flags.sh`
- `/etc/init.d/vmrunner`
- `/var/lib/vmrunner/challenges`
- serial login on `ttyS0`

Then verify:

```sh
ls -l /usr/local/bin/vmrunner /usr/local/bin/place_flags.sh
rc-update show | grep vmrunner
id ctf
id vmrunner
```

Expected permissions:

```text
/usr/local/bin/vmrunner      mode 0500, owner vmrunner:vmrunner
/usr/local/bin/place_flags.sh mode 0500, owner vmrunner:vmrunner
```

## 5. Edit Challenge Generation In Guest

Edit `/usr/local/bin/place_flags.sh` inside the guest. The script must use the
same question numbers and flag formats that you later enter in the maker UI.

Example:

```sh
#!/usr/bin/env sh
set -eu

VMRUNNER_BIN="${VMRUNNER_BIN:-/usr/local/bin/vmrunner}"
SEED_PATH="${SEED_PATH:-/run/vmrunner/seed}"
FLAG_ROOT="${FLAG_ROOT:-/var/lib/vmrunner/challenges}"
BASE_IMAGE="${BASE_IMAGE:-/opt/ctf/abc_base.jpg}"

seed=$(tr -d '\n\r' < "$SEED_PATH")
umask 027
mkdir -p "$FLAG_ROOT"

txt_flag="$($VMRUNNER_BIN -seed "$seed" -template 'flag{text+<hmac>_txt}' 1)"
img_flag="$($VMRUNNER_BIN -seed "$seed" -template 'flag{lol--<hmac>_img}' 2)"

printf '%s\n' "$txt_flag" > "$FLAG_ROOT/nope.txt"

if [ -f "$BASE_IMAGE" ]; then
  cp "$BASE_IMAGE" "$FLAG_ROOT/yep.jpg"
  chmod u+w "$FLAG_ROOT/yep.jpg"
else
  : > "$FLAG_ROOT/yep.jpg"
fi
printf '%s\n' "$img_flag" >> "$FLAG_ROOT/yep.jpg"

chmod 0640 "$FLAG_ROOT/nope.txt" "$FLAG_ROOT/yep.jpg"
```

Important details:

- If the maker UI says question 1 uses `flag{text+<hmac>_txt}`, the guest script
  must use exactly `flag{text+<hmac>_txt}` for question `1`.
- `flag{text+<hmac>_txt}` and `flag{txt+<hmac>_txt}` are different.
- `printf '%s\n' "$value"` is safer than `echo $value`.
- If you use `$BASE_IMAGE`, define `BASE_IMAGE`, otherwise `set -u` will make
  the script exit before writing files.
- If you set `FLAG_ROOT=/home/ctf`, make it writable by the service user:

```sh
install -d -o ctf -g ctf -m 2775 /home/ctf
adduser vmrunner ctf 2>/dev/null || true
```

The default `/var/lib/vmrunner/challenges` avoids writing into the login user's
home directory and is usually easier to reason about.

## 6. Test Guest Provisioning Before Shutdown

Still inside the booted guest, create a fake seed and run the script manually:

```sh
install -d -o vmrunner -g vmrunner -m 0750 /run/vmrunner
printf '746573742d73656564' > /run/vmrunner/seed
chown vmrunner:vmrunner /run/vmrunner/seed
chmod 0400 /run/vmrunner/seed

su vmrunner -s /bin/sh -c 'SEED_PATH=/run/vmrunner/seed /usr/local/bin/place_flags.sh'
```

For the example above, verify as the `ctf` user:

```sh
su - ctf
```
example:

```sh
cat /var/lib/vmrunner/challenges/nope.txt             # /tmp/vmrunner-challenges/
strings /var/lib/vmrunner/challenges/yep.jpg | tail
```

If this fails now, it will fail in VMRunner too. Fix it before uploading the
qcow2.

## 7. Verify Boot Service

Check that the service is installed and enabled:

```sh
ls -l /etc/init.d/vmrunner
rc-update show | grep vmrunner
rc-service vmrunner status
```

The service mounts the VMRunner runtime share at `/mnt/vmrunner`, copies
`/mnt/vmrunner/seed` to `/run/vmrunner/seed`, then runs `place_flags.sh` as the
restricted `vmrunner` user.

During manual provisioning, `/mnt/vmrunner/seed` will not exist unless the disk
is being launched by the VMRunner server. Use the fake-seed manual test above
for local validation. If you want to test the service itself before upload, make
a temporary runtime seed:

```sh
mkdir -p /mnt/vmrunner
printf '746573742d73656564' > /mnt/vmrunner/seed
/etc/init.d/vmrunner restart
```

## 8. Shut Down And Upload

Shut down the guest cleanly:

```sh
poweroff
```

Start VMRunner:

```sh
go run ./cmd/server
```

Open the maker UI, upload `data/ctfs/my-alpine/base.qcow2`, and create
questions:

| Question | Name | Validation | Flag format |
| --- | --- | --- | --- |
| 1 | text | HMAC | `flag{text+<hmac>_txt}` |
| 2 | img | HMAC | `flag{lol--<hmac>_img}` |
| 3 | flag | Static | `flag_content` |

The maker UI saves a CTF JSON under `data/ctfs/` with a generated ID like
`lab-e-f5QXXy`.

## 9. Launch And Solve

Open the CTF from the dashboard. The solve page shows the VM console, not a
separate file browser. Inside the VM as the `ctf` user, inspect the files you
created:

```sh
find /var/lib/vmrunner/challenges -maxdepth 2 -type f
cat /var/lib/vmrunner/challenges/nope.txt
strings /var/lib/vmrunner/challenges/yep.jpg | tail
```

Submit the discovered flags in the web UI.

## Debian Or Ubuntu Fallback

Use this path for Debian, Ubuntu, or other systemd guests.

Boot the installed disk with the host share:

```sh
scripts/install-iso-qcow2.sh \
  --disk image.qcow2 \
  --mode boot \
  --display vnc \
  --share guest-share \
  --net user
```

Inside the guest as root:

```sh
mkdir -p /mnt/share
modprobe 9pnet_virtio 2>/dev/null || true
mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/share

groupadd -f ctf
id -u ctf >/dev/null 2>&1 || useradd -m -s /bin/bash -g ctf ctf
printf '%s\n' 'ctf:ctf' | chpasswd

id -u vmrunner >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin vmrunner
usermod -aG ctf vmrunner

apt-get update
apt-get install -y binutils

install -o vmrunner -g vmrunner -m 0500 /mnt/share/vmrunner /usr/local/bin/vmrunner
install -o vmrunner -g vmrunner -m 0500 /mnt/share/place_flags.sh /usr/local/bin/place_flags.sh
install -m 0644 /mnt/share/vmrunner.service /etc/systemd/system/vmrunner.service
install -d -o vmrunner -g ctf -m 2750 /var/lib/vmrunner/challenges

systemctl daemon-reload
systemctl enable vmrunner.service
```

For serial login in Debian/Ubuntu guests:

```sh
mkdir -p /etc/systemd/system/serial-getty@ttyS0.service.d
cat >/etc/systemd/system/serial-getty@ttyS0.service.d/autologin.conf <<'EOF'
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin ctf --keep-baud 115200,38400,9600 %I $TERM
EOF
systemctl enable serial-getty@ttyS0.service
```

Then edit `/usr/local/bin/place_flags.sh`, run the fake-seed manual test from
step 6, shut down, upload the qcow2 in the maker UI, and create matching
question definitions.

## Troubleshooting

### `vmrunner binary not found or not executable`

The qcow2 was not provisioned with `/usr/local/bin/vmrunner`, or it is not
executable by the `vmrunner` user. Fix inside the guest:

```sh
mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/share
install -o vmrunner -g vmrunner -m 0500 /mnt/share/vmrunner /usr/local/bin/vmrunner
ls -l /usr/local/bin/vmrunner
```

### Files Do Not Appear In The Solve VM

Check these in order:

```sh
rc-service vmrunner status 2>/dev/null || systemctl status vmrunner.service
ls -l /run/vmrunner/seed
ls -l /usr/local/bin/vmrunner /usr/local/bin/place_flags.sh
find /var/lib/vmrunner/challenges -maxdepth 2 -type f -ls
```

Most causes are:

- the service was never enabled
- `/usr/local/bin/vmrunner` is missing
- `place_flags.sh` exits because `BASE_IMAGE` or another variable is undefined
- `FLAG_ROOT` is not writable by the service user
- the flag format in the maker UI does not exactly match the guest script

### 9p Mount Fails

Make sure the VM was started with `--share guest-share`. Then inside the guest:

```sh
modprobe 9pnet_virtio 2>/dev/null || true
mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/share
```

If the OS does not support virtio 9p, use another copy method during
provisioning, such as `scp`, but keep the VMRunner runtime 9p mount support in
the final image if you need dynamic HMAC flags.
