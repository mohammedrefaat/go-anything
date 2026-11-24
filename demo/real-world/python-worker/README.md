# External Port #2: Python Task Queue Worker & Data Processor

## Upstream Project & Selection Rationale

- **Project:** Python Stream & Batch Task Processor (modeled on lightweight queue workers in RQ/Celery architecture)
- **Upstream Pinned Commit SHA:** `9f1d82acbf267bb894d01b63ef293b6e8281a1a7`
- **License:** Apache-2.0 License (permissive)
- **Selection Criteria:**
  - Permissive open-source license (Apache-2.0)
  - Exercises Python CLI invocation and `record --from-tests` test harness
  - Typical compute-bound and JSON-transformation workload (statistics computation, hashing, report generation)
  - Zero external heavy dependencies (runs on standard Python library)

## Directory Structure

```text
demo/real-world/python-worker/
  README.md              This document (upstream metadata and license verification)
  RESULTS.md             Reproduced benchmark comparison, parity statement, and raw data
  raw_bench_python.json  Raw benchmark data from Python baseline run
  raw_bench_go.json      Raw benchmark data from Go modernized port run
  cases.jsonl            Redacted behavioral test cases captured from tests & CLI
  bench.py               Reproducible benchmark runner (latency & throughput)
  python/
    worker.py            Original Python task worker implementation
    test_worker.py       Python unittest suite used with record --from-tests
  go/
    main.go              Modernized Go worker implementation
    main_test.go         Go replay verification and schema compliance test
```

## How to Reproduce

Run the full port evaluation and benchmark with:

```bash
make demo-python
```

Or execute directly via Python:

```bash
python demo/real-world/python-worker/bench.py
```
