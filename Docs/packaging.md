# Packaging & Distribution

`go-anything` is distributed as signed, cross-platform standalone binaries with zero external runtime dependencies.

---

## Release Pipeline

Every official release of `go-anything` is built and published automatically via GitHub Actions using **GoReleaser**:
- Cross-compiled for Linux (`amd64`, `arm64`), macOS (`amd64`, `arm64`), and Windows (`amd64`).
- Cryptographically signed with **Cosign** (keyless OIDC via GitHub).
- Generates reproducible `checksums.txt` SHA-256 digests.
- Automated SBOM generation is planned for `v2.2.0` (see [GO_ANYTHING_FIX_AND_SECURITY_PLAN.md](GO_ANYTHING_FIX_AND_SECURITY_PLAN.md)).

---

## Installation Channels

### 1. Homebrew (macOS & Linux)
```bash
brew tap mohammedrefaat/tap
brew install go-anything
```

### 2. Scoop (Windows)
```powershell
scoop bucket add go-anything https://github.com/mohammedrefaat/scoop-bucket.git
scoop install go-anything
```

### 3. Prebuilt GitHub Release Binaries
Download the matching archive for your operating system from [Releases](https://github.com/mohammedrefaat/go-anything/releases):
```bash
# Verify checksum
sha256sum -c checksums.txt

# Verify signature with cosign
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "^https://github\.com/mohammedrefaat/go-anything/\.github/workflows/release\.yml@refs/tags/v.*$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

### 4. Direct Go Install
```bash
go install github.com/mohammedrefaat/go-anything/cmd/go-anything@latest
```
*(Matches the `github.com/mohammedrefaat/go-anything` module path in `go.mod`; v2+ Go module installs require a `/v2` module path suffix.)*

---

## Versioning Policy

- We strictly adhere to **Semantic Versioning 2.0.0**.
- Schemas in `schemas/v1/` are frozen and permanent.
- Extensions and command output contracts in `schemas/v2/` maintain backwards compatibility.
