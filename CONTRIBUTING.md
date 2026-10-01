# Contributing to This Repository

Thank you for your interest in contributing to this repository. Please note that this repository contains generated code. As such, we do not accept direct changes or pull requests. Instead, we encourage you to follow the guidelines below to report issues and suggest improvements.

## How to Report Issues

If you encounter any bugs or have suggestions for improvements, please open an issue on GitHub. When reporting an issue, please provide as much detail as possible to help us reproduce the problem. This includes:

- A clear and descriptive title
- Steps to reproduce the issue
- Expected and actual behavior
- Any relevant logs, screenshots, or error messages
- Information about your environment (e.g., operating system, software versions)
    - For example can be collected using the `npx envinfo` command from your terminal if you have Node.js installed

## Issue Triage and Upstream Fixes

We will review and triage issues as quickly as possible. Our goal is to address bugs and incorporate improvements in the upstream source code. Fixes will be included in the next generation of the generated code.

## Running the Acceptance Tests

The provider ships with a live acceptance suite in `internal/acceptance/` that exercises every resource and data source against the real OpenRouter Management API. Live tests are gated behind `TF_ACC=1`, so normal `go test ./...` runs (and the offline PR CI) skip them; the httptest-backed stub tests in the same package still run.

### Required environment variables

- `OPENROUTER_MANAGEMENT_KEY` (required) — a Management API key (`sk-or-mgmt-...`) from a **dedicated test organization with $0 credits**. Management keys cannot spend on inference, and a zero-credit org makes any inference key minted during the tests unusable. Never run the suite against a production org.
- `OPENROUTER_BASE_URL` (optional) — an explicit override to point the provider and the sweeper at a staging API base URL. When unset, the provider defaults to the production API (`https://openrouter.ai/api/v1`).
- `OPENROUTER_PRIVATE_ENDPOINT_*` (optional) — the live `openrouter_private_endpoint` tests need an org with the private endpoints entitlement and each skips unless its workspace is set: `OPENROUTER_PRIVATE_ENDPOINT_WORKSPACE_ID` (a workspace holding a BYOK key) for the lifecycle test, `OPENROUTER_PRIVATE_ENDPOINT_NO_BYOK_WORKSPACE_ID` (one without) for the failed-activation test. The full variable list is documented at the top of `internal/acceptance/private_endpoint_live_test.go`.

### Running locally

```sh
export TF_ACC=1
export OPENROUTER_MANAGEMENT_KEY=sk-or-mgmt-...
go test ./internal/acceptance/... -v -parallel 4 -timeout 30m
```

Every test skips cleanly when `TF_ACC` is unset; with `TF_ACC=1` and no key, the suite fails fast in `PreCheck`.

### Sweepers and fixture hygiene

All fixtures are named `tf-acc-<run-id>-<suffix>` and are destroyed by the testing framework at the end of each `TestCase`. A `TestMain` sweeper (`sweeper_test.go`) additionally lists the management collections before each run and deletes any leftover `tf-acc-*` resources from crashed or interrupted runs, so manual cleanup is almost never needed.

### Cost, rate limits, and quotas

- The suite creates and destroys real objects (workspaces, keys, guardrails, observability destinations). Management operations are free, but account caps apply — e.g. at most 5 observability destinations per type — which is why the suite runs with `-parallel 4`.
- Keep API rate limits in mind if you raise `-parallel`; the tests share one org.
- `openrouter_byok_key` is intentionally not covered: it requires a real third-party provider credential, which we do not store in CI.
- `data.openrouter_workspace_budgets` is intentionally not read: `GET /workspaces/{id}/budgets` returns 404 for a freshly created workspace with no budgets instead of the spec's 200-with-empty-list (Linear ENT-1742). Coverage will be restored once ENT-1742 is reproduced and resolved against the current deployed API.

### CI

`.github/workflows/ci.yaml` runs on every pull request targeting `main` and every push to `main`, without credentials: build, `go mod tidy -diff`, `gofmt` and `go vet` on everything except the generated SDK client (`internal/sdk`), `go test ./...` (unit and stub tests), `actionlint`, `terraform fmt` on `examples/`, and `goreleaser check`. Speakeasy regen PRs are opened with `GITHUB_TOKEN`, which does not trigger pull request workflows, so the push-to-`main` run validates them.

SDK updates merge without a human approval through the `openrouter-docs-sync` GitHub App, which must be a bypass actor on the `main` rulesets (repository and organization). The monorepo release flow opens a spec PR (`sdk-bot/openapi-update-*`, changing only `.speakeasy/in.openapi.yaml`). `.github/workflows/auto-merge-spec-update.yaml` merges the commit the app opened it with once `Build and test` and `Lint config` pass. That merge triggers `sdk_generation_for_spec_change.yaml`, whose regen PR `auto-merge-speakeasy-pr.yaml` merges directly. Both merges go through the REST merge endpoint because GitHub auto-merge never uses a ruleset bypass. Without the bypass, the regen PR is left with auto-merge queued and needs one approval, and the spec workflow fails with the rule violation.

`.github/workflows/acceptance.yaml` runs the live suite nightly (01:30 UTC), on `workflow_dispatch`, and on pull requests from branches in this repository that carry the `run-acceptance` label (applying the label starts a run; each push re-runs it while the label stays on). Fork PRs never receive the secret. The label is a convenience gate, not a security boundary: anyone who can push a branch here can already dispatch the workflow on it. It uses the `acceptance-testing` environment's `OPENROUTER_MANAGEMENT_KEY` secret. The `tf-acceptance` concurrency group (`cancel-in-progress: false`), shared with the release gate in `sdk_publish.yaml`, serializes every live run: the sweeper deletes all `tf-acc-*` resources, including a concurrent run's. The group sets `queue: max`, so runs wait in line rather than a newly queued PR run cancelling a pending release gate. The job requests only the `contents: read` permission. It validates that the management key secret is non-empty before running any tests, and never prints the key. The full test log is uploaded as a workflow artifact on every run (`if: always()`), and never contains Terraform state or the management key. Do not set `TF_LOG` or `TF_LOG_PROVIDER` in this workflow: the generated HTTP transport (`internal/provider/utils.go`) redacts only the `Authorization` header, not response bodies, so provider debug logging would leak full HTTP responses — including newly created secret material — into that artifact.

## Contact

If you have any questions or need further assistance, please feel free to reach out by opening an issue.

Thank you for your understanding and cooperation!

The Maintainers
