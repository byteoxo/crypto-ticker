#!/bin/sh

set -eu

REPOSITORY="byteoxo/crypto-ticker"
BINARY="crypto-ticker"
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"
VERSION="${VERSION:-latest}"

fail() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

download() {
	url=$1
	destination=$2

	if command -v curl >/dev/null 2>&1; then
		curl --proto '=https' --tlsv1.2 -fsSL "$url" -o "$destination"
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$url" -O "$destination"
	else
		fail "curl or wget is required"
	fi
}

[ "$(uname -s)" = "Linux" ] || fail "this installer only supports Linux"

case "$(uname -m)" in
	x86_64 | amd64)
		architecture=amd64
		;;
	aarch64 | arm64)
		architecture=arm64
		;;
	*)
		fail "unsupported architecture: $(uname -m)"
		;;
esac

command -v tar >/dev/null 2>&1 || fail "tar is required"
command -v install >/dev/null 2>&1 || fail "install is required"

archive="${BINARY}_linux_${architecture}.tar.gz"
if [ "$VERSION" = "latest" ]; then
	release_url="https://github.com/${REPOSITORY}/releases/latest/download"
else
	case "$VERSION" in
		v*) release_version=$VERSION ;;
		*) release_version="v${VERSION}" ;;
	esac
	release_url="https://github.com/${REPOSITORY}/releases/download/${release_version}"
fi

temporary_directory=$(mktemp -d)
trap 'rm -rf "$temporary_directory"' EXIT HUP INT TERM

printf 'Downloading %s (%s)...\n' "$BINARY" "$architecture"
download "${release_url}/${archive}" "${temporary_directory}/${archive}"
download "${release_url}/checksums.txt" "${temporary_directory}/checksums.txt"

expected_checksum=$(awk -v archive="$archive" '$2 == archive { print $1; exit }' "${temporary_directory}/checksums.txt")
[ -n "$expected_checksum" ] || fail "checksum for ${archive} was not found"

if command -v sha256sum >/dev/null 2>&1; then
	actual_checksum=$(sha256sum "${temporary_directory}/${archive}" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	actual_checksum=$(shasum -a 256 "${temporary_directory}/${archive}" | awk '{ print $1 }')
else
	fail "sha256sum or shasum is required to verify the download"
fi

[ "$actual_checksum" = "$expected_checksum" ] || fail "checksum verification failed"

tar -xzf "${temporary_directory}/${archive}" -C "$temporary_directory" "$BINARY"
mkdir -p "$INSTALL_DIR"
install -m 0755 "${temporary_directory}/${BINARY}" "${INSTALL_DIR}/${BINARY}"

printf 'Installed %s to %s\n' "$BINARY" "${INSTALL_DIR}/${BINARY}"
case ":${PATH}:" in
	*":${INSTALL_DIR}:"*) ;;
	*) printf 'Add %s to your PATH to run %s.\n' "$INSTALL_DIR" "$BINARY" ;;
esac
