# Installation

## Install script (recommended)

One command installs the correct binary for your platform, verifies the checksum, and places it on your `PATH`:

```bash
curl -fsSL https://silocorp.github.io/workflow/install.sh | sh
```

Or with `wget`:

```bash
wget -qO- https://silocorp.github.io/workflow/install.sh | sh
```

The script:

1. Detects your OS and architecture
2. Fetches the latest release version from the GitHub API
3. Downloads the archive from GitHub Releases
4. Verifies the SHA-256 checksum against the published `checksums.txt`
5. Extracts the binary and places it in `/usr/local/bin` (if writable) or `~/.local/bin`
6. Confirms the installation with `wf --version`

**Options**:

```bash
# Install a specific version
curl -fsSL https://silocorp.github.io/workflow/install.sh | sh -s -- --version 0.2.0

# Install to a custom directory
curl -fsSL https://silocorp.github.io/workflow/install.sh | WF_INSTALL_DIR=~/.local/bin sh

# Verify checksum only, do not install
curl -fsSL https://silocorp.github.io/workflow/install.sh | sh -s -- --verify-only
```

---

## Download binary manually

Pre-built binaries for all supported platforms are available on the [GitHub Releases page](https://github.com/silocorp/workflow/releases).

=== "Linux (amd64)"

    ```bash
    VERSION="0.2.0"
    curl -fsSL "https://github.com/silocorp/workflow/releases/download/v${VERSION}/wf_${VERSION}_linux_amd64.tar.gz" \
      -o wf.tar.gz

    # Verify checksum
    curl -fsSL "https://github.com/silocorp/workflow/releases/download/v${VERSION}/checksums.txt" \
      | grep "linux_amd64" | sha256sum -c

    tar -xzf wf.tar.gz
    sudo mv wf /usr/local/bin/
    wf --version
    ```

=== "Linux (arm64)"

    ```bash
    VERSION="0.2.0"
    curl -fsSL "https://github.com/silocorp/workflow/releases/download/v${VERSION}/wf_${VERSION}_linux_arm64.tar.gz" \
      -o wf.tar.gz

    curl -fsSL "https://github.com/silocorp/workflow/releases/download/v${VERSION}/checksums.txt" \
      | grep "linux_arm64" | sha256sum -c

    tar -xzf wf.tar.gz
    sudo mv wf /usr/local/bin/
    wf --version
    ```

=== "macOS (Apple Silicon)"

    ```bash
    VERSION="0.2.0"
    curl -fsSL "https://github.com/silocorp/workflow/releases/download/v${VERSION}/wf_${VERSION}_darwin_arm64.tar.gz" \
      -o wf.tar.gz

    curl -fsSL "https://github.com/silocorp/workflow/releases/download/v${VERSION}/checksums.txt" \
      | grep "darwin_arm64" | shasum -a 256 -c

    tar -xzf wf.tar.gz
    sudo mv wf /usr/local/bin/
    wf --version
    ```

=== "macOS (Intel)"

    ```bash
    VERSION="0.2.0"
    curl -fsSL "https://github.com/silocorp/workflow/releases/download/v${VERSION}/wf_${VERSION}_darwin_amd64.tar.gz" \
      -o wf.tar.gz

    curl -fsSL "https://github.com/silocorp/workflow/releases/download/v${VERSION}/checksums.txt" \
      | grep "darwin_amd64" | shasum -a 256 -c

    tar -xzf wf.tar.gz
    sudo mv wf /usr/local/bin/
    wf --version
    ```

=== "Windows (amd64)"

    Download `wf_0.2.0_windows_amd64.zip` from the [Releases page](https://github.com/silocorp/workflow/releases/latest), extract it, and move `wf.exe` to a directory on your `PATH`.

    PowerShell:

    ```powershell
    $version = "0.2.0"
    Invoke-WebRequest `
      -Uri "https://github.com/silocorp/workflow/releases/download/v$version/wf_${version}_windows_amd64.zip" `
      -OutFile wf.zip
    Expand-Archive wf.zip -DestinationPath .
    Move-Item wf.exe "$env:LOCALAPPDATA\Microsoft\WindowsApps\"
    wf --version
    ```

---

## Verify installation

```bash
wf --version
# wf version 0.2.0

wf health
```

`wf health` confirms the database is reachable, the workflows directory exists, and the configuration is valid.

---

## Initialise the workspace

```bash
wf init
```

Creates the required directories and a config stub:

| Path | Description |
|---|---|
| `~/.cache/workflow/workflow.db` | SQLite database (Linux) |
| `~/.config/workflow/workflows/` | Default workflows directory |
| `~/.cache/workflow/logs/` | Per-task log files |
| `~/.config/workflow/config.yaml` | Configuration file |

!!! note "Custom paths"
    All paths can be changed. See [Configuration](configuration.md).

---

## Platform notes

=== "Linux"

    Default data directory: `~/.cache/workflow/`
    Default config: `~/.config/workflow/config.yaml`

=== "macOS"

    Default data directory: `~/Library/Caches/workflow/`
    Default config: `~/Library/Application Support/workflow/config.yaml`

=== "Windows"

    Default data directory: `%LocalAppData%\workflow\`
    Default config: `%AppData%\workflow\config.yaml`

    Process group management (`SIGKILL` on timeout) uses Windows Job Objects — equivalent to the Unix `Setpgid` implementation.

---

## Upgrading

Re-run the install script — it always fetches the latest release:

```bash
curl -fsSL https://silocorp.github.io/workflow/install.sh | sh
```

Or download the new archive manually from the [Releases page](https://github.com/silocorp/workflow/releases) and replace the binary. Schema migrations run automatically on first use after an upgrade — no manual steps required.

---

## Install with `go install`

For Go developers who want the latest commit from the default branch:

```bash
go install github.com/silocorp/workflow@latest
```

The binary is placed in `$(go env GOPATH)/bin`. Ensure that directory is on your `PATH`.

---

## Build from source

For contributors and developers working on `wf` itself. Requires Go 1.24+.

```bash
git clone https://github.com/silocorp/workflow.git
cd workflow
go build -o wf .
sudo mv wf /usr/local/bin/
wf --version
```

See [CONTRIBUTING.md](https://github.com/silocorp/workflow/blob/master/CONTRIBUTING.md) for the full development guide.
