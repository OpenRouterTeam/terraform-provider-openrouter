# DEV-856: observability destination perpetual drift

> The full DEV-856 investigation lives on branch `dev-856-observability-destination-defaults`. This file carries only the Task 4 section: the independent test of the flat-config-synchronization candidate (PR #204).

## Task 4: independent verdict on flat config synchronization (PR #204)

**Verdict: `#204 separate defect`** — and the candidate is not mergeable in any part as written.

The hypothesis behind #204 is that the generated `RefreshFromShared*` mappers populate the typed per-variant config but never repopulate the flat `config` map, so the map must be re-serialized from the API response in Create, Read, and Update. Testing each lifecycle stage in isolation shows the premise is accurate but the remedy is not: one claimed call site is actively harmful, one is unnecessary, and one targets a real defect that is distinct from the perpetual diff DEV-856 describes.

Tested at `dev-856-fix` tip `8a043d9508c90b23951d993f7a44d071c15cb137`, merge-base with `main` `525c8b68e96d32ce86153c574d83e3b1427461e2`. No rebase onto `main`; the candidate is judged as written on its branch.

### What the branch actually wires

The branch's commit subject claims synchronization "on Read/Create/Update". The code wires **one** call site:

| Function | Defined | Called |
| --- | --- | --- |
| `refreshFlatConfigFromCreateResponse` | yes | yes — `observabilitydestination_resource.go:2529` |
| `refreshFlatConfigFromGetResponse` | yes | no |
| `refreshFlatConfigFromUpdateResponse` | yes | no |

Unused methods do not fail `go build`, so the discrepancy between the commit message and the code is invisible to CI.

### Why synchronizing this attribute cannot work

`config` is declared `Required: true` with no `Computed`. It is practitioner-owned, so Terraform enforces that the value returned from `ApplyResourceChange` equals the planned value. Writing an API-derived config over it violates that contract and Terraform aborts the apply:

```text
Error: Provider produced inconsistent result after apply
... produced an unexpected new value: .config: inconsistent values for sensitive attribute.
```

The response config is never byte-identical to what the practitioner wrote, even when the API echoes the request verbatim. The generated SDK carries `default:` struct tags on the S3 config:

```go
PathTemplate *string `default:"{prefix}/{date}" json:"pathTemplate"`
Prefix       *string `default:"openrouter-traces" json:"prefix"`
```

These are materialized during `UnmarshalJSON`, so re-serializing the typed struct always emits `prefix` and `pathTemplate` — two keys absent from the customer's configuration. This is the same defaults-injection family that the accepted fix (#192) removed; synchronizing the flat map re-imports it through a different door.

### Evidence

Deterministic stub-server tests in `internal/acceptance/stub_repro_test.go`, one per lifecycle stage, run against an in-process `httptest` stand-in for `/observability/destinations`. Each row is the same suite under a different combination of call sites.

| Test | Stage isolated | Create sync on (as written) | Both off | Read sync only |
| --- | --- | --- | --- | --- |
| `...PerpetualDiff` | read-side normalization | pass | pass | **fail** — refresh plan not empty |
| `...S3ImmediatePlanAfterCreate` | create, customer S3 shape | **fail** — inconsistent result after apply | pass | **fail** — refresh plan not empty |
| `...CreateResponseNormalization` | create, API-normalized response | **fail** — inconsistent result after apply | pass | pass |
| `...UpdateConvergence` | update | pass | pass | **fail** — refresh plan not empty |
| `...ReadDetectsOutOfBandChange` | read, genuine remote mutation | **fail** — drift undetected | **fail** — drift undetected | pass |

Reading the table by call site:

1. **Create — unjustified and harmful.** The only wired call site turns a working apply into a hard protocol error for the customer's own S3 shape. Disabling it makes the S3 and create-normalization tests pass with no other change. This is strictly worse than the drift #192 already fixed.

2. **Update — unnecessary.** `...UpdateConvergence` passes without `refreshFlatConfigFromUpdateResponse`. Update converges on the plan's config while the API normalizes its read responses, so the defined-but-uncalled hook is dead code, not a missing fix.

3. **Read — a real but different defect.** `...ReadDetectsOutOfBandChange` mutates the stub's stored destination between refreshes, the way a console edit would. Terraform reports `no-op`: Read carries the prior state's `config` forward untouched, so remote config edits are invisible to `plan`. Wiring `refreshFlatConfigFromGetResponse` fixes exactly that test — and breaks three others, because refreshing the whole map drags in both API normalization and the SDK's synthesized defaults. The defect is real; #204's remedy trades drift-blindness for the perpetual diff that DEV-856 is about.

A correct fix for the Read gap must reconcile only the keys the configuration declares, leaving keys the practitioner never wrote out of state entirely. That is a different change from #204 and needs its own issue.

### Disposition

- #204's production changes are reverted on the test branch: `observabilitydestination_config_sync.go` removed, the Create call site removed. The production tree is byte-identical to the merge-base.
- The six stub tests are kept. `...S3ImmediatePlanAfterCreate` now guards the invariant that broke — state must hold exactly the four keys the configuration declares — so a future re-introduction of response-driven synchronization fails immediately.
- The branch's stub tests used `resource.Test`, which skips without `TF_ACC` despite the file's comment claiming otherwise, so they never executed in the unit lane. They now use `resource.UnitTest` and run without a management key, guarded by a Terraform-binary check.
- Two tests are skipped for tracked defects rather than deleted or masked, so both gaps stay visible in every run log. `ImportStateVerifyIgnore` is not used.

### Known gaps

- **DEV-872** — the generated Read mapper never assigns `r.Type`, so `ImportStateVerify` fails structurally on `type` for this resource regardless of anything #204 does. `...Import` is skipped with that citation.
- **Read-path config drift blindness** — needs its own Linear issue; the skip on `...ReadDetectsOutOfBandChange` currently cites DEV-856 as the parent investigation.
