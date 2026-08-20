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
  - The enclosing `s3.config` object itself (line 1763-1764) is also `Computed: true` with no `Optional`, i.e. the entire nested object is framework-computed and derived server-side from the flat `config` map the user actually sets (the `bucketName`/`accessKeyId`/... JSON-encoded strings), not from `s3.config.*` directly. That flat `config` map is `schema.MapAttribute{Required: true, Sensitive: true, ...}` (line 503-511) — sensitive (not echoed back in CLI output), not a distinct "write-only" attribute kind; the framework has no `WriteOnly` field on this schema.

This matches DEV-856's stated root-cause hypothesis: computed-only generated defaults, not customer-supplied values, are what the provider schema treats as authoritative on each plan.

## Run 1 (harness bug, not the DEV-856 defect): commit `ab02b1c`

- Workflow run: [32417541489](https://github.com/OpenRouterTeam/terraform-provider-openrouter/actions/runs/32417541489), `workflow_dispatch`, head SHA `ab02b1c600c19389f2dc1cfa36afead93266068c`, conclusion `failure`.
- `TestAccObservabilityDestination_S3PathTemplateDrift` failed during `Config` apply (step 1 of 2) with an API `400`: `Error: unexpected response from API. Got an unexpected response code 400` on `POST /api/v1/observability/destinations`, body `{"error":{"message":"Invalid configuration: Invalid configuration: bucketName: Invalid input: expected string, received undefined; accessKeyId: Invalid input: expected string, received undefined; secretAccessKey: Invalid input: expected string, received undefined","code":400}}`.
- This is a harness defect, not the DEV-856 defect: `ab02b1c`'s test config used snake_case keys (`bucket_name`, `access_key_id`, `secret_access_key`, `path_template`) inside the destination's flat `config` map, but the Management API expects camelCase (`bucketName`, `accessKeyId`, `secretAccessKey`, `pathTemplate`) for that map's keys. The API rejected the create outright before any plan/diff logic could run, so this run produced no DEV-856 evidence either way.
- The diff between `ab02b1c` and `b452a8d` touches six lines of the same six-key map in `internal/acceptance/acceptance_test.go`, but only four keys actually change name (`bucket_name`→`bucketName`, `access_key_id`→`accessKeyId`, `secret_access_key`→`secretAccessKey`, `path_template`→`pathTemplate`); the other two (`region`, `prefix`) are unchanged key names, re-touched only for gofmt-style value-column realignment after the renames widened the key column. No assertion, plan-check, or config-value logic changed.

## Run 2 (DEV-856 defect evidence): commit `b452a8d`

- Workflow run: [32417882435](https://github.com/OpenRouterTeam/terraform-provider-openrouter/actions/runs/32417882435), `workflow_dispatch`, head SHA `b452a8d91dc1e115aa009f27f92dac633a8e19a9`, conclusion `success`.
- `TestAccObservabilityDestination_S3PathTemplateDrift` passed. Because the test's own assertions require `ExpectNonEmptyPlan: true` on both steps and a custom plan check (`requireS3ConfigDrift`) that fails the test if a non-empty plan is caused by anything other than `s3.config.prefix` / `s3.config.path_template` diverging, a passing run is itself the positive evidence: the drift reproduced, on every plan, for at least the two attributes DEV-856 names. See "Exclusivity of the drifting attributes" below for what this run does and does not establish about the sibling `s3.config` fields.

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

### Exclusivity of the drifting attributes (not established by this evidence)

This evidence proves `s3.config.prefix` and `s3.config.path_template` are not both stable across every plan; it does not establish that they are the *only* `s3.config` fields that drift. `requireS3ConfigDrift` (`internal/acceptance/acceptance_test.go`) only calls `extractS3ConfigField` for `prefix` and `path_template` — it never reads or logs the sibling fields defined in the same `s3.config` object (`internal/provider/observabilitydestination_resource.go` line 1763-1803: `access_key_id`, `bucket_name`, `endpoint`, `headers`, `region`, `secret_access_key`, `session_token`), so the test log contains no information about whether those siblings held steady or also drifted on the same plans. No out-of-band source answers this either: `.github/workflows/acceptance.yaml` has no `actions/upload-artifact` step (confirmed by `grep -n "upload-artifact" .github/workflows/acceptance.yaml` returning nothing), so `gh run download 32417882435` correctly reports "no valid artifacts found to download" — there is no `acceptance.log` artifact or raw `terraform show -json` plan retained anywhere for this run beyond the `go test -v` stdout already quoted above, and that stdout only ever contains the `prefix`/`path_template` values the test chose to log. Re-inspecting the existing run cannot settle exclusivity either way.

What would settle it, without dispatching a new protected run now: extend `requireS3ConfigDrift`/`extractS3ConfigField` to also log the sibling `s3.config.*` fields (`bucket_name`, `endpoint`, `headers`, `region` are non-sensitive in the schema; `access_key_id`, `secret_access_key`, `session_token` are `Sensitive: true` and must stay excluded per the existing sensitive-field guard), then read that broadened evidence off the *next* protected run that was going to happen anyway (nightly schedule or a dispatch already planned for the fix itself), rather than dispatching one solely for this question. Until then, treat the fix scope as "at least the `Default`-bearing computed fields `prefix` and `path_template` drift" rather than "only `Default`-bearing fields drift" or "every computed-only nested field drifts" — this evidence does not distinguish between those broader and narrower root-cause scopes.

### Verbatim log lines (non-secret evidence only)

```text
acceptance_test.go:388: DEV-856 evidence [post-apply state]: id="3995a4ba-873c-4265-9062-e768ac6602db" type="s3" enabled="false" s3.config.prefix="nonprod-coreml" s3.config.path_template="{prefix}/{apiKeyName}/{year}/{month}/{day}"
acceptance_test.go:418: DEV-856 evidence [first plan (pre-refresh)]: actions=[update] s3.config.prefix "nonprod-coreml" -> "<unavailable>", s3.config.path_template "{prefix}/{apiKeyName}/{year}/{month}/{day}" -> "<unavailable>"
acceptance_test.go:418: DEV-856 evidence [first plan (refreshed)]: actions=[update] s3.config.prefix "nonprod-coreml" -> "<unavailable>", s3.config.path_template "{prefix}/{apiKeyName}/{year}/{month}/{day}" -> "<unavailable>"
acceptance_test.go:418: DEV-856 evidence [repeated plan (pre-refresh)]: actions=[update] s3.config.prefix "nonprod-coreml" -> "<unavailable>", s3.config.path_template "{prefix}/{apiKeyName}/{year}/{month}/{day}" -> "<unavailable>"
acceptance_test.go:418: DEV-856 evidence [repeated plan (refreshed)]: actions=[update] s3.config.prefix "nonprod-coreml" -> "<unavailable>", s3.config.path_template "{prefix}/{apiKeyName}/{year}/{month}/{day}" -> "<unavailable>"
--- PASS: TestAccObservabilityDestination_S3PathTemplateDrift (2.24s)
```

No AWS credentials, Management API keys, or authorization header values appear in either run's log; the request/response dump in run 1's failure shows `Authorization: (sensitive)`, and the workflow's own `OPENROUTER_MANAGEMENT_KEY: ***` line is GitHub's standard secret masking. The S3 credential values in the test config (`AKIAIOSFODNN7EXAMPLE`, `dev856-example-secret-access-key-not-real`, bucket `tf-acc-dev-856-bucket`) are synthetic placeholders defined in the test itself — the Management API does not validate them at creation time, and this test never performs a real S3 write. `AKIAIOSFODNN7EXAMPLE` is AWS's own reserved documentation placeholder; the literal actually committed in `ab02b1c`/`b452a8d` and executed by both runs is a different but equally invented string in the same shape — this document uses the AWS-blessed placeholder instead of quoting that literal a second time outside the test file. The committed test itself is left unchanged (it must keep matching what run 32417882435 actually executed) — only this document's prose was reworded.

## Conclusion

The DEV-856 defect reproduces exactly as described: on a live acceptance run against provider `main` (commit `836c9c9a37b99dc6ca1c156385db6b6e4d7133d7`) plus the evidence test, an S3 observability destination created with the customer's exact `prefix` (`nonprod-coreml`) and `path_template` (`{prefix}/{apiKeyName}/{year}/{month}/{day}`) values round-trips correctly through the Management API into Terraform state, but every subsequent plan — including a second, independent plan with no intervening config change — reports a perpetual `update` action driven at least by those two attributes reverting toward an unknown/computed value instead of staying pinned to the applied state; whether other `s3.config` fields also drift on the same plans is not established by this evidence (see "Exclusivity of the drifting attributes" above). This is consistent with the `Computed`-only, `Default`-bearing schema for `s3.config.prefix` and `s3.config.path_template` in `internal/provider/observabilitydestination_resource.go`, which has no plan modifier to preserve prior known state. Run 1 (`ab02b1c`, run [32417541489](https://github.com/OpenRouterTeam/terraform-provider-openrouter/actions/runs/32417541489)) failed for an unrelated harness reason (snake_case config keys rejected by the Management API with a `400`) and produced no evidence either way; run 2 (`b452a8d`, run [32417882435](https://github.com/OpenRouterTeam/terraform-provider-openrouter/actions/runs/32417882435)) is the evidence run and passed, which — given the test's `ExpectNonEmptyPlan: true` and attribute-scoped plan check — is itself proof the defect reproduced. Per DEV-856 step 5, implementation should proceed on this basis; this test's `ExpectNonEmptyPlan: true` assertions should be inverted to expect an empty plan once the fix lands.

## Post-fix inventory (Task 2)

This section is added on top of the verbatim evidence-branch document above. It records the exact before/after count of `Default:` schema declarations in `internal/provider/observabilitydestination_resource.go`, gathered while implementing the fix on branch `dev-856-observability-destination-defaults`, and classifies every declaration per the rule: a surviving `Default:` is writable-and-valid only if its attribute is `Optional` (user-settable); a `Default:` on a `Computed`-only attribute is defective.

### Provenance

- Branch: `dev-856-observability-destination-defaults`, refreshed from `origin/main` via merge commit `070db8b0dab52f9d19e33d2884165836138a8ada` ("Merge remote-tracking branch 'origin/main' into dev-856-observability-destination-defaults", 2026-08-20 16:46:47 -0500).
- Regeneration command: `speakeasy run --target open-router -y -o console`, pinned CLI binary `1.795.0` (matches `.speakeasy/gen.lock` / `.speakeasy/workflow.lock` post-merge; no discrepancy found between the two lock files).
- Overlay: `.speakeasy/terraform-entities-overlay.yaml`, DEV-856 section, 22 `remove: true` actions against `.default` sub-paths — inherited unchanged from the existing PR #192 diff, not modified by this task.
- `internal/provider/observabilitydestination_data_source.go` and `observabilitydestinations_data_source.go` had 0 `Default:` declarations both before and after regeneration (unaffected either way; not discussed further below).

### Before regeneration: 55 total `Default:` declarations

Counted against the merge-commit baseline (`070db8b`) of `internal/provider/observabilitydestination_resource.go`, before running `speakeasy run`.

**19 per-destination unique config fields — all `Computed: true` only, no `Optional` (defective):**

| Destination | Attribute | Type | Overlay removal target |
|---|---|---|---|
| arize | base_url | String | `ObservabilityArizeDestination.config.baseUrl.default` |
| braintrust | base_url | String | `ObservabilityBraintrustDestination.config.baseUrl.default` |
| clickhouse | table | String | `ObservabilityClickhouseDestination.config.table.default` |
| datadog | url | String | `ObservabilityDatadogDestination.config.url.default` |
| grafana | base_url | String | `ObservabilityGrafanaDestination.config.baseUrl.default` |
| langfuse | base_url | String | `ObservabilityLangfuseDestination.config.baseUrl.default` |
| langsmith | endpoint | String | `ObservabilityLangsmithDestination.config.endpoint.default` |
| langsmith | project | String | `ObservabilityLangsmithDestination.config.project.default` |
| newrelic | region | String | `ObservabilityNewrelicDestination.config.region.default` |
| posthog | endpoint | String | `ObservabilityPosthogDestination.config.endpoint.default` |
| ramp | base_url | String | `ObservabilityRampDestination.config.baseUrl.default` |
| s3 | path_template | String | `ObservabilityS3Destination.config.pathTemplate.default` |
| s3 | prefix | String | `ObservabilityS3Destination.config.prefix.default` |
| snowflake | database | String | `ObservabilitySnowflakeDestination.config.database.default` |
| snowflake | schema | String | `ObservabilitySnowflakeDestination.config.schema.default` |
| snowflake | table | String | `ObservabilitySnowflakeDestination.config.table.default` |
| snowflake | warehouse | String | `ObservabilitySnowflakeDestination.config.warehouse.default` |
| weave | base_url | String | `ObservabilityWeaveDestination.config.baseUrl.default` |
| webhook | method | String | `ObservabilityWebhookDestination.config.method.default` |

`s3.config.path_template` / `s3.config.prefix` are the two attributes the evidence branch proved drift on directly via a live run. The other 17 rows carry the identical `Computed`-only-plus-`Default` pattern on sibling destination types; this inventory establishes them for the first time by full-file inspection, not a live acceptance run — the evidence document's "Exclusivity of the drifting attributes" hedge (does the defect extend beyond `prefix`/`path_template`?) is answered here for the `Default`-bearing mechanism specifically: yes, all 17 sibling unique-config-field attributes share the exact defective schema shape. Whether other, non-`Default`-bearing `s3.config` fields (`bucket_name`, `endpoint`, `headers`, `region`) also drift by some other mechanism remains outside this inventory's scope, per the original hedge.

**34 shared `filter_rules` fields — one `enabled` (Bool) + one `logic` (String) per destination-type block, repeated identically across all 17 destination types — all `Computed: true` only, no `Optional` (defective):**

arize, braintrust, clickhouse, datadog, grafana, langfuse, langsmith, newrelic, opik, otel_collector, posthog, ramp, s3, sentry, snowflake, weave, webhook

| Attribute | Type | Overlay removal target |
|---|---|---|
| enabled | Bool | `ObservabilityFilterRulesConfig.enabled.default` |
| logic | String | `ObservabilityFilterRuleGroup.logic.default` |

**2 top-level `filter_rules` fields — `Optional: true` (user-settable), writable-and-valid pre-regeneration:**

| Attribute | Type | Overlay removal target |
|---|---|---|
| filter_rules.enabled | Bool | `ObservabilityFilterRulesConfigNullable.enabled.default` |
| filter_rules.groups[].logic | String | `ObservabilityFilterRuleGroup.logic.default` (same shared schema as the 17 defective `logic` occurrences above — see regression below) |

19 + 34 + 2 = 55 total: 53 `Computed`-only (defective, in DEV-856's fix scope) + 2 `Optional` (writable-and-valid pre-regen).

### After regeneration: 0 total `Default:` declarations

`grep -c 'Default:' internal/provider/observabilitydestination_resource.go` → `0`, cross-checked with an independent script re-scan of the full file. All 55 pre-regen `Default:` declarations are gone: the 53 defective ones correctly, and the 2 legitimate `Optional` ones incorrectly (see regression below). Per the classification rule, every surviving default would need per-attribute classification — trivially satisfied here since none survive — but zero survivors is not by itself proof the fix is correct, because the 2 legitimate defaults were also deleted rather than preserved.

### Regression: the two legitimate defaults were not preserved

`filter_rules.enabled` and `filter_rules.groups[].logic` (the top-level, writable `filter_rules` attribute — not any per-destination `filter_rules` block) lost not only `Default:` but also `Computed: true`, confirmed in the regenerated file (`internal/provider/observabilitydestination_resource.go`, lines 630-646):

```go
// Before (merge-commit baseline, lines 658-673)
"enabled": schema.BoolAttribute{
    Computed:    true,
    Optional:    true,
    Default:     booldefault.StaticBool(true),
    Description: `Default: true`,
},
...
"logic": schema.StringAttribute{
    Computed:    true,
    Optional:    true,
    Default:     stringdefault.StaticString("and"),
    Validators:  []validator.String{stringvalidator.OneOf("and", "or")},
    Description: `Default: "and"; must be one of ["and", "or"]`,
},

// After (regenerated, lines 630-646)
"enabled": schema.BoolAttribute{
    Optional: true,
},
...
"logic": schema.StringAttribute{
    Optional:    true,
    Validators:  []validator.String{stringvalidator.OneOf("and", "or")},
    Description: `must be one of ["and", "or"]`,
},
```

Root cause: the overlay had already split `enabled` into two OpenAPI schemas — `ObservabilityFilterRulesConfig` (consumed only by the 17 read-only per-destination-type response paths) and `ObservabilityFilterRulesConfigNullable` (consumed only by the one writable top-level `filter_rules` attribute) — specifically so a `default`-removal action could target the 17 defective occurrences without touching the 1 legitimate one. That split correctly isolates *which* attribute the removal affects, but Speakeasy still derives `Computed: true` on an `Optional` attribute *from the presence of an OpenAPI `default`*: removing the `default` also removes `Computed`, regardless of which schema it was removed from. So the `ObservabilityFilterRulesConfigNullable.enabled.default` removal strips `Computed` from the one place it needed to stay, even though the schema split correctly kept it from touching the 17 defective occurrences.

`logic` has no equivalent split: `ObservabilityFilterRuleGroup.logic` is the single shared schema for all 18 `logic` occurrences (17 defective + 1 legitimate), so its `default` removal cannot be scoped at all without either leaving 17 sites unfixed or first splitting `ObservabilityFilterRuleGroup` the same way `ObservabilityFilterRulesConfig`/`ObservabilityFilterRulesConfigNullable` were already split.

Net effect: a user who omits `filter_rules.enabled` or a `filter_rules.groups[].logic` entry from their Terraform config previously got the server's default (`true` / `"and"`) computed into state on apply, with no diff on later plans. Post-fix, both attributes are `Optional`-only, not `Computed` — Terraform treats an omitted value as `null` rather than server-computed, which risks a "Provider produced inconsistent result after apply" error if the API still returns a non-null value for that field, or a silently dropped server-computed value if it does not. **This is a regression introduced by this fix, not present before it.** Recommended follow-up before treating the fix as complete: split `ObservabilityFilterRuleGroup` the way `ObservabilityFilterRulesConfig`/`ObservabilityFilterRulesConfigNullable` were already split, and restore `Computed: true` (alongside `Optional: true` and `Default:`) on `ObservabilityFilterRulesConfigNullable.enabled` and the nullable variant of `ObservabilityFilterRuleGroup.logic`.

### S3 fix confirmed correct

`s3.config.path_template` and `s3.config.prefix` — the two attributes the evidence branch proved drift on directly — are now:

```go
"path_template": schema.StringAttribute{
    Computed:    true,
    Description: `Template for S3 object path. The filename ({traceId}-{timestamp}.json) is automatically appended. Available variables: {prefix}, {date}, {year}, {month}, {day}, {apiKeyName}`,
},
"prefix": schema.StringAttribute{
    Computed: true,
},
```

`Computed: true`, no `Optional`, no `Default:` — exactly the intended fix, and the same pattern the other 17 defective per-destination unique fields (`base_url`, `url`, `table`, `endpoint`, `project`, `region`, `database`, `schema`, `warehouse`, `method`) now also follow.

### Regression test

`TestAccObservabilityDestination_S3NonDefaultConfig` (`internal/acceptance/acceptance_test.go`) replaces the class-equivalent placeholder values the existing PR #192 test used with the exact customer values from this document (`prefix = "nonprod-coreml"`, `pathTemplate = "{prefix}/{apiKeyName}/{year}/{month}/{day}"`), and requires an empty plan rather than the evidence branch's drift-requiring assertions: `ExpectNonEmptyPlan` is gone, and `plancheck.ExpectEmptyPlan()` is asserted on `PostApplyPreRefresh` and `PostApplyPostRefresh` for both the initial apply step and a second, independent `PlanOnly` step. Verified to compile and skip cleanly without `TF_ACC` set (`go test ./internal/acceptance/... -v -timeout 5m`, all 10 tests in the package skip with `"Acceptance tests skipped unless env 'TF_ACC' set"`); not dispatched live in this task — no protected acceptance run was authorized or run.
