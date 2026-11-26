# `go-anything` npm Package

Cross-platform npm installer for the `go-anything` application migration engine.

## Installation

```bash
npm install -g go-anything
```

Or run via `npx`:

```bash
npx go-anything --help
```

## Security & Trust Model

1. **Cryptographic Checksum Verification:** The `postinstall` script downloads the matching prebuilt binary for your operating system directly from GitHub Releases (`https://github.com/mohammedrefaat/go-anything/releases`).
2. **Fail-Closed SHA-256 Integrity:** The downloaded archive is hashed using SHA-256 and verified against the official `checksums.txt` digest published in the release before extraction. Any hash mismatch aborts the installation.
3. **Cosign Attestation:** The published binaries and `checksums.txt` are signed using keyless Cosign via GitHub Actions OIDC.
4. **Local Execution:** The installed binary runs locally with zero telemetry and fail-closed secret redaction.
