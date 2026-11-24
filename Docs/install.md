# Installing and Verifying go-anything

This guide describes how to download, cryptographically verify, and install prebuilt `go-anything` release binaries or compile from source.

---

## Supported Platforms

Official prebuilt binaries are published for every tagged release on [GitHub Releases](https://github.com/mohammedrefat/go-anything/releases):

| Operating System | Architecture | Artifact Name | Format |
| :--- | :--- | :--- | :--- |
| **Linux** | `amd64` (x86_64) | `go-anything_<version>_linux_amd64.tar.gz` | Tarball (`tar.gz`) |
| **Linux** | `arm64` (AArch64) | `go-anything_<version>_linux_arm64.tar.gz` | Tarball (`tar.gz`) |
| **macOS** | `arm64` (Apple Silicon) | `go-anything_<version>_darwin_arm64.tar.gz` | Tarball (`tar.gz`) |
| **macOS** | `amd64` (Intel) | `go-anything_<version>_darwin_amd64.tar.gz` | Tarball (`tar.gz`) |
| **Windows** | `amd64` (x86_64) | `go-anything_<version>_windows_amd64.zip` | Zip Archive (`.zip`) |

All release artifacts are built using pure Go (`CGO_ENABLED=0`) with `-trimpath` flags and cryptographically signed using keyless [Sigstore Cosign](https://github.com/sigstore/cosign).

---

## 1. Quick Install via Prebuilt Binaries

### Linux & macOS (Bash / Zsh)

Run the following commands to download, verify, and install the latest `go-anything` binary:

```bash
VERSION="1.0.0"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

RELEASE_URL="https://github.com/mohammedrefat/go-anything/releases/download/v${VERSION}"
ARTIFACT="go-anything_${VERSION}_${OS}_${ARCH}.tar.gz"

# 1. Download binary archive, checksums, and signature bundle
curl -fsSL -O "${RELEASE_URL}/${ARTIFACT}"
curl -fsSL -O "${RELEASE_URL}/checksums.txt"
curl -fsSL -O "${RELEASE_URL}/checksums.txt.sigstore.json"

# 2. Cryptographically verify signature with Cosign (Keyless Sigstore)
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "^https://github\\.com/mohammedrefat/go-anything/\\.github/workflows/release\\.yml@refs/tags/v.*$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt

# 3. Verify SHA-256 Checksum
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum --ignore-missing -c checksums.txt
else
  shasum -a 256 --ignore-missing -c checksums.txt
fi

# 4. Extract and move binary to /usr/local/bin
tar -xzf "${ARTIFACT}" go-anything
sudo install -m 0755 go-anything /usr/local/bin/go-anything
rm -f go-anything "${ARTIFACT}" checksums.txt checksums.txt.sigstore.json

# 5. Verify installation
go-anything version
```

---

### Windows (PowerShell)

In an elevated or user PowerShell terminal:

```powershell
$Version = "1.0.0"
$Arch = "amd64"
$ReleaseUrl = "https://github.com/mohammedrefat/go-anything/releases/download/v$Version"
$Artifact = "go-anything_${Version}_windows_${Arch}.zip"

# 1. Download release archive, checksums, and signature bundle
Invoke-WebRequest -Uri "$ReleaseUrl/$Artifact" -OutFile $Artifact
Invoke-WebRequest -Uri "$ReleaseUrl/checksums.txt" -OutFile "checksums.txt"
Invoke-WebRequest -Uri "$ReleaseUrl/checksums.txt.sigstore.json" -OutFile "checksums.txt.sigstore.json"

# 2. Cryptographically verify signature with Cosign (if cosign is installed)
if (Get-Command cosign -ErrorAction SilentlyContinue) {
    cosign verify-blob `
      --bundle checksums.txt.sigstore.json `
      --certificate-identity-regexp "^https://github\.com/mohammedrefat/go-anything/\.github/workflows/release\.yml@refs/tags/v.*$" `
      --certificate-oidc-issuer "https://token.actions.githubusercontent.com" `
      checksums.txt
}

# 3. Verify SHA-256 Checksum
$expectedHash = (Get-Content checksums.txt | Select-String $Artifact).Line.Split(" ")[0].ToLower()
$actualHash = (Get-FileHash -Algorithm SHA256 $Artifact).Hash.ToLower()
if ($expectedHash -ne $actualHash) {
    throw "Security Check Failed: SHA-256 checksum mismatch for $Artifact!"
}
Write-Host "Checksum verified: $actualHash" -ForegroundColor Green

# 4. Extract archive
Expand-Archive -Path $Artifact -DestinationPath .\go-anything-bin -Force

# 5. Move executable to a directory in your PATH (e.g. ~/bin or C:\Program Files\go-anything)
$targetDir = "$HOME\bin"
if (!(Test-Path $targetDir)) { New-Item -ItemType Directory -Path $targetDir }
Move-Item .\go-anything-bin\go-anything.exe "$targetDir\go-anything.exe" -Force
Remove-Item -Recurse -Force .\go-anything-bin, $Artifact, checksums.txt, checksums.txt.sigstore.json

# 6. Verify installation
go-anything version
```

---

## 2. Install via Go Toolchain (`go install`)

If you have Go 1.27+ installed:

```bash
# Install latest release binary into $GOPATH/bin
go install github.com/mohammedrefat/go-anything/cmd/go-anything@latest

# Verify installation
go-anything version
```

Ensure `$GOPATH/bin` (or `~/go/bin`) is included in your system `PATH`.

---

## 3. Build from Source

To compile the latest release or developmental branch locally:

```bash
git clone https://github.com/mohammedrefat/go-anything.git
cd go-anything

# Build stripped, reproducible binary with CGO disabled
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ./bin/go-anything ./cmd/go-anything

# Verify
./bin/go-anything version
```

---

## 4. Post-Installation Verification

Run the built-in diagnostic and health suite:

```bash
# Check version, default evidence profile, and available workers
go-anything version

# Run workspace diagnostic checks
go-anything doctor
```
