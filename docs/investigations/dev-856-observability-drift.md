# DEV-856: Observability destination drift — pre-fix evidence

This document records the exact evidence, gathered against a protected live acceptance environment on an isolated evidence branch, that the observability destination S3 config drift described in DEV-856 reproduces on provider `main` before any fix is applied. It exists to give the fix implementation a reproducible, credential-free baseline to work against and to revert to an empty-plan assertion once the fix lands.

## Branch provenance

The evidence branch `dev-856-evidence` was cut from provider `main` at commit `836c9c9a37b99dc6ca1c156385db6b6e4d7133d7` ("ci: regenerated with OpenAPI Doc , Speakeasy CLI 1.795.0 (#210)"), confirmed as `origin/main`'s tip and as the merge-base of `dev-856-evidence` with `origin/main` in the evidence worktree. Two commits were added on top: `ab02b1c` ("test: reproduce observability destination drift"), which added the acceptance test below, and `b452a8d` ("fix: use camelCase config keys for S3 destination in acceptance test"), which fixed a harness bug in that same test (see "Run 1" below). `b452a8d` is the branch's current HEAD.

## Version facts

- Provider HEAD at evidence collection time: `b452a8d91dc1e115aa009f27f92dac633a8e19a9`.
- Speakeasy generator version (from `.speakeasy/gen.lock` and `.speakeasy/workflow.lock`): `1.795.0`. `.speakeasy/workflow.yaml` pins the *tool* to `latest`, but the *generated output* records the resolved version, `1.795.0`, in `gen.lock` and `workflow.lock`.
- `github.com/hashicorp/terraform-plugin-framework`: `v1.19.0` (go.mod).
- `github.com/hashicorp/terraform-plugin-testing`: `v1.16.0` (go.mod).
- `internal/provider/observabilitydestination_resource.go` defines the S3 destination's `config.path_template` and `config.prefix` attributes as **Computed-only** (no `Optional: true`) with a schema-level static `Default`, and the same is true of every other destination type's URL/table/database-name-style fields in this file — this is a generated-schema pattern, not specific to S3:
  - `path_template` (line 1782-1786): `Computed: true`, `Default: stringdefault.StaticString("{prefix}/{date}")`.
  - `prefix` (line 1787-1791): `Computed: true`, `Default: stringdefault.StaticString("openrouter-traces")`.
  - The enclosing `s3.config` object itself (line 1763-1764) is also `Computed: true` with no `Optional`, i.e. the entire nested object is framework-computed and derived server-side from the flat, write-only `config` map the user actually sets (the `bucketName`/`accessKeyId`/... JSON-encoded strings), not from `s3.config.*` directly.

This matches DEV-856's stated root-cause hypothesis: computed-only generated defaults, not customer-supplied values, are what the provider schema treats as authoritative on each plan.

## Run 1 (harness bug, not the DEV-856 defect): commit `ab02b1c`

- Workflow run: [32417541489](https://github.com/OpenRouterTeam/terraform-provider-openrouter/actions/runs/32417541489), `workflow_dispatch`, head SHA `ab02b1c600c19389f2dc1cfa36afead93266068c`, conclusion `failure`.
- `TestAccObservabilityDestination_S3PathTemplateDrift` failed during `Config` apply (step 1 of 2) with an API `400`: `Error: unexpected response from API. Got an unexpected response code 400` on `POST /api/v1/observability/destinations`, body `{"error":{"message":"Invalid configuration: Invalid configuration: bucketName: Invalid input: expected string, received undefined; accessKeyId: Invalid input: expected string, received undefined; secretAccessKey: Invalid input: expected string, received undefined","code":400}}`.
- This is a harness defect, not the DEV-856 defect: `ab02b1c`'s test config used snake_case keys (`bucket_name`, `access_key_id`, `secret_access_key`, `path_template`) inside the destination's flat `config` map, but the Management API expects camelCase (`bucketName`, `accessKeyId`, `secretAccessKey`, `pathTemplate`) for that map's keys. The API rejected the create outright before any plan/diff logic could run, so this run produced no DEV-856 evidence either way.
- The diff between `ab02b1c` and `b452a8d` is confined to those six map key names in `internal/acceptance/acceptance_test.go` (snake_case → camelCase); no assertion, plan-check, or config-value logic changed.

## Run 2 (DEV-856 defect evidence): commit `b452a8d`

- Workflow run: [32417882435](https://github.com/OpenRouterTeam/terraform-provider-openrouter/actions/runs/32417882435), `workflow_dispatch`, head SHA `b452a8d91dc1e115aa009f27f92dac633a8e19a9`, conclusion `success`.
- `TestAccObservabilityDestination_S3PathTemplateDrift` passed. Because the test's own assertions require `ExpectNonEmptyPlan: true` on both steps and a custom plan check (`requireS3ConfigDrift`) that fails the test if a non-empty plan is caused by anything other than `s3.config.prefix` / `s3.config.path_template` diverging, a passing run is itself the positive evidence: the drift reproduced, on every plan, for exactly the two attributes DEV-856 names.

### Plan/state matrix (from run 32417882435's test log)

| Point in test | `s3.config.prefix` | `s3.config.path_template` |
| --- | --- | --- |
| Customer values sent in config | `nonprod-coreml` | `{prefix}/{apiKeyName}/{year}/{month}/{day}` |
| Post-apply Terraform state (remote round-trip) | `nonprod-coreml` | `{prefix}/{apiKeyName}/{year}/{month}/{day}` |
| First plan, before refresh (`Before` → `After`) | `nonprod-coreml` → `<unavailable>` | `{prefix}/{apiKeyName}/{year}/{month}/{day}` → `<unavailable>` |
| First plan, after refresh (`Before` → `After`) | `nonprod-coreml` → `<unavailable>` | `{prefix}/{apiKeyName}/{year}/{month}/{day}` → `<unavailable>` |
| Repeated plan (separate `PlanOnly` step), before refresh | `nonprod-coreml` → `<unavailable>` | `{prefix}/{apiKeyName}/{year}/{month}/{day}` → `<unavailable>` |
| Repeated plan, after refresh | `nonprod-coreml` → `<unavailable>` | `{prefix}/{apiKeyName}/{year}/{month}/{day}` → `<unavailable>` |

`<unavailable>` is the test's own placeholder (from `extractS3ConfigField` in `internal/acceptance/acceptance_test.go`) for a planned value that is not a plain JSON string at that path in the raw `terraform show -json` plan — i.e. the field comes back `unknown` rather than holding a concrete value, which is consistent with the provider's plan logic treating the whole `s3.config` object as freshly `Computed` (no `UseStateForUnknown`-style plan modifier) rather than preserving the value the API just round-tripped back. Every one of the four plan checks reported `actions=[update]` for `openrouter_observability_destination.s3_drift`, and the API-confirmed state in between (the "post-apply state" row) proves the API itself stored and returned the customer's exact `nonprod-coreml` / `{prefix}/{apiKeyName}/{year}/{month}/{day}` values correctly — the divergence is introduced by the provider's own plan/diff step, not by the API.

The repeated-plan step (a second, independent `PlanOnly` invocation of the same config against the already-applied state, with no config change in between) shows the identical `update` diff on the identical two attributes, confirming the drift is perpetual — it recurs on every plan rather than being a one-time artifact of the initial apply.

### Verbatim log lines (non-secret evidence only)

```text
acceptance_test.go:388: DEV-856 evidence [post-apply state]: id="3995a4ba-873c-4265-9062-e768ac6602db" type="s3" enabled="false" s3.config.prefix="nonprod-coreml" s3.config.path_template="{prefix}/{apiKeyName}/{year}/{month}/{day}"
acceptance_test.go:418: DEV-856 evidence [first plan (pre-refresh)]: actions=[update] s3.config.prefix "nonprod-coreml" -> "<unavailable>", s3.config.path_template "{prefix}/{apiKeyName}/{year}/{month}/{day}" -> "<unavailable>"
acceptance_test.go:418: DEV-856 evidence [first plan (refreshed)]: actions=[update] s3.config.prefix "nonprod-coreml" -> "<unavailable>", s3.config.path_template "{prefix}/{apiKeyName}/{year}/{month}/{day}" -> "<unavailable>"
acceptance_test.go:418: DEV-856 evidence [repeated plan (pre-refresh)]: actions=[update] s3.config.prefix "nonprod-coreml" -> "<unavailable>", s3.config.path_template "{prefix}/{apiKeyName}/{year}/{month}/{day}" -> "<unavailable>"
acceptance_test.go:418: DEV-856 evidence [repeated plan (refreshed)]: actions=[update] s3.config.prefix "nonprod-coreml" -> "<unavailable>", s3.config.path_template "{prefix}/{apiKeyName}/{year}/{month}/{day}" -> "<unavailable>"
--- PASS: TestAccObservabilityDestination_S3PathTemplateDrift (2.24s)
```

No AWS credentials, Management API keys, or authorization header values appear in either run's log; the request/response dump in run 1's failure shows `Authorization: (sensitive)`, and the workflow's own `OPENROUTER_MANAGEMENT_KEY: ***` line is GitHub's standard secret masking. The S3 credential values in the test config (`AKIADEV856EXAMPLEKEY`, `dev856-example-secret-access-key-not-real`, bucket `tf-acc-dev-856-bucket`) are synthetic placeholders defined in the test itself — the Management API does not validate them at creation time, and this test never performs a real S3 write.

## Conclusion

The DEV-856 defect reproduces exactly as described: on a live acceptance run against provider `main` (commit `836c9c9a37b99dc6ca1c156385db6b6e4d7133d7`) plus the evidence test, an S3 observability destination created with the customer's exact `prefix` (`nonprod-coreml`) and `path_template` (`{prefix}/{apiKeyName}/{year}/{month}/{day}`) values round-trips correctly through the Management API into Terraform state, but every subsequent plan — including a second, independent plan with no intervening config change — reports a perpetual `update` action driven solely by those two attributes reverting toward an unknown/computed value instead of staying pinned to the applied state. This is consistent with the `Computed`-only, `Default`-bearing schema for `s3.config.prefix` and `s3.config.path_template` in `internal/provider/observabilitydestination_resource.go`, which has no plan modifier to preserve prior known state. Run 1 (`ab02b1c`, run [32417541489](https://github.com/OpenRouterTeam/terraform-provider-openrouter/actions/runs/32417541489)) failed for an unrelated harness reason (snake_case config keys rejected by the Management API with a `400`) and produced no evidence either way; run 2 (`b452a8d`, run [32417882435](https://github.com/OpenRouterTeam/terraform-provider-openrouter/actions/runs/32417882435)) is the evidence run and passed, which — given the test's `ExpectNonEmptyPlan: true` and attribute-scoped plan check — is itself proof the defect reproduced. Per DEV-856 step 5, implementation should proceed on this basis; this test's `ExpectNonEmptyPlan: true` assertions should be inverted to expect an empty plan once the fix lands.
