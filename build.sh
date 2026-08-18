#!/usr/bin/env bash
# Build RSTS/E V7.2-10 for any platform and architecture the Go toolchain
# supports.
#
# CGO is off for every target, so the binary carries its own runtime and
# resolver and needs no shared libraries at run time. (macOS is the one
# exception the Go toolchain imposes: darwin binaries always link the
# system libSystem.dylib, which ships with every Mac.)
set -euo pipefail

cd "$(dirname "$0")"

OUT_DIR=bin
PKG=./cmd/rsts
OSES=""
ARCHES=""
PAIRS=""
ALL=0
EVERY=0

# The platforms people actually ask for, built by --all. Both Macs are
# here: Intel and Apple silicon.
ALL_TARGETS=(
	linux/amd64
	linux/arm64
	linux/386
	linux/arm
	windows/amd64
	windows/arm64
	windows/386
	darwin/amd64
	darwin/arm64
	freebsd/amd64
	freebsd/arm64
)

usage() {
	cat <<'EOF'
Usage: ./build.sh [platform] [architecture] [os/arch] ...

Platform:  (default: this machine)
  --linux                   Linux
  --windows, --win          Windows
  --mac, --macos, --darwin  macOS
  --freebsd                 FreeBSD
  --openbsd  --netbsd  --solaris  --plan9  --aix  --illumos  --dragonfly
  --os NAME                 any GOOS

Architecture:
  --amd64, --x64, --x86-64  64-bit Intel/AMD (this is an Intel Mac)
  --arm64, --aarch64        64-bit ARM (this is an Apple silicon Mac)
  --x86, --386              32-bit Intel
  --arm                     32-bit ARM (ARMv7)
  --riscv64  --ppc64le  --ppc64  --s390x  --mips64le  --mips64
  --mipsle  --mips  --loong64  --wasm
  --arch NAME               any GOARCH

Any GOOS/GOARCH the toolchain knows can also be given directly as a
pair, which is the way to reach anything without a flag of its own:

  ./build.sh linux/riscv64 openbsd/arm64 js/wasm

Other:
  --all                     the common platforms, including both Macs
  --everything              every pair "go tool dist list" reports;
                            ones this program cannot build are reported
                            and skipped rather than stopping the run
  --out DIR                 output directory (default: bin)
  --list                    list every GOOS/GOARCH the toolchain supports
  -h, --help                this text

Platform and architecture flags accumulate into a matrix, so two of each
builds four binaries. With nothing named at all you get a binary for the
machine you are on. With a platform but no architecture you get this
machine's architecture when building for this machine, and the
platform's usual architectures when cross compiling, so --mac gives you
both an Intel and an Apple silicon binary.

Binaries are named rsts-PLATFORM-ARCHITECTURE (with .exe on Windows).
A build for this machine is also copied to bin/rsts.

Examples:
  ./build.sh                       this machine
  ./build.sh --mac                 Intel and Apple silicon
  ./build.sh --mac --amd64         Intel Macs only
  ./build.sh --mac --arm64         Apple silicon only
  ./build.sh --linux --windows --amd64 --arm64    four binaries
  ./build.sh linux/riscv64
  ./build.sh --all
  ./build.sh --everything
EOF
}

add_os() {
	case " $OSES " in *" $1 "*) return 0 ;; esac
	OSES="$OSES $1"
}

add_arch() {
	case " $ARCHES " in *" $1 "*) return 0 ;; esac
	ARCHES="$ARCHES $1"
}

add_pair() {
	case " $PAIRS " in *" $1 "*) return 0 ;; esac
	PAIRS="$PAIRS $1"
}

need_value() {
	if [[ -z ${2:-} ]]; then
		echo "?$1 needs a value" >&2
		exit 2
	fi
}

# Architectures to build when a platform is named but no architecture is.
# Cross compiling for a platform should cover the machines people run it
# on, which for macOS means Intel as well as Apple silicon.
common_arches() {
	case "$1" in
	darwin) echo "amd64 arm64" ;;
	windows) echo "amd64 arm64" ;;
	linux) echo "amd64 arm64" ;;
	freebsd | openbsd | netbsd) echo "amd64 arm64" ;;
	*) echo "amd64" ;;
	esac
}

# A short note for the platforms whose name does not make the machine
# obvious.
describe() {
	case "$1" in
	darwin/amd64) echo "Intel Mac" ;;
	darwin/arm64) echo "Apple silicon" ;;
	windows/386) echo "32-bit Windows" ;;
	linux/arm) echo "ARMv7, Raspberry Pi" ;;
	*) echo "" ;;
	esac
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--linux) add_os linux ;;
	--windows | --win) add_os windows ;;
	--mac | --macos | --darwin | --osx) add_os darwin ;;
	--freebsd) add_os freebsd ;;
	--openbsd) add_os openbsd ;;
	--netbsd) add_os netbsd ;;
	--dragonfly) add_os dragonfly ;;
	--solaris) add_os solaris ;;
	--illumos) add_os illumos ;;
	--aix) add_os aix ;;
	--plan9) add_os plan9 ;;
	--android) add_os android ;;
	--ios) add_os ios ;;
	--js) add_os js ;;
	--wasip1) add_os wasip1 ;;
	--os)
		need_value "$1" "${2:-}"
		add_os "$2"
		shift
		;;
	--amd64 | --x64 | --x86-64 | --x86_64) add_arch amd64 ;;
	--x86 | --386 | --i386) add_arch 386 ;;
	--arm64 | --aarch64) add_arch arm64 ;;
	--arm) add_arch arm ;;
	--riscv64) add_arch riscv64 ;;
	--ppc64le) add_arch ppc64le ;;
	--ppc64) add_arch ppc64 ;;
	--s390x) add_arch s390x ;;
	--mips64le) add_arch mips64le ;;
	--mips64) add_arch mips64 ;;
	--mipsle) add_arch mipsle ;;
	--mips) add_arch mips ;;
	--loong64) add_arch loong64 ;;
	--wasm) add_arch wasm ;;
	--arch)
		need_value "$1" "${2:-}"
		add_arch "$2"
		shift
		;;
	--all) ALL=1 ;;
	--everything | --all-targets) EVERY=1 ;;
	--out)
		need_value "$1" "${2:-}"
		OUT_DIR=$2
		shift
		;;
	--list)
		go tool dist list
		exit 0
		;;
	-h | --help)
		usage
		exit 0
		;;
	*/*) add_pair "$1" ;;
	*)
		echo "?Unknown option $1" >&2
		echo "Type ./build.sh --help" >&2
		exit 2
		;;
	esac
	shift
done

HOST_OS=$(go env GOHOSTOS)
HOST_ARCH=$(go env GOHOSTARCH)
SUPPORTED=$(go tool dist list)

TARGETS=()
if ((EVERY)); then
	while read -r pair; do
		TARGETS+=("$pair")
	done <<<"$SUPPORTED"
elif ((ALL)); then
	TARGETS=("${ALL_TARGETS[@]}")
else
	for pair in $PAIRS; do
		TARGETS+=("$pair")
	done
	if [[ -n $OSES || -n $ARCHES || ${#TARGETS[@]} -eq 0 ]]; then
		[[ -z $OSES ]] && OSES=$HOST_OS
		for os in $OSES; do
			arches=$ARCHES
			if [[ -z $arches ]]; then
				if [[ $os == "$HOST_OS" ]]; then
					arches=$HOST_ARCH
				else
					arches=$(common_arches "$os")
				fi
			fi
			for arch in $arches; do
				TARGETS+=("$os/$arch")
			done
		done
	fi
fi

mkdir -p "$OUT_DIR"

built=()
skipped=()
for target in "${TARGETS[@]}"; do
	os=${target%/*}
	arch=${target#*/}
	if ! grep -qx "$target" <<<"$SUPPORTED"; then
		echo "?$target is not a platform/architecture this toolchain supports" >&2
		echo "Type ./build.sh --list" >&2
		exit 2
	fi
	name="rsts-$os-$arch"
	case "$os" in
	windows) name="$name.exe" ;;
	js | wasip1) name="$name.wasm" ;;
	esac
	echo "Building $target"
	# Unlink first: overwriting a binary that is still running fails with
	# "Text file busy", and the emulator is often up while you rebuild.
	rm -f "$OUT_DIR/$name"
	if ! CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOARM=7 \
		go build -trimpath -ldflags "-s -w" -o "$OUT_DIR/$name" "$PKG" 2>/tmp/rsts-build.$$; then
		if ((EVERY)); then
			skipped+=("$target: $(head -n 1 /tmp/rsts-build.$$)")
			rm -f /tmp/rsts-build.$$
			continue
		fi
		cat /tmp/rsts-build.$$ >&2
		rm -f /tmp/rsts-build.$$
		exit 1
	fi
	rm -f /tmp/rsts-build.$$
	built+=("$OUT_DIR/$name")
	if [[ $os == "$HOST_OS" && $arch == "$HOST_ARCH" ]]; then
		rm -f "$OUT_DIR/rsts"
		cp "$OUT_DIR/$name" "$OUT_DIR/rsts"
		built+=("$OUT_DIR/rsts")
	fi
done

echo
for path in "${built[@]}"; do
	case "$path" in
	/*) shown=$path ;;
	*) shown=$(pwd)/$path ;;
	esac
	pair=${path##*/rsts-}
	pair=${pair%.exe}
	pair=${pair%.wasm}
	note=$(describe "${pair/-//}")
	if [[ -n $note ]]; then
		printf '%8s  %-52s %s\n' "$(du -h "$path" | cut -f1)" "$shown" "$note"
	else
		printf '%8s  %s\n' "$(du -h "$path" | cut -f1)" "$shown"
	fi
done

if ((${#skipped[@]})); then
	echo
	echo "Not buildable on this toolchain:"
	for s in "${skipped[@]}"; do
		echo "  $s"
	done
fi
