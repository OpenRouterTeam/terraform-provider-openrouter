# Speakeasy agent skills

Vendored from [speakeasy-api/skills](https://github.com/speakeasy-api/skills) at commit `d2eab59` (Apache-2.0, see `LICENSE`), in the [Agent Skills](https://agentskills.io/specification) format that `npx skills add speakeasy-api/skills` installs. `.claude/skills` is a symlink to this directory so Claude Code picks them up too; other agents read `.agents/skills`.

Included, because they apply to how this repo is generated (Terraform target, overlays in `.speakeasy/`):

| Skill | Use when |
|---|---|
| `speakeasy-context` | Looking up Speakeasy CLI behavior (`speakeasy agent context`) instead of guessing |
| `generate-terraform-provider` | Changing entity annotations, CRUD mapping, or workflow config |
| `manage-openapi-overlays` | Editing the overlays in `.speakeasy/` |
| `diagnose-generation-failure` | `speakeasy run` fails |
| `improve-sdk-naming` | Renaming generated methods or resources |

The skills are generic Speakeasy guidance. Where they disagree with `CONTRIBUTING.md`, follow `CONTRIBUTING.md`: generation here goes through `.github/workflows/regenerate.yaml` and `sdk_generation.yaml`, and `.speakeasy/workflow.yaml` is the source of truth for the spec pipeline.

To refresh, re-copy these directories from a newer checkout of the upstream repo and update the commit above. Skills not listed (SDK hooks, MCP, multi-target, and so on) are deliberately left out.
