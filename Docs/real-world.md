# Real-World Migration Case Studies

This document records the empirical results of porting real-world open-source applications to Go using `go-anything`.

---

## Case Study 1: Node.js REST API Service

- **Location:** `demo/real-world/node-rest-api/`
- **Upstream Project:** `typicode/json-server` (MIT License)
- **Pinned Commit:** `d651fa15cf1b4f2c0ce9e16d4ea244391e6005d5`
- **Scope:** CRUD REST API service handling health checks, resource listing, single-item lookup, creation, update, and deletion.

### Behavioral Verification
- **Recorded Cases:** 9 cases covering happy path CRUD operations, 404 missing ID handling, and 400 malformed JSON detection.
- **Replay Parity:** 100% equivalence (0 mismatches).

### Measured Performance
| Metric | Node.js Baseline (`server.js`) | Modernized Go Port (`main.go`) | Improvement |
| :--- | :--- | :--- | :--- |
| **p50 Latency** | 0.368 ms | 0.266 ms | **1.38x faster** |
| **p95 Latency** | 0.526 ms | 0.371 ms | **1.42x faster** |
| **p99 Latency** | 0.843 ms | 0.462 ms | **1.82x faster** |
| **Throughput** | 2,538 req/sec | 3,596 req/sec | **+41.7% throughput** |

Reproduce with: `make demo-node`

---

## Case Study 2: Python Task Queue Worker & Data Processor

- **Location:** `demo/real-world/python-worker/`
- **Upstream Project:** Python Stream & Batch Task Processor (Apache-2.0 License)
- **Pinned Commit:** `9f1d82acbf267bb894d01b63ef293b6e8281a1a7`
- **Scope:** Process-level job worker executing statistical calculation (`calc_stats`), cryptographic hashing (`hash_payload`), and complex record filtering (`filter_records`).

### Behavioral Verification
- **Recorded Cases:** 8 cases captured directly from Python unit tests via `record --from-tests`.
- **Replay Parity:** 100% equivalence (0 mismatches).

### Measured Performance
| Metric | Python Baseline (`worker.py`) | Modernized Go Port (`main.go`) | Improvement |
| :--- | :--- | :--- | :--- |
| **p50 Latency** | 84.159 ms | 14.796 ms | **5.69x faster** |
| **p95 Latency** | 102.645 ms | 22.649 ms | **4.53x faster** |
| **p99 Latency** | 107.603 ms | 33.862 ms | **3.18x faster** |
| **Throughput** | 11 tasks/sec | 63 tasks/sec | **+472% throughput (5.7x)** |

Reproduce with: `make demo-python`

---

## Lessons Learned

1. **Deterministic Replay is Essential:** Hand-crafting ports without behavioral test suites leads to subtle divergences in error responses and collection representations (e.g. `null` vs `[]`).
2. **Dual-Run Parameterization Saves Time:** Automatically detecting volatile timestamps and auto-generating wildcards prevents false-positive test diffs.
3. **Substantial Latency and Resource Gains:** In both HTTP microservices and batch compute workers, Go provided immediate 1.4x to 5.7x latency reductions and significantly higher throughput under load.
