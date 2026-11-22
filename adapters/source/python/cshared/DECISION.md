# ADR / Evaluation Decision: Evaluation of CPython `c-shared` Transport (Task T6.4)

- **Date:** 2026-11-22
- **Status:** **Deferred / Experimental**
- **Decision:** Reject `c-shared` as a production transport for `go-anything` Python migrations. Keep `Subprocess` and `HTTP` as the exclusive production-grade transports.

---

## 1. Context and Problem Statement

In Task T6.1 and T6.4 of Milestone M6, we evaluated embedding compiled Go code directly into CPython using Go's `-buildmode=c-shared` build mode (calling exported Go functions via `ctypes` or `cffi`).

The initial theoretical appeal of `c-shared` was avoiding process-boundary serialization latency for high-throughput library calls. However, embedding the multi-threaded Go runtime inside the single-process CPython interpreter creates severe architectural hazards that violate `go-anything`'s core requirements for determinism, stability, and fail-closed safety.

---

## 2. Technical Evaluation of Core Hazards

Our evaluation test suite (`adapters/source/python/cshared/`) analyzed three fatal hazard dimensions:

### Hazard 1: POSIX `fork()` Without `exec()` Deadlocks (Severity: FATAL)

- **Root Cause:** The Go runtime is fundamentally multi-threaded from the moment of initialization. It immediately spawns OS background threads for garbage collection, `sysmon`, timer management, and OS thread polling.
- **POSIX Semantics:** Under POSIX standards, `fork()` duplicates *only* the calling OS thread in the child process. Any mutex, futex, channel lock, or heap allocator lock held by any other thread in the parent remains held forever in the child process.
- **Python Ecosystem Impact:** The Python production ecosystem relies heavily on pre-fork worker models:
  - **Gunicorn** (`gunicorn -w 4 app:app`) initializes libraries in the master process before calling `os.fork()` to spawn worker processes.
  - **Celery** defaults to a prefork concurrency pool using `os.fork()`.
  - **uWSGI** and Python `multiprocessing.Pool` default to `fork` on Linux.
- **Consequence:** The first time any forked child process attempts an allocation (`malloc`, `mheap`, `mcache`) or enters a Go runtime lock held during `fork()`, the child worker process deadlocks indefinitely (`futex_wait`). This causes silent, unrecoverable worker hangs in production.

### Hazard 2: CPython GIL Interaction & Thread-State Incompatibilities (Severity: HIGH)

- **Root Cause:** CPython relies on the Global Interpreter Lock (GIL) to synchronize memory access across Python threads. Go's M:N scheduler multiplexes goroutines over arbitrary OS threads (`M:N`).
- **Failure Modes:**
  1. **Thread Starvation:** When Python calls Go via `ctypes`/`cffi`, the calling thread holds the GIL. If Go performs network I/O, disk writes, or GC cycles without releasing the GIL (`Py_BEGIN_ALLOW_THREADS`), all other Python threads in the application freeze.
  2. **ThreadState Corruption:** If a Go goroutine attempts to invoke a Python callback, it must obtain a valid `PyThreadState` via `PyGILState_Ensure()`. Because the Go scheduler can dynamically migrate a goroutine to a different OS thread between scheduling points, thread-local state and GIL tracking become corrupted unless pinned with `runtime.LockOSThread()`.
  3. **AB-BA Deadlocks:** If Go code holds a Go mutex and attempts to acquire the GIL while a Python thread holds the GIL and waits on a Go mutex, a classic circular deadlock occurs.

### Hazard 3: Signal Handling Conflicts (Severity: HIGH)

- **Root Cause:** The Go runtime installs low-level signal handlers for `SIGSEGV`, `SIGBUS`, `SIGFPE`, `SIGPROF`, and `SIGPIPE` upon initialization (using `SA_ONSTACK`) to support nil-pointer detection and preemption (`SIGURG`).
- **Failure Modes:**
  1. CPython registers signal handlers for `SIGINT` (to raise `KeyboardInterrupt`) and `SIGTERM`.
  2. When CPython overrides Go's signal mask, Go's nil-pointer detection crashes with an immediate SIGSEGV core dump instead of a recoverable panic.
  3. Conversely, if Go intercepts `SIGINT`, Python command-line tools and CLI apps become unresponsive to `Ctrl+C`.
  4. Profilers (`cProfile`, `py-spy`, `pprof`) conflict over `SIGPROF` timer ticks.

---

## 3. Decision: Subprocess and HTTP Transports Certified as Production-Grade

We certify **`Subprocess`** and **`HTTP`** as the exclusive production-grade transports for Python migrations:

| Capability / Attribute | `Subprocess` / `HTTP` (Production) | `c-shared` (Experimental / Rejected) |
| :--- | :--- | :--- |
| **Process Isolation** | Strict OS process boundary; crash in one cannot corrupt the other | Single address space; memory corruption crashes entire service |
| **`fork()` Safety** | Fully safe; processes spawned cleanly with `exec` / fresh address space | Fatal deadlocks in Gunicorn, Celery, and prefork workers |
| **Concurrency / GIL** | Full multi-core parallel dispatch (`runtime.NumCPU()`) | Serialized by single-process GIL contention |
| **Signal Handling** | Clean, isolated POSIX signal domains | Collisions over `SIGSEGV`, `SIGINT`, and `SIGPROF` |
| **Memory Ownership** | Decoupled; caller-allocates / serialized value-passing | Incompatible GC runtimes (Refcount vs Mark-Sweep) |
| **Fail-Closed Redaction**| In-memory redaction enforced before process boundary | Difficult to audit internal in-memory object references |

---

## 4. Preconditions for Re-evaluating `c-shared`

`c-shared` will remain strictly experimental until all of the following conditions are satisfied:
1. **Free-Threaded CPython:** CPython adopts PEP 703 (free-threaded Python without the GIL) as a stable, default production configuration.
2. **Elimination of `fork()`:** Python application servers and task queues completely deprecate `os.fork()` in favor of `posix_spawn` or multiprocessing `spawn` context.
3. **Formal C-ABI Sandbox:** Memory isolation is guaranteed by an intermediary memory sandbox (such as WebAssembly / WASI via `wazero`) rather than direct in-process C pointer passing.
