#!/usr/bin/env bash
set -euo pipefail

tag=$1
version=${tag#v}
output=$(cd "$2" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
prefix="$work/notesflow/$version"
mkdir -p "$prefix/bin" "$prefix/.brew"

definition="$work/notesflow.rb"
cat > "$definition" <<FORMULA
class Notesflow < Formula
  desc "Terminal workspace for LaTeX notes"
  homepage "https://github.com/AndreaUnali/notesflow"
  version "$version"

  on_macos do
    on_arm do
      url "https://github.com/AndreaUnali/notesflow/releases/download/$tag/notesflow-$tag-darwin-arm64.tar.gz"
      sha256 "$(sha256sum "$output/notesflow-$tag-darwin-arm64.tar.gz" | cut -d ' ' -f1)"
    end
    on_intel do
      url "https://github.com/AndreaUnali/notesflow/releases/download/$tag/notesflow-$tag-darwin-amd64.tar.gz"
      sha256 "$(sha256sum "$output/notesflow-$tag-darwin-amd64.tar.gz" | cut -d ' ' -f1)"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/AndreaUnali/notesflow/releases/download/$tag/notesflow-$tag-linux-arm64.tar.gz"
      sha256 "$(sha256sum "$output/notesflow-$tag-linux-arm64.tar.gz" | cut -d ' ' -f1)"
    end
    on_intel do
      url "https://github.com/AndreaUnali/notesflow/releases/download/$tag/notesflow-$tag-linux-amd64.tar.gz"
      sha256 "$(sha256sum "$output/notesflow-$tag-linux-amd64.tar.gz" | cut -d ' ' -f1)"
    end
  end

  def install
    bin.install "notesflow"
  end

  test do
    assert_equal "notesflow #{version}", shell_output("#{bin}/notesflow --version").strip
  end
end
FORMULA

# Each bottle contains the same binary as its platform's release archive.
install -m 644 "$definition" "$prefix/.brew/notesflow.rb"
cat > "$prefix/INSTALL_RECEIPT.json" <<RECEIPT
{
  "built_as_bottle": true,
  "source": {
    "tap": "andreaunali/notesflow",
    "versions": {"stable": "$version", "version_scheme": 0},
    "spec": "stable"
  },
  "runtime_dependencies": [],
  "changed_files": []
}
RECEIPT

block="$work/bottle.txt"
printf '  bottle do\n    root_url "https://github.com/AndreaUnali/notesflow/releases/download/%s"\n' "$tag" > "$block"
while read -r platform bottle_tag; do
    tar -xzf "$output/notesflow-$tag-$platform.tar.gz" -C "$prefix/bin" notesflow
    bottle="$output/notesflow-$version.$bottle_tag.bottle.tar.gz"
    tar -czf "$bottle" -C "$work" notesflow
    checksum=$(sha256sum "$bottle" | cut -d ' ' -f1)
    printf '    sha256 cellar: :any_skip_relocation, %s: "%s"\n' "$bottle_tag" "$checksum" >> "$block"
done <<'PLATFORMS'
darwin-arm64 arm64_monterey
darwin-amd64 monterey
linux-arm64 arm64_linux
linux-amd64 x86_64_linux
PLATFORMS
printf '  end\n\n' >> "$block"
awk -v block="$block" '/^  def install$/ { while ((getline line < block) > 0) print line } { print }' \
    "$definition" > "$output/notesflow-bottle-formula.rb"
