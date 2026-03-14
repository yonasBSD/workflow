#!/bin/sh
# wf installer
#
# Usage:
#   curl -fsSL https://silocorp.github.io/workflow/install.sh | sh
#   curl -fsSL https://silocorp.github.io/workflow/install.sh | sh -s -- --version 0.2.0
#   curl -fsSL https://silocorp.github.io/workflow/install.sh | WF_INSTALL_DIR=~/.local/bin sh
#   curl -fsSL https://silocorp.github.io/workflow/install.sh | sh -s -- --verify-only

set -e

# ── Configuration ─────────────────────────────────────────────────────────────

REPO="silocorp/workflow"
BINARY="wf"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GITHUB_RELEASES="https://github.com/${REPO}/releases/download"

# ── Defaults (overridable via env or flags) ────────────────────────────────────

VERSION=""
INSTALL_DIR="${WF_INSTALL_DIR:-}"
VERIFY_ONLY=0

# ── Parse flags ───────────────────────────────────────────────────────────────

while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            VERSION="$2"; shift 2 ;;
        --install-dir)
            INSTALL_DIR="$2"; shift 2 ;;
        --verify-only)
            VERIFY_ONLY=1; shift ;;
        *)
            echo "Unknown option: $1" >&2
            exit 1 ;;
    esac
done

# ── Helpers ───────────────────────────────────────────────────────────────────

say()  { printf '\033[1m%s\033[0m\n' "$1"; }
ok()   { printf '\033[32m✓\033[0m %s\n' "$1"; }
err()  { printf '\033[31m✗\033[0m %s\n' "$1" >&2; exit 1; }
warn() { printf '\033[33m!\033[0m %s\n' "$1" >&2; }

need() {
    if ! command -v "$1" >/dev/null 2>&1; then
        err "Required tool not found: $1 — please install it and retry."
    fi
}

# ── Detect OS ─────────────────────────────────────────────────────────────────

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux)  OS="linux"  ;;
    darwin) OS="darwin" ;;
    *)      err "Unsupported operating system: $(uname -s). Download manually from https://github.com/${REPO}/releases" ;;
esac

# ── Detect architecture ───────────────────────────────────────────────────────

ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)   ARCH="amd64"  ;;
    aarch64|arm64)  ARCH="arm64"  ;;
    armv7l)         ARCH="armv7"  ;;
    *)              err "Unsupported architecture: $(uname -m). Download manually from https://github.com/${REPO}/releases" ;;
esac

# ── Fetch latest version if not specified ─────────────────────────────────────

say "wf installer"

if [ -z "$VERSION" ]; then
    need curl
    say "  Fetching latest release..."
    VERSION="$(curl -fsSL "$GITHUB_API" \
        | grep '"tag_name"' \
        | sed 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/')"
    if [ -z "$VERSION" ]; then
        err "Could not determine the latest release. Check https://github.com/${REPO}/releases"
    fi
fi

say "  Version  : v${VERSION}"
say "  Platform : ${OS}/${ARCH}"

# ── Construct URLs ────────────────────────────────────────────────────────────

ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
ARCHIVE_URL="${GITHUB_RELEASES}/v${VERSION}/${ARCHIVE}"
CHECKSUM_URL="${GITHUB_RELEASES}/v${VERSION}/checksums.txt"

# ── Download to temp dir ──────────────────────────────────────────────────────

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

say "  Downloading archive..."
if command -v curl >/dev/null 2>&1; then
    curl -fsSL --progress-bar "$ARCHIVE_URL"  -o "${TMP}/${ARCHIVE}"
    curl -fsSL               "$CHECKSUM_URL" -o "${TMP}/checksums.txt"
elif command -v wget >/dev/null 2>&1; then
    wget -q --show-progress "$ARCHIVE_URL"  -O "${TMP}/${ARCHIVE}"
    wget -q                 "$CHECKSUM_URL" -O "${TMP}/checksums.txt"
else
    err "Neither curl nor wget found. Install one and retry."
fi

# ── Verify checksum ───────────────────────────────────────────────────────────

say "  Verifying checksum..."
EXPECTED="$(grep "${ARCHIVE}" "${TMP}/checksums.txt" | awk '{print $1}')"

if [ -z "$EXPECTED" ]; then
    warn "Checksum entry for ${ARCHIVE} not found in checksums.txt — skipping verification."
else
    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL="$(sha256sum "${TMP}/${ARCHIVE}" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        ACTUAL="$(shasum -a 256 "${TMP}/${ARCHIVE}" | awk '{print $1}')"
    else
        warn "No sha256sum or shasum available — skipping checksum verification."
        ACTUAL="$EXPECTED"
    fi

    if [ "$ACTUAL" != "$EXPECTED" ]; then
        err "Checksum mismatch!
  Expected : ${EXPECTED}
  Got      : ${ACTUAL}
The downloaded archive may be corrupt or tampered with. Aborting."
    fi
    ok "Checksum verified."
fi

[ "$VERIFY_ONLY" -eq 1 ] && { ok "Verification complete. Not installing (--verify-only)."; exit 0; }

# ── Extract ───────────────────────────────────────────────────────────────────

say "  Extracting..."
tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"

if [ ! -f "${TMP}/${BINARY}" ]; then
    err "Binary '${BINARY}' not found in archive. Archive contents: $(ls "$TMP")"
fi

# ── Select install directory ──────────────────────────────────────────────────

if [ -z "$INSTALL_DIR" ]; then
    if [ -w /usr/local/bin ]; then
        INSTALL_DIR="/usr/local/bin"
    elif [ -w "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
        INSTALL_DIR="$HOME/.local/bin"
    else
        err "Cannot determine a writable install directory. Set WF_INSTALL_DIR or use --install-dir."
    fi
fi

mkdir -p "$INSTALL_DIR"
say "  Installing to ${INSTALL_DIR}/${BINARY}..."

# If the target directory requires elevated permissions and we're not root, try sudo
if [ ! -w "$INSTALL_DIR" ]; then
    if command -v sudo >/dev/null 2>&1; then
        say "  (requires sudo for ${INSTALL_DIR})"
        sudo mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
        sudo chmod 755 "${INSTALL_DIR}/${BINARY}"
    else
        err "Cannot write to ${INSTALL_DIR} and sudo is not available. Set WF_INSTALL_DIR=~/.local/bin to install without sudo."
    fi
else
    mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    chmod 755 "${INSTALL_DIR}/${BINARY}"
fi

# ── Verify ────────────────────────────────────────────────────────────────────

ok "Installed: ${INSTALL_DIR}/${BINARY}"

# Check if install dir is on PATH
case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
        warn "${INSTALL_DIR} is not on your PATH."
        warn "Add the following to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        warn "  export PATH=\"${INSTALL_DIR}:\$PATH\""
        ;;
esac

# Run version check
if "${INSTALL_DIR}/${BINARY}" --version >/dev/null 2>&1; then
    ok "$("${INSTALL_DIR}/${BINARY}" --version)"
    echo ""
    echo "Run 'wf init' to initialise your workspace."
    echo "Documentation: https://silocorp.github.io/workflow"
else
    warn "Installation complete, but 'wf --version' failed. Check your PATH."
fi
