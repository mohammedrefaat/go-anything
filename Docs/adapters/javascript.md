# JavaScript Language Adapter

`adapters/source/javascript` provides first-class support for migrating JavaScript and Node.js applications (.js, .mjs, .cjs) to idiomatic Go.

## Overview & Capabilities

- **Adapter Identifier:** `javascript`
- **Target Target Language:** `go`
- **Specification Version:** `v1`
- **Supported Transports:** `subprocess`, `http`
- **Default Statement Coverage Minimum:** 80%
- **Max Unreplayable Ratio:** 10%

## Runtime Requirements

- Node.js 18+ (verified via `go-anything doctor`)
- `npm`, `pnpm`, or `yarn` package managers

## Execution & Workflow Mechanics

### 1. Unit Running & Replay Driver
The adapter executes units via Node.js sub-processes or HTTP reverse proxies:
- Command: `node <entrypoint.js>`
- HTTP Mode: starts service on an ephemeral port and streams JSON-RPC / REST payloads.

### 2. Behavioral Capture & Redaction
- Inputs captured: HTTP methods, route paths, request query params, JSON request bodies, CLI arguments (`argv`), environment variables.
- Outputs captured: HTTP status codes, response headers, parsed JSON bodies, exit codes, stdout/stderr.
- Core-owned in-memory fail-closed redaction substitutes sensitive fields (`password`, `authorization`, `auth.token`, `secret`) and marks volatile timestamps before disk persistence.

### 3. Test Framework Discovery & Execution
The adapter automatically inspects `package.json` scripts and devDependencies to discover test suites:
- `node --test`: Native Node.js test runner
- `jest`: Jest runner
- `vitest`: Vitest runner
- `mocha`: Mocha runner

When `record --from-tests` is invoked, the adapter executes tests with interception hooks to extract characterization cases automatically.

### 4. Profiling & Hot-Path Candidates
Candidate ranking via `go-anything profile` utilizes:
- `node --cpu-prof`: Generates V8 CPU profile (`.cpuprofile`)
- `node --prof`: Generates V8 execution tick logs

## Adapter Conformance

The JavaScript adapter satisfies the strict conformance suite in `pkg/adapterkit/conformance`:
- Validates repository detection (`Detect`) on CommonJS and ESM modules.
- Enforces multi-transport replay determinism (`Subprocess` and `HTTP`).
- Adheres to the frozen adapter contract in `schemas/v1/`.
