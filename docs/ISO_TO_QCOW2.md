# ISO To QCOW2 Workflow

Use this when you want to install any bootable OS ISO into a reusable qcow2
disk image.

## Install An ISO

```sh
scripts/install-iso-qcow2.sh \
  --iso /path/to/installer.iso \
  --disk data/ctfs/my-os/base.qcow2 \
  --size 8G \
  --display serial
```

For graphical installers, use VNC:

```sh
scripts/install-iso-qcow2.sh \
  --iso /path/to/installer.iso \
  --disk data/ctfs/my-os/base.qcow2 \
  --size 16G \
  --display vnc
```

Then connect a VNC client to `127.0.0.1:5909`.

## Boot The Installed Disk

After the installer finishes and shuts down:

```sh
scripts/install-iso-qcow2.sh \
  --disk data/ctfs/my-os/base.qcow2 \
  --mode boot \
  --display serial
```

Use `--display vnc` if the installed OS does not expose a serial console.

## Share Files While Provisioning

To copy VMRunner guest artifacts into a Linux guest, expose a host directory:

```sh
scripts/install-iso-qcow2.sh \
  --disk data/ctfs/my-os/base.qcow2 \
  --mode boot \
  --display serial \
  --share guest-share
```

Inside most Linux guests:

```sh
mkdir -p /mnt/share
mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/share
```

## Use The QCOW2 In VM Runner

In the CTF JSON:

```json
{
  "vm_config": {
    "image_path": "./data/ctfs/my-os/base.qcow2",
    "image_format": "qcow2",
    "memory_mb": 1024,
    "cpus": 1,
    "architecture": "x86_64",
    "timeout_seconds": 1800,
    "display_type": "terminal"
  }
}
```

Use `"display_type": "vnc"` if the guest needs a graphical console.

## Notes

- `--disk-if virtio` is the default because VM Runner boots qcow2 images with
  virtio disks. Most Linux installers support this.
- For maximum installer compatibility, try `--disk-if ide`; but then make sure
  your final VM Runner boot path also supports that guest.
- A live ISO alone is not a reusable CTF image. The reusable artifact is the
  installed qcow2 disk.
- For dynamic HMAC flags, the installed guest still needs the `vmrunner` helper
  and a boot service such as `examples/guest/vmrunner.service` or
  `examples/guest/vmrunner.openrc`.
