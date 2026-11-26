# Hot-Path Sidecar Builds

The hot-path pipeline compiles and vets generated Go sidecars (`bridges/<unit>/`) inside `internal/sandbox`.

## Offline & Sandboxed Execution

Both `go build` (during `ReplayAndDiff`) and `go vet` (during `RunGate`) run with:

- `AllowNetwork: false` (OS-level network namespace on Linux; best-effort proxy sink + warning on tier-2 platforms)
- `GOFLAGS=-mod=readonly`
- `GOTOOLCHAIN=local`
- `GOPROXY=off`

## Prerequisites: Vendoring or Warm Module Cache

Because network access, `go.mod` mutations, and automatic toolchain downloads are disabled during sidecar builds, the workspace must satisfy one of:

1. **Vendored dependencies:** run `go mod vendor` before running the hot-path pipeline, or
2. **Warm local module cache:** ensure all required modules are already present in `GOMODCACHE` (for example via `go mod download`) and the locally installed Go toolchain satisfies `go.mod`.

## Platform Isolation Tiers

- **Linux (Tier 1):** Runs inside user/network/mount/PID namespaces (or `bwrap` fallback) with outbound networking blocked at the kernel level.
- **Windows / macOS (Tier 2):** Runs in best-effort mode with environment filtering and invalid proxy variables, and prints `sandbox.BestEffortWarning` to `stderr`.
