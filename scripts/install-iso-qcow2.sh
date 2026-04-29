#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage:
  scripts/install-iso-qcow2.sh --iso ISO --disk DISK [options]
  scripts/install-iso-qcow2.sh --disk DISK --mode boot [options]

Create and boot a qcow2 disk for installing an arbitrary OS ISO.

Required:
  --disk PATH             qcow2 disk path
  --iso PATH              installer ISO path, required for --mode install

Options:
  --mode install|boot     install boots ISO + disk; boot boots disk only (default: install)
  --size SIZE             qcow2 size when creating a new disk (default: 8G)
  --memory MB             VM memory in MB (default: 2048)
  --cpus N                VM CPU count (default: 2)
  --display serial|vnc|gtk
                          serial is best for server/text installers,
                          vnc is best for graphical installers,
                          gtk opens a local QEMU window (default: serial)
  --vnc-display N         VNC display number, port is 5900+N (default: 9)
  --disk-if virtio|ide|sata
                          virtio matches VM Runner's QEMU path for Linux guests;
                          ide is broad compatibility (default: virtio)
  --share PATH            expose host path as 9p mount tag "share"
  --net user|none         user NAT or no network (default: user)
  --qemu PATH             QEMU binary (default: /usr/bin/qemu-system-x86_64)
  --no-kvm                do not use KVM acceleration
  --recreate              delete and recreate DISK before booting installer
  --dry-run               print command instead of running
  -h, --help              show this help

Examples:
  scripts/install-iso-qcow2.sh --iso alpine.iso --disk data/ctfs/alpine/base.qcow2 --size 4G --display serial
  scripts/install-iso-qcow2.sh --iso ubuntu.iso --disk data/ctfs/ubuntu/base.qcow2 --display vnc
  scripts/install-iso-qcow2.sh --disk data/ctfs/alpine/base.qcow2 --mode boot --display serial

Inside Linux guests, a --share PATH mount can usually be mounted with:
  mount -t 9p -o trans=virtio,version=9p2000.L share /mnt/share
EOF
}

mode="install"
iso=""
disk=""
size="8G"
memory="2048"
cpus="2"
display="serial"
vnc_display="9"
disk_if="virtio"
share=""
net="user"
qemu="/usr/bin/qemu-system-x86_64"
use_kvm="1"
recreate="0"
dry_run="0"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --mode) mode="${2:?}"; shift 2 ;;
    --iso) iso="${2:?}"; shift 2 ;;
    --disk) disk="${2:?}"; shift 2 ;;
    --size) size="${2:?}"; shift 2 ;;
    --memory) memory="${2:?}"; shift 2 ;;
    --cpus) cpus="${2:?}"; shift 2 ;;
    --display) display="${2:?}"; shift 2 ;;
    --vnc-display) vnc_display="${2:?}"; shift 2 ;;
    --disk-if) disk_if="${2:?}"; shift 2 ;;
    --share) share="${2:?}"; shift 2 ;;
    --net) net="${2:?}"; shift 2 ;;
    --qemu) qemu="${2:?}"; shift 2 ;;
    --no-kvm) use_kvm="0"; shift ;;
    --recreate) recreate="1"; shift ;;
    --dry-run) dry_run="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

case "$mode" in install|boot) ;; *) echo "--mode must be install or boot" >&2; exit 1 ;; esac
case "$display" in serial|vnc|gtk) ;; *) echo "--display must be serial, vnc, or gtk" >&2; exit 1 ;; esac
case "$disk_if" in virtio|ide|sata) ;; *) echo "--disk-if must be virtio, ide, or sata" >&2; exit 1 ;; esac
case "$net" in user|none) ;; *) echo "--net must be user or none" >&2; exit 1 ;; esac

if [ -z "$disk" ]; then
  echo "--disk is required" >&2
  usage >&2
  exit 1
fi
if [ "$mode" = "install" ] && [ -z "$iso" ]; then
  echo "--iso is required in install mode" >&2
  usage >&2
  exit 1
fi
if [ "$mode" = "install" ] && [ ! -f "$iso" ]; then
  echo "ISO not found: $iso" >&2
  exit 1
fi
if [ ! -x "$qemu" ]; then
  echo "QEMU binary not executable: $qemu" >&2
  exit 1
fi

mkdir -p "$(dirname "$disk")"
if [ "$mode" = "install" ]; then
  if [ "$recreate" = "1" ] && [ -e "$disk" ]; then
    rm -f "$disk"
  fi
  if [ ! -e "$disk" ]; then
    qemu-img create -f qcow2 "$disk" "$size"
  fi
fi

args=("$qemu" "-m" "$memory" "-smp" "$cpus")
if [ "$use_kvm" = "1" ] && [ -e /dev/kvm ]; then
  args+=("-enable-kvm" "-cpu" "host")
fi

case "$display" in
  serial)
    args+=("-nographic" "-serial" "mon:stdio")
    ;;
  vnc)
    args+=("-display" "none" "-vnc" "127.0.0.1:$vnc_display")
    echo "VNC display enabled: connect to 127.0.0.1:$((5900 + vnc_display))" >&2
    ;;
  gtk)
    args+=("-display" "gtk")
    ;;
esac

args+=("-drive" "file=$disk,format=qcow2,if=$disk_if")

if [ "$mode" = "install" ]; then
  args+=("-cdrom" "$iso" "-boot" "order=d,once=d")
else
  args+=("-boot" "c")
fi

if [ -n "$share" ]; then
  mkdir -p "$share"
  args+=("-virtfs" "local,path=$share,mount_tag=share,security_model=none,id=share0")
fi

if [ "$net" = "user" ]; then
  args+=("-netdev" "user,id=n0" "-device" "e1000,netdev=n0")
else
  args+=("-net" "none")
fi

printf 'Running:'
printf ' %q' "${args[@]}"
printf '\n'

if [ "$display" = "serial" ]; then
  echo "QEMU serial controls: Ctrl-a x quits; Ctrl-a h shows help." >&2
fi

if [ "$dry_run" = "1" ]; then
  exit 0
fi

exec "${args[@]}"
