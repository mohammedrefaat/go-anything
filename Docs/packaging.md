# Packaging & Distribution

`go-anything` is distributed as signed, cross-platform standalone binaries with zero external runtime dependencies.

---

## Release Pipeline

Every official release of `go-anything` is built and published automatically via GitHub Actions using **GoReleaser**:
- Cross-compiled for Linux (amd64, arm64), macOS (amd64, arm64), and Windows (amd64, arm64).
- Cryptographically signed with **Cosign** (keyless OIDC via GitHub).
- Generates reproducible `checksums.txt` SHA-256 digests.
- Software Bill of Materials (SBOM) produced via `syft` in SPDX/CycloneDX format.

---

## Installation Channels

### 1. Homebrew (macOS & Linux)
```bash
brew tap mohammedrefat/tap
brew install go-anything
```

### 2. Scoop (Windows)
```powershell
scoop bucket add go-anything https://github.com/mohammedrefat/scoop-bucket.git
scoop install go-anything
```

### 3. Prebuilt GitHub Release Binaries
Download the matching archive for your operating system from [Releases](https://github.com/mohammedrefat/go-anything/releases):
```bash
# Verify checksum
sha256sum -c checksums.txt

# Verify signature with cosign
cosign verify-blob \
  --certificate-identity https://github.com/mohammedrefat/go-anything/.github/workflows/release.yml@refs/tags/v2.0.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --signature go-anything_2.0.0_linux_amd64.tar.gz.sig \
  go-anything_2.0.0_linux_amd64.tar.gz
```

### 4. Direct Go Install
```bash
go install github.com/mohammedrefat/go-anything/cmd/go-anything@v2.0.0
```

---

## Versioning Policy

- We strictly adhere to **Semantic Versioning 2.0.0**.
- Schemas in `schemas/v1/` are frozen and permanent.
- Extensions and command output contracts in `schemas/v2/` maintain backwards compatibility.
