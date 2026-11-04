#!/bin/sh
# Installs pm on Linux or macOS from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/kdicusb/portman/main/install.sh | sh
#
# PM_INSTALL_DIR  directory for pm (default /usr/local/bin)
# PM_VERSION      release to install, like v0.2.0-beta.1 (default: newest stable)
# PM_FROM         install from this directory, the output of make dist, instead of GitHub
set -eu

repo=kdicusb/portman
dir=${PM_INSTALL_DIR:-/usr/local/bin}
gum_version=2.0.2

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
x86_64 | amd64) arch=amd64 gum_arch=x86_64 ;;
aarch64 | arm64) arch=arm64 gum_arch=arm64 ;;
*) echo "pm has no build for $(uname -m)" >&2 && exit 1 ;;
esac
case $os/$gum_arch in
linux/x86_64) gum_sum=d842e06d93dbed90af48cb8dd10698db6f22e331fc40346bb37bbc753109edc2 ;;
linux/arm64) gum_sum=8ebf8b54ec1e8c81f2bb58b59ff9b70998186a4d11375f0cf357b80e0ccfa1d5 ;;
darwin/x86_64) gum_sum=5374966c7c7199ea879fcaa525ddc6d447a098d3d35496e430a9a1ef38d30485 ;;
darwin/arm64) gum_sum=4777a69b1170b8db23c95d5889fb32186cfda1a3ac950d339aa17e3513633890 ;;
*) echo "pm has no build for $os" >&2 && exit 1 ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1
}

# gum only draws the output, so fetch it before anything else and check it.
gum_os=$(uname -s)
gum_dir="gum_${gum_version}_${gum_os}_${gum_arch}"
curl -fsSL -o "$tmp/gum.tar.gz" "https://github.com/charmbracelet/gum/releases/download/v$gum_version/$gum_dir.tar.gz"
[ "$(sha256 "$tmp/gum.tar.gz")" = "$gum_sum" ] || { echo "checksum mismatch for gum" >&2 && exit 1; }
tar -xzf "$tmp/gum.tar.gz" -C "$tmp" "$gum_dir/gum"
gum() { "$tmp/$gum_dir/gum" "$@"; }

# The same accent and red as pm itself. Output captured by $(...) goes
# through a pipe, so gum needs telling that colors reach a terminal.
ACCENT='#14B8A6' RED='#EF4444'
export GUM_SPIN_SPINNER_FOREGROUND=$ACCENT
[ -t 1 ] && export CLICOLOR_FORCE=1

ok() { printf '  %s %s\n' "$(gum style --foreground $ACCENT ✓)" "$*"; }
info() { gum style --faint "    $*"; }
die() {
	printf '  %s\n' "$(gum style --foreground $RED "✗ $*")" >&2
	exit 1
}
# spin runs a command behind a spinner and keeps its error output on failure.
spin() {
	title=$1
	shift
	if [ -t 1 ]; then gum spin --title "  $title" --show-error -- "$@"; else "$@"; fi
}

echo
printf '  %s %s\n\n' "$(gum style --foreground $ACCENT --bold portman)" "$(gum style --faint installer)"

if [ -n "${PM_FROM:-}" ]; then
	src=$PM_FROM
	tag=${PM_VERSION:-$(cd "$src" && ls pm_*_"${os}_$arch" 2>/dev/null | head -n 1 | sed "s/^pm_\(.*\)_${os}_$arch\$/\1/")}
	[ -n "$tag" ] || die "no pm_*_${os}_$arch in $src"
	ok "Using $tag from $src"
else
	src=$tmp
	if [ -z "${PM_VERSION:-}" ]; then
		spin "Finding the newest release" sh -c \
			"curl -fsL https://api.github.com/repos/$repo/releases/latest > '$tmp/latest.json'" ||
			die "found no stable release of pm on GitHub"
		PM_VERSION=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp/latest.json")
		[ -n "$PM_VERSION" ] || die "no stable release found"
	fi
	tag=$PM_VERSION
	url="https://github.com/$repo/releases/download/$tag"
	spin "Downloading pm $tag" sh -c \
		"curl -fsSL -o '$tmp/pm_${tag}_${os}_$arch' '$url/pm_${tag}_${os}_$arch' && curl -fsSL -o '$tmp/SHA256SUMS' '$url/SHA256SUMS'" ||
		die "couldn't download pm $tag for $os/$arch"
	ok "Downloaded pm $tag"
fi

asset="pm_${tag}_${os}_$arch"
want=$(grep " $asset\$" "$src/SHA256SUMS" | cut -d' ' -f1)
[ -n "$want" ] && [ "$want" = "$(sha256 "$src/$asset")" ] || die "checksum mismatch for $asset"
ok "Checksum verified"

old=$("$dir/pm" --version 2>/dev/null | sed 's/^pm version //' || true)
cp "$src/$asset" "$tmp/pm"
chmod +x "$tmp/pm"
sudo=
if ! { [ -d "$dir" ] && [ -w "$dir" ]; } && ! { [ ! -e "$dir" ] && mkdir -p "$dir" 2>/dev/null; }; then
	sudo=sudo
	info "$dir needs sudo"
	sudo -v || die "sudo is needed to write $dir"
fi
$sudo mkdir -p "$dir"
$sudo mv "$tmp/pm" "$dir/pm"
if [ -n "$old" ] && [ "$old" != "$tag" ]; then
	ok "Installed $dir/pm $(gum style --faint "(was $old)")"
else
	ok "Installed $dir/pm"
fi

case ":$PATH:" in
*":$dir:"*) ;;
*) info "$dir isn't on your PATH: add it to use pm by name" ;;
esac

echo
printf '  Next, start portman:  %s\n' "$(gum style --foreground $ACCENT 'pm start')"
printf '  Then map an app:      %s\n\n' "$(gum style --foreground $ACCENT 'pm add api 8080')"
