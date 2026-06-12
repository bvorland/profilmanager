#!/usr/bin/env sh
# install.sh — one-shot installer for profilmanager (pm) on macOS / Linux.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/bvorland/profilmanager/main/install.sh | sh
#   ./install.sh --version v0.1.0
#   PREFIX=/usr/local ./install.sh
#
# Environment / flags:
#   PREFIX       Install prefix (default: $HOME/.local). Binary lands in $PREFIX/bin.
#   --version V  Install a specific tag (default: latest release).
#   --dry-run    Print the steps but do not download or write anything.
#
# Idempotent: re-running upgrades to the same/newer version in place.

set -eu

REPO="bvorland/profilmanager"
BIN_NAME="pm"
VERSION=""
DRY_RUN=0
PREFIX="${PREFIX:-$HOME/.local}"

err()  { printf 'install.sh: error: %s\n' "$*" >&2; exit 1; }
info() { printf 'install.sh: %s\n' "$*"; }
run()  { if [ "$DRY_RUN" -eq 1 ]; then printf '  + %s\n' "$*"; else eval "$@"; fi; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help)
      sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) err "unknown argument: $1" ;;
  esac
done

uname_s="$(uname -s)"
uname_m="$(uname -m)"
case "$uname_s" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *) err "unsupported OS: $uname_s (use install.ps1 on Windows)" ;;
esac
case "$uname_m" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) err "unsupported architecture: $uname_m" ;;
esac

for cmd in curl tar mkdir mv chmod; do
  command -v "$cmd" >/dev/null 2>&1 || err "missing required command: $cmd"
done
SHA_CMD=""
if command -v sha256sum >/dev/null 2>&1; then SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then SHA_CMD="shasum -a 256"
else err "need sha256sum or shasum -a 256 for checksum verification"
fi

if [ -z "$VERSION" ]; then
  info "Resolving latest release of $REPO ..."
  VERSION="$(curl -fsSL -o /dev/null -w '%{url_effective}' \
    "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
  [ -n "$VERSION" ] || err "could not resolve latest version"
fi
VER_NO_V="${VERSION#v}"
info "Installing $BIN_NAME $VERSION for $OS/$ARCH into $PREFIX/bin"

ARCHIVE="${BIN_NAME}_${VER_NO_V}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/$REPO/releases/download/$VERSION"

TMP="$(mktemp -d 2>/dev/null || mktemp -d -t pm-install)"
trap 'rm -rf "$TMP"' EXIT INT TERM

run "curl -fsSL -o \"$TMP/$ARCHIVE\" \"$BASE_URL/$ARCHIVE\""
run "curl -fsSL -o \"$TMP/checksums.txt\" \"$BASE_URL/checksums.txt\""

info "Verifying SHA-256 checksum ..."
if [ "$DRY_RUN" -eq 0 ]; then
  ( cd "$TMP" && grep " $ARCHIVE\$" checksums.txt | $SHA_CMD -c - ) \
    || err "checksum verification failed for $ARCHIVE"
fi

run "tar -xzf \"$TMP/$ARCHIVE\" -C \"$TMP\""
run "mkdir -p \"$PREFIX/bin\""
run "mv \"$TMP/$BIN_NAME\" \"$PREFIX/bin/$BIN_NAME\""
run "chmod 755 \"$PREFIX/bin/$BIN_NAME\""

info "Installed $PREFIX/bin/$BIN_NAME"
case ":$PATH:" in
  *":$PREFIX/bin:"*) ;;
  *) info "Add \"$PREFIX/bin\" to your PATH, e.g.:"
     info "  echo 'export PATH=\"$PREFIX/bin:\$PATH\"' >> ~/.profile" ;;
esac
cat <<EOF

Next: enable shell integration so PM_SESSION_ID is bound for every shell.
  bash:  eval "\$($BIN_NAME session init --shell bash)" >> ~/.bashrc
  zsh:   eval "\$($BIN_NAME session init --shell zsh)"  >> ~/.zshrc
  fish:  $BIN_NAME session init --shell fish | source   >> ~/.config/fish/config.fish

Run '$BIN_NAME doctor' to verify your environment.
EOF
