#!/bin/sh
set -eu

usage() {
	printf '%s\n' 'usage: install.sh [--version vX.Y.Z]' >&2
	exit 2
}

version=
while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			[ "$#" -ge 2 ] || usage
			version=$2
			shift 2
			;;
		*) usage ;;
	esac
done

if [ -n "$version" ]; then
	case "$version" in
		v*) ;;
		*) printf '%s\n' 'install.sh: --version must start with v and contain only release characters' >&2; exit 2 ;;
	esac
	case "$version" in
		*[!v0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz._-]*) printf '%s\n' 'install.sh: invalid --version value' >&2; exit 2 ;;
	esac
fi

os=$(uname -s)
case "$os" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) printf 'install.sh: unsupported operating system: %s\n' "$os" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64|amd64) arch=amd64 ;;
	aarch64|arm64) arch=arm64 ;;
	*) printf 'install.sh: unsupported architecture: %s\n' "$arch" >&2; exit 1 ;;
esac

artifact="herdr-soho_${os}_${arch}"
release_base=${HERDR_SOHO_RELEASE_BASE:-https://github.com/djalmajr/herdr-soho/releases}
if [ -n "$version" ]; then
	release_url="$release_base/download/$version"
else
	release_url="$release_base/latest/download"
fi
install_dir=${HERDR_SOHO_INSTALL_DIR:-$HOME/.local/bin}
destination="$install_dir/herdr-soho"

# A literal newline and tab: $(printf '\n') would be empty, since command
# substitution drops trailing newlines, and an empty pattern matches anything.
nl='
'
tab='	'
case "$release_base" in
	*' '*|*"$nl"*|*"$tab"*) printf '%s\n' 'install.sh: release base URL contains whitespace' >&2; exit 2 ;;
esac
mkdir -p "$install_dir"
binary_tmp="$install_dir/.herdr-soho.tmp.$$"
sums_tmp="$install_dir/.herdr-soho-sums.tmp.$$"
cleanup() { rm -f "$binary_tmp" "$sums_tmp"; }
trap cleanup 0
trap 'exit 1' HUP INT TERM
(set -C; : > "$binary_tmp") 2>/dev/null || { printf '%s\n' 'install.sh: temporary file already exists' >&2; exit 1; }
(set -C; : > "$sums_tmp") 2>/dev/null || { printf '%s\n' 'install.sh: temporary file already exists' >&2; exit 1; }

curl -fsSL "$release_url/$artifact" -o "$binary_tmp" || { printf 'install.sh: failed to download %s\n' "$artifact" >&2; exit 1; }
curl -fsSL "$release_url/SHA256SUMS" -o "$sums_tmp" || { printf '%s\n' 'install.sh: failed to download SHA256SUMS' >&2; exit 1; }

expected=$(awk -v name="$artifact" '$2 == name { print $1; found++ } END { if (found != 1) exit 1 }' "$sums_tmp") || {
	printf 'install.sh: SHA256SUMS has no unique entry for %s\n' "$artifact" >&2
	exit 1
}
case "$expected" in
	*[!0123456789abcdefABCDEF]*|'') printf 'install.sh: invalid sha256 entry for %s\n' "$artifact" >&2; exit 1 ;;
esac
[ "${#expected}" -eq 64 ] || { printf 'install.sh: invalid sha256 entry for %s\n' "$artifact" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$binary_tmp" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$binary_tmp" | awk '{print $1}')
else
	printf '%s\n' 'install.sh: sha256sum or shasum is required' >&2
	exit 1
fi
# sha256sum and shasum print lowercase; a SHA256SUMS in uppercase is still valid.
expected=$(printf '%s' "$expected" | tr 'ABCDEF' 'abcdef')
actual=$(printf '%s' "$actual" | tr 'ABCDEF' 'abcdef')
[ "$actual" = "$expected" ] || { printf 'install.sh: sha256 mismatch for %s\n' "$artifact" >&2; exit 1; }

chmod 755 "$binary_tmp"
mv -f "$binary_tmp" "$destination"
rm -f "$sums_tmp"
trap - 0 HUP INT TERM

case ":${PATH:-}:" in
	*":$install_dir:"*) ;;
	*) printf 'Add %s to your PATH to run herdr-soho from any shell.\n' "$install_dir" ;;
esac
"$destination" --version
