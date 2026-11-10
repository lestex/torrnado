#!/usr/bin/env bash
#
# Renders the Homebrew formula for a published tag to stdout.
#
#   contrib/homebrew/render.sh v0.10.0                # fetches the tag's checksums.txt
#   contrib/homebrew/render.sh v0.10.0 checksums.txt  # or reads a local copy
#
# The formula is built from the checksums the release itself published, so
# it cannot name an archive that does not exist or carry a hash for a
# different build than the one on the release page.
#
# It wraps pre-compiled binaries rather than building from source, and
# that is deliberate: a build on the user's machine would not be the
# CGO_ENABLED=0 binary the release ships, and the two keep piece-completion
# state in databases that do not read each other's files. See
# .goreleaser.yaml.
#
# Strict on purpose. A generator that quietly emits a formula with an empty
# hash is worse than one that stops, because what it writes is published.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="lestex/torrnado"

tag="${1:-}"
if [[ -z "$tag" ]]; then
  echo "usage: render.sh <tag> [checksums.txt]" >&2
  exit 2
fi

# Plain vX.Y.Z only. A -rc or -beta tag is a pre-release, and a pre-release
# must never become what `brew install` hands people - the same rule
# .goreleaser.yaml applies to the release page itself.
if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "render.sh: $tag is not a plain vX.Y.Z release tag" >&2
  exit 1
fi
version="${tag#v}"

if [[ -n "${2:-}" ]]; then
  sums="$(cat "$2")"
else
  sums="$(curl -fsSL "https://github.com/${repo}/releases/download/${tag}/checksums.txt")"
fi

# One hash per archive, found by exact file name. Exactly one match each:
# none means the release is missing a target, more than one means the file
# is not the one this expects, and either way publishing would be a guess.
hash_for() {
  local archive="torrnado_${version}_$1.tar.gz" found
  found="$(awk -v f="$archive" '$2 == f { print $1 }' <<<"$sums")"
  if [[ "$(wc -l <<<"$found" | tr -d ' ')" != 1 || ! "$found" =~ ^[0-9a-f]{64}$ ]]; then
    echo "render.sh: want exactly one sha256 for $archive in the checksums, got: '${found}'" >&2
    exit 1
  fi
  printf '%s' "$found"
}

darwin_arm64="$(hash_for darwin_arm64)"
darwin_amd64="$(hash_for darwin_amd64)"
linux_arm64="$(hash_for linux_arm64)"
linux_amd64="$(hash_for linux_amd64)"

out="$(sed \
  -e "s/@VERSION@/${version}/g" \
  -e "s/@DARWIN_ARM64@/${darwin_arm64}/" \
  -e "s/@DARWIN_AMD64@/${darwin_amd64}/" \
  -e "s/@LINUX_ARM64@/${linux_arm64}/" \
  -e "s/@LINUX_AMD64@/${linux_amd64}/" \
  "$here/torrnado.rb.in")"

# A placeholder left behind means the template grew a field this script
# does not know about.
if grep -q '@[A-Z0-9_]*@' <<<"$out"; then
  echo "render.sh: unfilled placeholder in the rendered formula:" >&2
  grep -n '@[A-Z0-9_]*@' <<<"$out" >&2
  exit 1
fi

printf '%s\n' "$out"
