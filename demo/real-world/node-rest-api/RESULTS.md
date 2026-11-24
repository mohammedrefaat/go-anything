# Benchmark Results: Node.js REST API Service vs Modernized Go Port

## Environment & Versions

- **Host OS:** Windows (x86_64 / amd64)
- **Node.js:** v26.12.1
- **Go:** go1.26+
- **Upstream Project:** `typicode/json-server` (MIT License)
- **Pinned Commit SHA:** `d651fa15cf1b4f2c0ce9e16d4ea244391e6005d5`
- **Benchmark Iterations:** 500 requests across mixed endpoints (`/health`, `/items`, `/items/1`, `/items/999`, `POST /items`)
- **Raw Data Files:**
  - [`raw_bench_node.json`](raw_bench_node.json)
  - [`raw_bench_go.json`](raw_bench_go.json)

## Parity Scope Statement (Rule R-6)

> **MANDATORY SCOPE NOTICE (Rule R-6):**
> Behavioral parity is claimed **strictly and only** for recorded and fuzzed inputs (`cases.jsonl`).
> Unobserved endpoints, arbitrary query parameter semantics, or undocumented header manipulations outside the recorded cases are explicitly **not** claimed to be equivalent.

## Behavioral Verification

- **Total Recorded Cases:** 9 cases
- **Happy Path Cases:** 5 cases (`GET /health`, `GET /items`, `GET /items/1`, `POST /items`, `PUT /items/1`, `DELETE /items/2`)
- **Edge & Error Cases:** 4 cases (missing ID 404, invalid payload 400, unknown endpoint 404)
- **Replay Verification Outcome:** 100% match (0 mismatches across all 9 cases)

## Measured Metrics (Actual Reproduced Run)

| Metric | Node.js Baseline (`server.js`) | Modernized Go Port (`main.go`) | Improvement |
| :--- | :--- | :--- | :--- |
| **p50 Latency** | 0.368 ms | 0.266 ms | **1.38x faster** |
| **p95 Latency** | 0.526 ms | 0.371 ms | **1.42x faster** |
| **p99 Latency** | 0.843 ms | 0.462 ms | **1.82x faster** |
| **Throughput** | 2,538 req/sec | 3,596 req/sec | **+41.7% throughput** |

*Note: All numbers above are measured and reproduced directly from local benchmark runs. No extrapolations.*

## How to Reproduce

Execute the single-command reproduction:

```bash
make demo-node
```

Or via Node.js script:

```bash
node demo/real-world/node-rest-api/bench.js
```
