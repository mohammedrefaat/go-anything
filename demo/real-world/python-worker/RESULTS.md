# Benchmark Results: Python Worker / CLI vs Modernized Go Port

## Environment & Versions

- **Host OS:** Windows (x86_64 / amd64)
- **Python:** 3.15.2
- **Go:** go1.26+
- **Upstream Project:** Python Stream & Batch Task Processor (RQ/Celery style worker)
- **Pinned Commit SHA:** `9f1d82acbf267bb894d01b63ef293b6e8281a1a7`
- **Benchmark Iterations:** 200 process-level task executions across mixed compute jobs (`calc_stats`, `hash_payload`, `filter_records`)
- **Raw Data Files:**
  - [`raw_bench_python.json`](raw_bench_python.json)
  - [`raw_bench_go.json`](raw_bench_go.json)

## Parity Scope Statement (Rule R-6)

> **MANDATORY SCOPE NOTICE (Rule R-6):**
> Behavioral parity is claimed **strictly and only** for recorded and fuzzed inputs (`cases.jsonl`).
> Unobserved tasks, undocumented payload shapes, or arbitrary CLI flags outside the recorded cases are explicitly **not** claimed to be equivalent.

## Behavioral Verification

- **Total Recorded Cases:** 8 cases (captured via `record --from-tests` from `test_worker.py` and CLI runs)
- **Covered Tasks:** `calc_stats`, `hash_payload` (SHA256, MD5, SHA1), `filter_records`, and unknown task error handling
- **Replay Verification Outcome:** 100% match (0 mismatches across all 8 cases)

## Measured Metrics (Actual Reproduced Run)

| Metric | Python Baseline (`worker.py`) | Modernized Go Port (`main.go`) | Improvement |
| :--- | :--- | :--- | :--- |
| **p50 Latency** | 84.159 ms | 14.796 ms | **5.69x faster** |
| **p95 Latency** | 102.645 ms | 22.649 ms | **4.53x faster** |
| **p99 Latency** | 107.603 ms | 33.862 ms | **3.18x faster** |
| **Throughput** | 11 tasks/sec | 63 tasks/sec | **+472% throughput (5.7x)** |

*Note: All numbers above are measured and reproduced directly from local benchmark runs. No extrapolations.*

## How to Reproduce

Execute the single-command reproduction:

```bash
make demo-python
```

Or execute via Python:

```bash
python demo/real-world/python-worker/bench.py
```
