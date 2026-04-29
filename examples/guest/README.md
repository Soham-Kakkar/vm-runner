# Guest Example Artifacts

This directory contains the reference guest-side setup for VMRunner-backed CTFs.

Files:

- `vmrunner.service`: systemd unit that runs the flag placement bootstrap.
- `place_flags.sh`: reads the injected seed, generates HMAC flags, and writes challenge files.

Expected runtime layout inside the guest:

- `vmrunner` installed at `/usr/local/bin/vmrunner`
- seed mounted or copied to `/mnt/vmrunner/seed`
- flag output directory owned by `vmrunner` at `/var/lib/vmrunner/challenges`
- optional base image asset at `/opt/ctf/abc_base.jpg` (readable by `vmrunner`)

To try it manually inside a guest or container:

```sh
export VMRUNNER_BIN=/usr/local/bin/vmrunner
export SEED_PATH=/mnt/vmrunner/seed
export FLAG_ROOT=/var/lib/vmrunner/challenges
sh ./place_flags.sh
```