# Guest Example Artifacts

This directory contains the reference guest-side setup for VMRunner-backed CTFs.

Files:

- `vmrunner.service`: systemd unit that runs the flag placement bootstrap.
- `vmrunner.openrc`: OpenRC unit for Alpine-style guests.
- `place_flags.sh`: reads the injected seed, generates HMAC flags, and writes challenge files.

Expected runtime layout inside the guest:

- `vmrunner` installed at `/usr/local/bin/vmrunner`
- `place_flags.sh` installed at `/usr/local/bin/place_flags.sh`
- server seed exposed by QEMU as the `vmrunner` 9p mount tag
- service-mounted seed source at `/mnt/vmrunner/seed`
- private seed copy at `/run/vmrunner/seed`, owned by `vmrunner`
- flag output directory owned by `vmrunner:ctf` at `/var/lib/vmrunner/challenges`
- optional base image asset at `/opt/ctf/abc_base.jpg` (readable by `vmrunner`)

Suggested guest permissions:

```sh
adduser -D -H -s /sbin/nologin vmrunner
addgroup ctf
adduser ctf ctf
adduser vmrunner ctf
install -o vmrunner -g vmrunner -m 0500 vmrunner /usr/local/bin/vmrunner
install -o vmrunner -g vmrunner -m 0500 place_flags.sh /usr/local/bin/place_flags.sh
install -d -o vmrunner -g ctf -m 2750 /var/lib/vmrunner/challenges
```

The service examples mount the QEMU 9p tag automatically:

```sh
mount -t 9p -o trans=virtio,version=9p2000.L vmrunner /mnt/vmrunner
```

To try it manually inside a guest or container:

```sh
export VMRUNNER_BIN=./vmrunner
export SEED_PATH=/tmp/vmrunner-seed
export FLAG_ROOT=/tmp/vmrunner-challenges
sh ./place_flags.sh
```

Generated demo outputs:

- `abc.txt`: `flag{<hmac>_txt}` starts at character 80 by default.
- `abc.jpg`: `FLAG:flag{<hmac>_img}` is appended so `strings abc.jpg` can find it.
