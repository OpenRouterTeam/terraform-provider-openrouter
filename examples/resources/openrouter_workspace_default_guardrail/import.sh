# Import by workspace id (UUID or slug). Works before the default guardrail
# has ever been configured; the resource imports as present and unconfigured.
terraform import openrouter_workspace_default_guardrail.production "11111111-1111-1111-1111-111111111111"
