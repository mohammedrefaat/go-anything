# ADR-007: Canonical Owner Identity and Module Path Resolution

* **Status:** Resolved
* **Date:** 2026-11-24
* **Deciders:** Mohammed Refaat / go-anything core team
* **Consulted:** Architecture Plan v3, Docs/GO_ANYTHING_FIX_AND_SECURITY_PLAN.md (Finding F1, Task A1)

---

## 1. Context & Problem Statement

Earlier in the project lifecycle, the codebase, module definition, documentation, package management configs (Homebrew, Scoop, npm), and release signing configurations used the GitHub handle `mohammedrefat` (single 'a'). However, the canonical GitHub account and repository location is:
`https://github.com/mohammedrefaat/go-anything` (`mohammedrefaat` with two 'a's).

This mismatch introduced several critical defects (P0):
1. **Module Installation Failure:** `go install github.com/mohammedrefat/go-anything/cmd/go-anything@latest` failed because the remote GitHub repository is located at `mohammedrefaat/go-anything`, violating Go module path resolution.
2. **Git Remote Refusal:** Unauthenticated `git ls-remote https://github.com/mohammedrefat/go-anything` and `git clone` failed because the legacy path does not exist.
3. **Cryptographic Release Verification Failure:** Sigstore Cosign verification commands documented across `README.md` and `Docs/install.md` verified against `--certificate-identity-regexp` matching `mohammedrefat`, which fails against real release assets signed via GitHub Actions OIDC tokens issued under `mohammedrefaat/go-anything`.
4. **Package Distribution Desynchronization:** Homebrew formula, Scoop bucket, and npm packages pointed to non-existent or conflicting repository namespaces.

---

## 2. Decision

We establish `mohammedrefaat` as the single canonical owner identity and module path for the project:

1. **Go Module Path:**
   - Update `go.mod` module line to `module github.com/mohammedrefaat/go-anything`.
   - Mechanically rewrite all import paths across all Go packages and subpackages (`cmd/`, `internal/`, `pkg/`, `adapters/`, `tools/`, `demo/`) to `github.com/mohammedrefaat/go-anything/...`.

2. **Cosign Certificate Identity Regular Expression:**
   - Standardize all release verification instructions in `README.md`, `README.ar.md`, `Docs/install.md`, `Docs/packaging.md`, and case studies to match the canonical GitHub Actions OIDC workflow identity:
     `^https://github\.com/mohammedrefaat/go-anything/\.github/workflows/release\.yml@refs/tags/v.*$`

3. **Package Distribution & Metadata:**
   - Update `.goreleaser.yaml` to set `owner: mohammedrefaat` for `brews` and `scoops`, and update `homepage` and repository URLs.
   - Update `packaging/npm/package.json` repository URLs and `packaging/npm/scripts/install.js`.
   - Update all documentation files (`README.md`, translations, `Docs/`, `AGENTS.md`, `CLAUDE.md`, `GOVERNANCE.md`, `SECURITY.md`) to reference `mohammedrefaat`.

4. **Automated Regression Guard:**
   - Add a regression test in `cmd/go-anything/owner_test.go` that traverses the repository and asserts zero occurrences of the legacy handle `mohammedrefat` exist across tracked files, allow-listing only this ADR document and `DECISIONS.md`.

---

## 3. Consequences

### Positive
- `go install github.com/mohammedrefaat/go-anything/cmd/go-anything@latest` succeeds out-of-the-box.
- Git clone commands, release download URLs, and GitHub links point to the active canonical repository.
- Keyless Sigstore Cosign signature verification succeeds against authentic GitHub Actions release attestations.
- Complete consistency across code, schemas, documentation, and package manifests.
- Regressions are automatically blocked by CI via `owner_test.go`.

### Negative / Neutral
- Downstream forks or local development checkouts must update their git remote URLs to `https://github.com/mohammedrefaat/go-anything.git`.
