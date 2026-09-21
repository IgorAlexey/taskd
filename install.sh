#!/bin/sh
set -eu

repo=IgorAlexey/taskd
dir="${TASKD_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "install.sh: no taskd build for $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "install.sh: no taskd build for $(uname -m)" >&2; exit 1 ;;
esac

tag="${TASKD_VERSION:-}"
if [ -z "$tag" ]; then
  tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
  tag="${tag##*/}"
  [ "$tag" != latest ] || tag=""
fi
[ -n "$tag" ] || { echo "install.sh: could not find the latest release" >&2; exit 1; }
version="${tag#v}"
tag="v$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/taskd.tar.gz" "https://github.com/$repo/releases/download/$tag/taskd_${version}_${os}_${arch}.tar.gz"
tar -xzf "$tmp/taskd.tar.gz" -C "$tmp"
mkdir -p "$dir"
install -m 755 "$tmp/taskd" "$tmp/taskd-tui" "$dir"
echo "installed taskd $tag to $dir"

case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "add it to your PATH:  export PATH=\"$dir:\$PATH\"" ;;
esac
