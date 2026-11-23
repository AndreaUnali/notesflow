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

declare -A archive_sha
for os in darwin linux; do
    for arch in amd64 arm64; do
        target="$os-$arch"
        archive="notesflow-$tag-$target.tar.gz"
        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
            -ldflags "-s -w -X main.version=$version" -o "$work/notesflow" ./cmd/notesflow
        tar -czf "$output/$archive" -C "$work" notesflow
        archive_sha[$target]=$(sha256sum "$output/$archive" | cut -d ' ' -f1)

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

cat > "$output/notesflow.rb" <<FORMULA
class Notesflow < Formula
  desc "Terminal workspace for LaTeX notes"
  homepage "https://github.com/AndreaUnali/notesflow"
  version "$version"

  on_macos do
    on_arm do
      url "https://github.com/AndreaUnali/notesflow/releases/download/$tag/notesflow-$tag-darwin-arm64.tar.gz"
      sha256 "${archive_sha[darwin-arm64]}"
    end
    on_intel do
      url "https://github.com/AndreaUnali/notesflow/releases/download/$tag/notesflow-$tag-darwin-amd64.tar.gz"
      sha256 "${archive_sha[darwin-amd64]}"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/AndreaUnali/notesflow/releases/download/$tag/notesflow-$tag-linux-arm64.tar.gz"
      sha256 "${archive_sha[linux-arm64]}"
    end
    on_intel do
      url "https://github.com/AndreaUnali/notesflow/releases/download/$tag/notesflow-$tag-linux-amd64.tar.gz"
      sha256 "${archive_sha[linux-amd64]}"
    end
  end

  depends_on "neovim"
  depends_on "poppler"

  def install
    bin.install "notesflow"
  end

  test do
    assert_equal "notesflow #{version}", shell_output("#{bin}/notesflow --version").strip
  end
end
FORMULA

(
    cd "$output"
    sha256sum "notesflow-$tag-"*.tar.gz notesflow-linux-*.deb notesflow.rb > SHA256SUMS
)
