#!/bin/sh
# Install torrnado: fetch the release archive for this machine, check it
# against the release's own checksums.txt, and put the binary on PATH.
#
#   curl -fsSL https://torrnado.dev/install.sh | sh
#
# TORRNADO_VERSION      tag to install (default: the latest release)
# TORRNADO_INSTALL_DIR  where the binary goes (default: /usr/local/bin,
#                       or ~/.local/bin when that is not writable)
#
# POSIX sh, not bash: this is piped into whatever the reader's /bin/sh is.

set -eu

REPO="lestex/torrnado"

say() { printf '%s\n' "$*"; }
die() { printf 'install: %s\n' "$*" >&2; exit 1; }

if command -v curl >/dev/null 2>&1; then
	get() { curl -fsSL "$1" -o "$2"; }
	read_url() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
	get() { wget -qO "$2" "$1"; }
	read_url() { wget -qO- "$1"; }
else
	die "curl or wget is required"
fi

command -v tar >/dev/null 2>&1 || die "tar is required"

# The archive names GoReleaser writes, from the same uname the release
# matrix is built for. Anything else has no archive to download.
os=$(uname -s)
case "$os" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) die "unsupported system: $os (torrnado builds for Linux and macOS)" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "unsupported architecture: $arch" ;;
esac

version="${TORRNADO_VERSION:-}"
if [ -z "$version" ]; then
	version=$(read_url "https://api.github.com/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name"[ :]*"\([^"]*\)".*/\1/p' | head -1)
	[ -n "$version" ] || die "could not work out the latest release"
fi

# The tag carries a v, the archive names do not.
archive="torrnado_${version#v}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "torrnado $version - $os/$arch"
get "$base/$archive" "$tmp/$archive" || die "no $archive in $version"
get "$base/checksums.txt" "$tmp/checksums.txt" || die "could not fetch checksums.txt"

# Verified before anything is unpacked: this script is piped into a
# shell, so the one thing it can still promise is that the bytes it runs
# are the bytes that were released.
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
else
	die "sha256sum or shasum is required to verify the download"
fi
want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$archive is not listed in checksums.txt"
[ "$got" = "$want" ] || die "checksum mismatch for $archive"

tar xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/torrnado" ] || die "no torrnado binary in $archive"

# No sudo from a piped script: fall back to a directory this user owns
# rather than asking for a password nobody typed this command expecting.
dir="${TORRNADO_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ]; then
		dir=/usr/local/bin
	else
		dir="$HOME/.local/bin"
	fi
fi
mkdir -p "$dir" || die "could not create $dir"
[ -w "$dir" ] || die "$dir is not writable - set TORRNADO_INSTALL_DIR, or rerun under sudo"

install -m 0755 "$tmp/torrnado" "$dir/torrnado" 2>/dev/null ||
	{ cp "$tmp/torrnado" "$dir/torrnado" && chmod 0755 "$dir/torrnado"; }

say "installed $dir/torrnado"

case ":$PATH:" in
*":$dir:"*) ;;
*)
	say ""
	say "$dir is not on your PATH. Add it:"
	say "    export PATH=\"$dir:\$PATH\""
	;;
esac

say ""
"$dir/torrnado" version
