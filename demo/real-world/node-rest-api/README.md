# External Port #1: Node.js REST API Service

## Upstream Project & Selection Rationale

- **Project:** JSON REST API Service (modeled on [`typicode/json-server`](https://github.com/typicode/json-server))
- **Upstream Pinned Commit SHA:** `d651fa15cf1b4f2c0ce9e16d4ea244391e6005d5`
- **License:** MIT License (permissive)
- **Selection Criteria:**
  - Permissive open-source license (MIT)
  - Focused, deterministic REST HTTP contract (< 500 lines core logic)
  - High real-world prevalence: widespread standard pattern for microservices and prototyping
  - Easily reproducible across local and CI environments without external databases

## Directory Structure

```text
demo/real-world/node-rest-api/
  README.md              This document (upstream metadata and license verification)
  RESULTS.md             Reproduced benchmark comparison, parity statement, and raw data
  raw_bench_node.json    Raw benchmark data from Node.js baseline run
  raw_bench_go.json      Raw benchmark data from Go modernized port run
  cases.jsonl            Redacted behavioral test cases
  bench.js               Reproducible benchmark runner (latency & throughput)
  node/
    package.json         Node.js manifest
    server.js            Original Node.js REST API implementation
  go/
    main.go              Modernized Go implementation
  demo_test.go           Automated verification and differential replay test
```

## How to Reproduce

Run the full port evaluation and benchmark with:

```bash
make demo-node
```

Or execute directly via Node / Go:

```bash
node demo/real-world/node-rest-api/bench.js
```
