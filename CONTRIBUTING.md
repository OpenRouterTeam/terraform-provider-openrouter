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

The provider ships with a live acceptance suite in `internal/acceptance/` that exercises every resource and data source against the real OpenRouter Management API. It is gated behind `TF_ACC=1`, so normal `go test ./...` runs (and PR CI) skip it entirely — PR build duration is unaffected.

### Required environment variables

- `OPENROUTER_MANAGEMENT_KEY` (required) — a Management API key (`sk-or-mgmt-...`) from a **dedicated test organization with $0 credits**. Management keys cannot spend on inference, and a zero-credit org makes any inference key minted during the tests unusable. Never run the suite against a production org.
- `OPENROUTER_BASE_URL` (optional) — an explicit override to point the provider and the sweeper at a staging API base URL. When unset, the provider defaults to the production API (`https://openrouter.ai/api/v1`).

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

`.github/workflows/acceptance.yaml` runs the suite nightly (01:30 UTC) and on `workflow_dispatch`, using the `acceptance-testing` environment's `OPENROUTER_MANAGEMENT_KEY` secret. It never runs on pull requests, and a concurrency group (`tf-acceptance`, `cancel-in-progress: false`) serializes runs to avoid shared-state flakes. The job requests only the `contents: read` permission. It validates that the management key secret is non-empty before running any tests, and never prints the key. The full test log is uploaded as a workflow artifact on every run (`if: always()`), and never contains Terraform state or the management key. Do not set `TF_LOG` or `TF_LOG_PROVIDER` in this workflow: the generated HTTP transport (`internal/provider/utils.go`) redacts only the `Authorization` header, not response bodies, so provider debug logging would leak full HTTP responses — including newly created secret material — into that artifact.

## Contact

If you have any questions or need further assistance, please feel free to reach out by opening an issue.

Thank you for your understanding and cooperation!

The Maintainers
