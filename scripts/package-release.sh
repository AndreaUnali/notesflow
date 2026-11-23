#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

if [[ $# != 2 || ! ${1:-} =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf 'Usage: %s vMAJOR.MINOR.PATCH OUTPUT_DIRECTORY\n' "$0" >&2
    exit 2
fi

tag=$1
version=${tag#v}
mkdir -p "$2"
output=$(cd "$2" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for os in darwin linux; do
    for arch in amd64 arm64; do
        target="$os-$arch"
        archive="notesflow-$tag-$target.tar.gz"
        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
            -ldflags "-s -w -X main.version=$version" -o "$work/notesflow" ./cmd/notesflow
        tar -czf "$output/$archive" -C "$work" notesflow

        if [[ $os == linux ]]; then
            package="$work/deb-$arch"
            mkdir -p "$package/DEBIAN" "$package/usr/bin"
            install -m 755 "$work/notesflow" "$package/usr/bin/notesflow"
            cat > "$package/DEBIAN/control" <<CONTROL
Package: notesflow
Version: $version
Section: editors
Priority: optional
Architecture: $arch
Maintainer: AndreaUnali <AndreaUnali@users.noreply.github.com>
Depends: neovim, poppler-utils
Suggests: latexmk, texlive-latex-extra
Homepage: https://github.com/AndreaUnali/notesflow
Description: Terminal workspace for LaTeX notes
 Edit notes in Neovim, preview PDFs, and explore links between topics.
CONTROL
            dpkg-deb --root-owner-group --build "$package" "$output/notesflow-linux-$arch.deb"
        fi
    done
done


scripts/bottle-release.sh "$tag" "$output"

(
    cd "$output"
    sha256sum "notesflow-$tag-"*.tar.gz notesflow-linux-*.deb notesflow-*.bottle.tar.gz notesflow-bottle-formula.rb > SHA256SUMS
)
