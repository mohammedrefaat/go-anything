# ADR-001: Restricted Execution Sandbox Backend

* **Status:** Resolved
* **Date:** 2026-11-19
* **Deciders:** go-anything core team / task T0.0
* **Consulted:** Architecture Plan v3, Task Plan v3.1

---

## Context

`go-anything` automates migrating legacy applications to Go by recording and replaying actual system behavior against test cases (Milestone 1, Task T0.5). During case recording and differential fuzzing, `go-anything` executes the original application (which may be legacy Go, Python, Node.js, Ruby, etc.) and analyzes its input/output characteristics.

Executing untrusted or legacy code introduces inherent security risks. Execution Rule #3 mandates **Fail-closed security**:
1. No outbound network access by default (network enabled only when explicitly opted in via `--allow-network`).
2. Environment isolation using a strict allowlist (stripping host secrets, API keys, and unlisted environment variables).
3. Temporary working copy filesystem isolation (writes must not mutate the parent repository or host files).
4. Resource constraints (CPU, memory, and timeout limits to prevent runaway or fork-bomb processes).

Different host operating systems provide varying levels of unprivileged process isolation:
- **Linux:** Unprivileged user namespaces, network namespaces, mount namespaces, and tools like Bubblewrap (`bwrap`).
- **macOS:** Deprecated `sandbox-exec` (Seatbelt); no unprivileged kernel namespaces.
- **Windows:** Job Objects provide CPU/memory limits, but no lightweight kernel-level network or filesystem namespace isolation without Hyper-V/WSL2/containers.
- **Container Runtimes (Docker / Podman):** High isolation across all platforms, but require external daemons, root/service privileges, container images, and heavy startup latency that degrades developer experience and loop speed.

We must decide the execution sandbox backend architecture that balances security, developer velocity, platform portability, and zero-telemetry local operation.

---

## Alternatives Considered

### Alternative 1: Direct Linux Namespaces (`unshare` / Go `syscall.SysProcAttr`)
- **Mechanism:** Use Linux kernel namespaces directly (`CLONE_NEWNET`, `CLONE_NEWPID`, `CLONE_NEWNS`, `CLONE_NEWIPC`, `CLONE_NEWUTS`) via Go's `syscall.SysProcAttr.Cloneflags` or invoking `unshare`.
- **Pros:**
  - Zero external binary or daemon dependencies on standard Linux.
  - Extremely fast process startup (<5ms overhead).
  - Strong kernel-enforced network and filesystem isolation without root when unprivileged user namespaces (`CLONE_NEWUSER`) are permitted.
- **Cons:**
  - Linux-only.
  - Some hardened Linux distributions (e.g., Debian/RHEL sysctl configurations) disable unprivileged user namespaces by default (`kernel.unprivileged_userns_clone = 0`).

### Alternative 2: Bubblewrap (`bwrap`)
- **Mechanism:** Invoke the unprivileged sandboxing tool `bwrap` (widely used by Flatpak and modern desktop tools) to construct bind mounts, drop network, and configure isolated namespaces.
- **Pros:**
  - Standard, battle-tested unprivileged sandbox utility on Linux.
  - Gracefully handles setuid-root fallback on distributions where unprivileged user namespaces are disabled.
  - Declarative CLI flags for filesystem binds and network isolation (`--unshare-net`, `--ro-bind`, `--tmpfs`).
- **Cons:**
  - Linux-only.
  - Requires the `bwrap` binary to be installed on the host.

### Alternative 3: Container Runtimes (Docker / Podman / containerd)
- **Mechanism:** Run every sandboxed execution inside a container.
- **Pros:**
  - Consistent runtime isolation on Linux, macOS, and Windows.
  - Mature ecosystem for cgroup resource limits and network disabling (`--network none`).
- **Cons:**
  - Heavyweight: requires a running daemon (Docker/Podman VM on macOS/Windows).
  - High process startup latency (seconds vs milliseconds), severely degrading differential fuzzing and recorded case replay.
  - Complicates local developer onboarding and breaks the "download binary and run" zero-infrastructure promise.

### Alternative 4: Platform-Native macOS/Windows Sandboxing APIs
- **Mechanism:** Use macOS `sandbox-exec` / Seatbelt profiles and Windows AppContainer / Job Objects.
- **Pros:**
  - Native to the respective OS.
- **Cons:**
  - Apple has formally deprecated `sandbox-exec`; writing `.sb` Scheme profiles is undocumented, unsupported, and brittle across macOS versions.
  - Windows AppContainer requires complex ACL setup and does not cleanly isolate loopback network traffic without administrative firewall manipulation.
  - High maintenance burden with fragile security boundaries.

### Alternative 5: Process-Level Best-Effort Runner with Environment & Filesystem Scoping
- **Mechanism:** Run the process using standard `os/exec` with an environment allowlist, temporary scratch directory as working copy, strict execution timeout, and process group termination.
- **Pros:**
  - Uniform implementation across all operating systems without elevated privileges.
  - Zero external dependencies.
- **Cons:**
  - Does not provide true kernel-enforced network namespace blocking on non-Linux hosts.

---

## Decision

We adopt a **tiered sandbox backend** centered on native Linux namespaces with `bubblewrap` compatibility, accompanied by an explicit, transparent best-effort fallback on macOS and Windows:

1. **Linux (Tier 1 - Strict Kernel Isolation):**
   - The primary sandbox runner uses Linux namespaces (`unshare` via `syscall.SysProcAttr` with `CLONE_NEWNET`, `CLONE_NEWNS`, `CLONE_NEWPID`, `CLONE_NEWIPC`, `CLONE_NEWUSER`).
   - If unprivileged user namespaces are disabled by host sysctl, the runner detects and delegates to `bubblewrap` (`bwrap`) if available.
   - Network isolation is enforced at the kernel layer (loopback down unless `--allow-network` is specified).
   - Filesystem is isolated with a private mount namespace and ephemeral tmpfs working copy.

2. **macOS & Windows (Tier 2 - Best-Effort Isolation with Explicit Warning):**
   - On macOS and Windows, where unprivileged kernel namespaces are unavailable without running a container virtual machine, the sandbox operates in **best-effort fallback mode**.
   - **Environment Isolation:** Strips all unlisted environment variables, specifically dropping host secrets, cloud credentials, tokens, and SSH/GPG keys.
   - **Working Copy Isolation:** Creates an isolated temporary directory for the execution copy; mutations do not reach the repository workspace.
   - **Network Mitigation & Warning:**
     - Prints a prominent warning to `stderr`:
       ```text
       [WARN] Sandbox running in best-effort mode: OS-level network isolation is not supported natively on this platform without virtualization. Ensure repository code is trusted.
       ```
     - For processes running without `--allow-network`, standard proxy environment variables (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `http_proxy`, `https_proxy`, `all_proxy`) are set to invalid loopback sinks (`127.0.0.1:0`).
   - **Process & Resource Control:** Enforces strict execution deadlines via `context.WithTimeout`. On Windows, processes are assigned to a Job Object (or killed via `taskkill /T /F` process tree termination); on macOS/Unix, process groups are terminated with `SIGKILL`.

3. **No Mandatory Container Daemon:**
   - Docker and Podman will **not** be required for standard CLI operation. Containerized runners may be added as an optional external execution target in future releases, but core M0/M1 workflows remain lightweight and zero-dependency.

---

## Consequences

### Positive
- **Instant Developer Onboarding:** Works immediately out of the box on Linux, macOS, and Windows without installing Docker, configuring daemons, or requesting sudo privileges.
- **Strong Security Where Supported:** Linux users and CI environments receive full, fail-closed network and filesystem namespace isolation.
- **Clear Threat Model & Transparency:** Clear warning on macOS/Windows ensures users understand that executing original code requires repository trust (`SECURITY.md` reflects this model).
- **Fast Execution:** Microsecond process startup enables high-throughput recording and fuzzing across hundreds of test cases.

### Negative / Trade-offs
- **Incomplete Network Block on macOS/Windows:** A malicious payload on macOS or Windows running without `--allow-network` could theoretically open raw sockets if it ignores proxy environment variables. This is mitigated by documenting the repository trust requirement in `SECURITY.md` and recommending Linux / CI for untrusted code evaluation.
- **Platform-Specific Go Build Tags:** `internal/sandbox` requires platform-specific implementations (`sandbox_linux.go`, `sandbox_darwin.go`, `sandbox_windows.go`).

---

## Implementation Guidance for Task T0.5
- Package: `internal/sandbox`
- Implement interface `Runner` with methods `Run(ctx context.Context, cfg Config) (*Result, error)`.
- Enforce `--allow-network` flag: when false, instantiate network namespace isolation on Linux, or null-proxy routing and best-effort warning on Darwin/Windows.
- Implement temp dir cleanup and timeout enforcement via process groups.
