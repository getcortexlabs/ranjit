#!/bin/sh
# Install a released binary in the current user's directory. No sudo, Go or Docker.
set -eu

version=${RANJIT_VERSION:-v0.1.0}
destination=${RANJIT_INSTALL_DIR:-"$HOME/.local/bin"}
case "$version" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo 'Invalid RANJIT_VERSION' >&2; exit 1 ;; esac
case "$version" in *[!a-zA-Z0-9.-]*) echo 'Invalid RANJIT_VERSION' >&2; exit 1 ;; esac
case "$(uname -s)" in Darwin) system=darwin ;; Linux) system=linux ;; *) echo 'Download the Windows zip from https://github.com/getcortexlabs/ranjit/releases' >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) echo 'Unsupported architecture' >&2; exit 1 ;; esac

command -v curl >/dev/null 2>&1 || { echo 'curl is required' >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo 'tar is required' >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then checksum=sha256sum
elif command -v shasum >/dev/null 2>&1; then checksum=shasum
else echo 'sha256sum or shasum is required' >&2; exit 1
fi

temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
asset="ranjit_${version}_${system}_${arch}.tar.gz"
base="https://github.com/getcortexlabs/ranjit/releases/download/$version"
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/$asset" -o "$temporary/$asset"
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/checksums.txt" -o "$temporary/checksums.txt"
expected=$(awk -v asset="$asset" '$2 == asset {print $1}' "$temporary/checksums.txt")
case "$expected" in ''|*[!a-f0-9]*) echo 'Missing or invalid checksum' >&2; exit 1 ;; esac
[ "${#expected}" -eq 64 ] || { echo 'Invalid checksum length' >&2; exit 1; }
if [ "$checksum" = sha256sum ]; then actual=$(sha256sum "$temporary/$asset" | awk '{print $1}')
else actual=$(shasum -a 256 "$temporary/$asset" | awk '{print $1}')
fi
[ "$actual" = "$expected" ] || { echo 'Checksum mismatch; installation stopped' >&2; exit 1; }
tar -xzf "$temporary/$asset" -C "$temporary" ranjit
mkdir -p "$destination"
install -m 755 "$temporary/ranjit" "$destination/.ranjit-install-$$"
mv -f "$destination/.ranjit-install-$$" "$destination/ranjit"
printf 'Ranjit %s installed: %s/ranjit\n' "$version" "$destination"
case ":$PATH:" in *":$destination:"*) printf 'Try: ranjit demo\n' ;; *) printf 'Add this directory to your PATH: %s\nTry: "%s/ranjit" demo\n' "$destination" "$destination" ;; esac
