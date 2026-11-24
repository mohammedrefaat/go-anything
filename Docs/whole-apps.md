# Migrating Whole Applications

While `go-anything` focuses on porting code **one unit at a time**, entire monolithic codebases and complex microservices can be systematically modernized using structured waves.

---

## Strategy: The Multi-Wave Strangler Fig

```text
[ Legacy Application Monolith ]
   |
   +-- Wave 0: Leaf Utilities & Domain Math (pure stateless functions)
   +-- Wave 1: Data Models & Persistence Adapters
   +-- Wave 2: Business Logic & Processing Pipelines
   +-- Wave 3: HTTP API Handlers & Routing
   +-- Wave 4: Ingress Front-Door & Infrastructure Lift
```

---

## Step-by-Step Whole-Application Migration

### Phase 1: Assessment and Wave Graph Generation
Run `go-anything analyze` to generate the complete topological migration wave graph:
```bash
go-anything analyze --dir . --json > waves.json
```
Review the dependencies. Confirm that Wave 0 units have zero internal dependencies.

### Phase 2: Leaf Unit Migration (Wave 0)
Start at the foundation:
1. Claim a Wave 0 unit: `go-anything claim --unit-id pkg/token`
2. Record baseline cases: `go-anything record --from-tests`
3. Port the Go package: Write `pkg/token/token.go`
4. Verify diff: `go-anything diff --unit-id pkg/token`
5. Run gate: `go-anything gate --unit-id pkg/token`
6. Mark verified: `go-anything verify --unit-id pkg/token`

Repeat for all units in Wave 0. Once Wave 0 units are `Verified`, units in Wave 1 automatically unlock into `Ready` state.

### Phase 3: Strangler Bridge Integration
For services with deep internal call graphs, use `go-anything bridge` to generate sidecar bridges:
- Allows legacy code (Node.js, Python) to call ported Go functions over stdio/IPC.
- Enables progressive performance improvements in production before the entire application is ported.

### Phase 4: Ingress Front Door & Shadow Mode
When HTTP routes are ported:
1. Deploy the `go-anything` front-door reverse proxy in **Shadow Mode**.
2. Live customer traffic hits the legacy application as primary.
3. Requests are duplicated asynchronously to the Go implementation.
4. Divergences in status codes or payloads are logged to `.go-anything/reports/shadow_mismatches.jsonl`.

### Phase 5: Production Cutover & Final Lift
1. When shadow mode logs show 0 mismatches across thousands of live requests, the human operator issues cutover:
   ```bash
   go-anything approve --unit-id api/v1
   go-anything promote --unit-id api/v1
   ```
2. Primary traffic now flows directly to the Go service. Fallback traffic to legacy is monitored via metrics.
3. Once fallback traffic remains at zero for the cutover window (e.g. 7 days), finalize the modernization:
   ```bash
   go-anything lift --unit-id api/v1
   ```
4. The legacy bridge and original code are permanently removed. The migration is complete.
