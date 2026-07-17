# Terraform Entity Annotation Plan — OpenRouter Provider

Plan for annotating the OpenRouter OpenAPI spec (`.speakeasy/out.openapi.yaml`, sourced
from the upstream TypeScript SDK repo) with `x-speakeasy-entity` /
`x-speakeasy-entity-operation` so `speakeasy run` produces real Terraform resources.

## Ground rules

- **All annotations go in an overlay** — the spec is fetched remotely
  (`workflow.yaml` → OpenRouterTeam/typescript-sdk), so we cannot edit it directly.
- **New dedicated overlay file**: `.speakeasy/terraform-entities-overlay.yaml`,
  appended to `sources."OpenRouter API".overlays` in `workflow.yaml` *after* the
  existing `speakeasy-modifications-overlay.yaml`. Keeping it separate means the
  studio-managed modifications overlay can regenerate without clobbering TF annotations.
- **Entity names in PascalCase** (`ApiKey` → `openrouter_api_key`); plural PascalCase for
  list data sources (`ApiKeys`). Never `#list` — list endpoints get a plural entity with `#read`.
- **Entity placement**: this API wraps every CRUD payload in a `{ data: ... }` envelope.
  Annotation goes on the *inner* object (component schema or `properties.data`), which is
  the documented "lower level" placement; sibling envelope properties (e.g. the created
  API-key secret) flatten into the resource.

## Resource inventory

### Phase 1 — clean CRUD, annotate first

| Entity | Endpoints | Notes |
|---|---|---|
| `ApiKey` | `POST /keys` (create), `GET /keys/{hash}` (read), `PATCH /keys/{hash}` (update), `DELETE /keys/{hash}` (delete) | Schemas are **inline** (no shared component) — must annotate request body schema *and* each response's `properties.data` with the same entity name (agent-lessons Mistake 2). Path param `hash` auto-correlates with `data.hash`. Create response's sibling `key` (the secret, returned only once) flattens into the resource → mark `x-speakeasy-param-sensitive: true`. `expires_at`/`creator_user_id`/`workspace_id` in create but not update → ForceNew (correct). `disabled` update-only → settable post-create only (API limitation, acceptable). |
| `Guardrail` | `POST /guardrails`, `GET/PATCH/DELETE /guardrails/{id}` | Responses `$ref` `Create/Get/Update GuardrailResponse` whose `data` is `allOf [Guardrail]`. Annotate the **`Guardrail` component** once + the request components (`CreateGuardrailRequest`, `UpdateGuardrailRequest`) with the same name. `workspace_id` create-only → ForceNew. |
| `Workspace` | `POST /workspaces`, `GET/PATCH/DELETE /workspaces/{id}` | Same envelope pattern via `Workspace` component. Clean create/update symmetry. |
| `ByokKey` | `POST /byok`, `GET/PATCH/DELETE /byok/{id}` | Annotate `BYOKKey` component + `CreateBYOKKeyRequest`/`UpdateBYOKKeyRequest` as `ByokKey` (PascalCase override — raw `BYOKKey` would produce a bad underscore name). The credential `key` is write-only: present in create/update requests, **never returned** → mark `x-speakeasy-param-sensitive: true` (and consider `x-speakeasy-terraform-write-only` if we require TF ≥ 1.11). Drift on the key value is undetectable — expected. `workspace_id` create-only → ForceNew. |

Overlay sketch (ApiKey, the inline case):

```yaml
actions:
  - target: $["paths"]["/keys"]["post"]
    update:
      x-speakeasy-entity-operation: ApiKey#create
  - target: $["paths"]["/keys"]["post"]["requestBody"]["content"]["application/json"]["schema"]
    update:
      x-speakeasy-entity: ApiKey
  - target: $["paths"]["/keys"]["post"]["responses"]["201"]["content"]["application/json"]["schema"]["properties"]["data"]
    update:
      x-speakeasy-entity: ApiKey
  - target: $["paths"]["/keys"]["post"]["responses"]["201"]["content"]["application/json"]["schema"]["properties"]["key"]
    update:
      x-speakeasy-param-sensitive: true
  - target: $["paths"]["/keys/{hash}"]["get"]
    update:
      x-speakeasy-entity-operation: ApiKey#read
  - target: $["paths"]["/keys/{hash}"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["properties"]["data"]
    update:
      x-speakeasy-entity: ApiKey
  # ... PATCH → ApiKey#update (+ request schema & data), DELETE → ApiKey#delete
```

Component-based case (Guardrail):

```yaml
  - target: $["components"]["schemas"]["Guardrail"]
    update:
      x-speakeasy-entity: Guardrail
  - target: $["components"]["schemas"]["CreateGuardrailRequest"]
    update:
      x-speakeasy-entity: Guardrail
  - target: $["components"]["schemas"]["UpdateGuardrailRequest"]
    update:
      x-speakeasy-entity: Guardrail
  - target: $["paths"]["/guardrails"]["post"]
    update:
      x-speakeasy-entity-operation: Guardrail#create
  # ... get/patch/delete on /guardrails/{id}
```

### Phase 2 — harder shapes, decide individually

| Entity | Endpoints | Problem & approach |
|---|---|---|
| `ObservabilityDestination` | `POST /observability/destinations`, `GET/PATCH/DELETE /observability/destinations/{id}` | Response `data` is a **discriminated `oneOf` of 17 variants** (`type` discriminator: datadog, langfuse, s3, ...); create request `config` is an open object (`additionalProperties`) validated server-side. Per agent-lessons Mistake 1, do **not** overlay the open `config` into typed shapes. Attempt annotation as-is first (gen.yaml already has `inferUnionDiscriminators` + `preApplyUnionDiscriminators`); if union reconciliation fails, fall back to annotating variants individually or defer. Secrets inside variant configs (`secretKey`, api keys) need `x-speakeasy-param-sensitive`. |
| `WorkspaceBudget` | `PUT /workspaces/{id}/budgets/{interval}` (upsert), `DELETE` same path, `GET /workspaces/{id}/budgets` (list only) | Upsert maps to `WorkspaceBudget#create,update` on the PUT. **No singular read endpoint** — refresh/import can't work without one. Options: (a) ship create/update/delete without read (no drift detection), (b) request a `GET /workspaces/{id}/budgets/{interval}` upstream, (c) defer. Recommend (b) + defer until then. |
| `GuardrailKeyAssignment` / `GuardrailMemberAssignment` | `POST /guardrails/{id}/assignments/keys` (+`/remove`), same for members | Bulk association endpoints (request: `key_hashes[]`; response: assignment rows). Not clean CRUD; modeling one-assignment-per-resource over a bulk API needs careful correlation. Defer to phase 2; list endpoints can still be data sources (`GuardrailKeyAssignments#read`). |
| Workspace membership | `POST /workspaces/{id}/members/add` / `/remove`, `GET /workspaces/{id}/members` | Same bulk-association pattern; defer. |

### Data sources (read-only, plural entities)

Low-risk, add after Phase 1 resources build:

- `ApiKeys#read` → `GET /keys`; `Guardrails#read` → `GET /guardrails`;
  `Workspaces#read` → `GET /workspaces`; `ByokKeys#read` → `GET /byok`
- `Models#read` → `GET /models`; `Model#read` → `GET /model/{author}/{slug}`;
  `Providers#read` → `GET /providers`
- `Presets#read` → `GET /presets`; `Preset#read` → `GET /presets/{slug}` (no write ops — data source only)
- `Credits#read` → `GET /credits`
- Optional: `ZdrEndpoints`, `EmbeddingsModels`, `ImagesModels`, `VideosModels` — likely skip; inference-catalog noise for a TF provider.

Inference/completion endpoints (`/chat/completions`, `/messages`, `/responses`, `/images`,
`/embeddings`, `/audio/*`, `/videos`, `/rerank`, `/analytics/*`, `/activity`, ...) get **no
annotations** — they are not infrastructure.

### Cleanup

- Upstream spec carries a stray `x-speakeasy-entity: ChatStreamChunk` on the
  `ChatStreamChunk` component with no entity operations. Remove it via overlay so it
  can't materialize as a phantom entity.

## Execution order

1. ~~**Wire overlay**~~ ✅ `.speakeasy/terraform-entities-overlay.yaml` created, wired into `workflow.yaml`.
2. ~~**Phase 1 entities**~~ ✅ ApiKey, Guardrail, Workspace, ByokKey (+ ChatStreamChunk removal).
   `PATCH /keys/{hash}` doubles as `ApiKey#create#2` so `disabled` applies at creation.
3. ~~**Generate & verify**~~ ✅ 5 resources, 20 data sources; `go build ./...` clean;
   ForceNew/Sensitive inference verified; binary serves plugin protocol.
4. ~~**Data sources**~~ ✅ All plural `#read` annotations landed, including
   `GuardrailKeyAssignments`, `GuardrailMemberAssignments`, `WorkspaceMembers`,
   `WorkspaceBudgets` interim data sources.
5. ~~**ObservabilityDestination**~~ ✅ Shipped as `openrouter_observability_destination`.
   Union generated as: required `type` (OneOf validator, ForceNew) + required open
   `config` JSON map (input, never overwritten by refresh) + 17 Computed-only typed
   variant attributes populated from the discriminator response. All 19 secret config
   fields Sensitive. Adversarially reviewed: no perpetual-diff or leak paths found.
   Note for docs: `config` keys are the API's camelCase names (`publicKey`, not
   `public_key`).
   Gotcha fixed along the way: `provider` is a reserved TF root attribute — BYOK's
   `provider` field and the `/byok` list query param are renamed `provider_slug`
   via `x-speakeasy-name-override` (wire format unchanged).
6. ~~**Smoke test**~~ ✅ Exceeded: CI acceptance suite (`internal/acceptance/`,
   `.github/workflows/acceptance.yaml`) runs create → import → update for
   api_key, guardrail, workspace, and observability_destination against the
   live API nightly — green (Linear DEV-488). Live findings: api_key imports
   by `hash`; `POST /guardrails` requires `reset_interval` with `limit_usd`
   (spec gap, upstream candidate).
7. **Housekeeping**: README resource list regenerated automatically ✅;
   publishing checklist tracked in Linear DEV-489.

## Deferred (blocked or needs design)

- **WorkspaceBudget resource** — blocked on upstream `GET /workspaces/{id}/budgets/{interval}` (DEV-486). List data source shipped.
- **Assignment/membership resources** — bulk-association modeling decision pending (DEV-487). List data sources shipped.

## Known risks

- **Envelope flattening**: `data`-level entity placement flattens sibling props (docs warn
  about conflicts). The only meaningful sibling is `key` on ApiKey create — desired.
- **`respectRequiredFields: false`** in gen.yaml — required-ness inference for
  Required/Optional attributes should still come from create-request `required` lists;
  verify after first generation that `name` etc. come out `Required`.
- **allOf-wrapped data** (`data: allOf [Guardrail, {description}]`) — gen.yaml uses
  `allOfMergeStrategy: shallowMerge`; entity annotation on the `Guardrail` component
  should survive the merge. Verify in generated output; fall back to annotating the
  `data` property directly if not.
- Upstream spec churn: overlay JSONPaths target paths/components by name; a rename
  upstream silently drops the action. `speakeasy overlay validate` + generation diff
  on each `speakeasy run` catches this.
