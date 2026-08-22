package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// TestAccApiKey_ForceNew verifies that changing a create-only attribute
// (expires_at exists in the create request but not the update request) plans
// a replacement rather than an in-place update.
func TestAccApiKey_ForceNew(t *testing.T) {
	name := testName("key-fn")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_api_key.test": {path: "/keys", idAttr: "hash"},
		}),
		Steps: []resource.TestStep{
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_api_key" "test" {
  name       = %q
  limit      = 0
  expires_at = "2030-01-01T00:00:00Z"
}
`, name),
				Check: resource.TestCheckResourceAttr("openrouter_api_key.test", "expires_at", "2030-01-01T00:00:00Z"),
			},
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_api_key" "test" {
  name       = %q
  limit      = 0
  expires_at = "2031-01-01T00:00:00Z"
}
`, name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_api_key.test", plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// TestAccGuardrail_ForceNew verifies workspace_id (create-only) forces
// replacement, using a workspace created in the same config.
func TestAccGuardrail_ForceNewWorkspace(t *testing.T) {
	name := testName("gr-fn")

	base := func(wsRef string) string {
		return providerConfig() + fmt.Sprintf(`
resource "openrouter_workspace" "a" {
  name = "%[1]s-a"
  slug = "%[1]s-a"
}

resource "openrouter_workspace" "b" {
  name = "%[1]s-b"
  slug = "%[1]s-b"
}

resource "openrouter_guardrail" "test" {
  name           = %[1]q
  limit_usd      = 1
  reset_interval = "monthly"
  workspace_id   = openrouter_workspace.%[2]s.id
}
`, name, wsRef)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_workspace.a":    {path: "/workspaces", idAttr: "id"},
			"openrouter_workspace.b":    {path: "/workspaces", idAttr: "id"},
			"openrouter_guardrail.test": {path: "/guardrails", idAttr: "id"},
		}),
		Steps: []resource.TestStep{
			{
				Config: base("a"),
				Check:  resource.TestCheckResourceAttrPair("openrouter_guardrail.test", "workspace_id", "openrouter_workspace.a", "id"),
			},
			{
				Config: base("b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("openrouter_guardrail.test", plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

// TestAccDataSources_Management reads every management data source against
// resources created in the same configuration, proving list population and
// singular lookups end to end.
func TestAccDataSources_Management(t *testing.T) {
	name := testName("ds")

	config := providerConfig() + fmt.Sprintf(`
resource "openrouter_workspace" "test" {
  name = %[1]q
  slug = %[1]q
}

resource "openrouter_api_key" "test" {
  name  = %[1]q
  limit = 0
}

resource "openrouter_guardrail" "test" {
  name           = %[1]q
  limit_usd      = 1
  reset_interval = "monthly"
}

data "openrouter_api_key" "by_hash" {
  hash = openrouter_api_key.test.hash
}

data "openrouter_api_keys" "all" {
  depends_on = [openrouter_api_key.test]
}

data "openrouter_guardrail" "by_id" {
  id = openrouter_guardrail.test.id
}

data "openrouter_guardrails" "all" {
  depends_on = [openrouter_guardrail.test]
}

data "openrouter_workspace" "by_id" {
  id = openrouter_workspace.test.id
}

data "openrouter_workspaces" "all" {
  depends_on = [openrouter_workspace.test]
}

data "openrouter_workspace_members" "members" {
  id = openrouter_workspace.test.id
}

# NOTE: data.openrouter_workspace_budgets is intentionally not read here.
# Live finding (Linear ENT-1742): GET /workspaces/{id}/budgets returns 404
# "Workspace not found" for a freshly created workspace with no budgets,
# contradicting the spec's 200-with-empty-list. Restore this read when
# ENT-1742 is fixed; provider-side tracking in DEV-486.

data "openrouter_guardrail_key_assignments" "key_assignments" {
  depends_on = [openrouter_guardrail.test]
}

data "openrouter_guardrail_member_assignments" "member_assignments" {
  depends_on = [openrouter_guardrail.test]
}

data "openrouter_byok_keys" "all" {}

data "openrouter_credits" "current" {}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_workspace.test": {path: "/workspaces", idAttr: "id"},
			"openrouter_api_key.test":   {path: "/keys", idAttr: "hash"},
			"openrouter_guardrail.test": {path: "/guardrails", idAttr: "id"},
		}),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Singular lookups resolve the created resources.
					resource.TestCheckResourceAttr("data.openrouter_api_key.by_hash", "name", name),
					resource.TestCheckResourceAttr("data.openrouter_guardrail.by_id", "name", name),
					resource.TestCheckResourceAttr("data.openrouter_workspace.by_id", "name", name),
					// List data sources return at least the created entries.
					resource.TestCheckResourceAttrSet("data.openrouter_api_keys.all", "data.#"),
					resource.TestCheckResourceAttrSet("data.openrouter_guardrails.all", "data.#"),
					resource.TestCheckResourceAttrSet("data.openrouter_workspaces.all", "data.#"),
					// Scoped lists respond (may legitimately be empty).
					resource.TestCheckResourceAttrSet("data.openrouter_workspace_members.members", "data.#"),
					resource.TestCheckResourceAttrSet("data.openrouter_guardrail_key_assignments.key_assignments", "data.#"),
					resource.TestCheckResourceAttrSet("data.openrouter_guardrail_member_assignments.member_assignments", "data.#"),
					resource.TestCheckResourceAttrSet("data.openrouter_byok_keys.all", "data.#"),
					// Credits endpoint returns the account totals object.
					resource.TestCheckResourceAttrSet("data.openrouter_credits.current", "data.total_credits"),
				),
			},
		},
	})
}

// TestAccDataSources_Catalog reads the public catalog data sources. These hit
// unauthenticated-style read paths but still exercise the generated
// pagination and envelope handling for large responses (models is the
// biggest payload in the provider).
func TestAccDataSources_Catalog(t *testing.T) {
	config := providerConfig() + `
data "openrouter_models" "all" {}

data "openrouter_providers" "all" {}

data "openrouter_presets" "all" {}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.openrouter_models.all", "data.#"),
					resource.TestCheckResourceAttrSet("data.openrouter_providers.all", "data.#"),
					resource.TestCheckResourceAttrSet("data.openrouter_presets.all", "data.#"),
				),
			},
		},
	})
}

// TestAccFullGraph_Integration provisions a realistic dependency graph in one
// apply: workspace -> scoped api_key + budget-capped guardrail + webhook
// destination, then destroys everything. This is the closest CI analogue to
// a real customer configuration (IaC onboarding scenario) and catches
// cross-resource ordering or reference regressions no single-resource test
// can.
func TestAccFullGraph_Integration(t *testing.T) {
	name := testName("graph")

	config := providerConfig() + fmt.Sprintf(`
resource "openrouter_workspace" "env" {
  name        = %[1]q
  slug        = %[1]q
  description = "acceptance full-graph workspace"
}

resource "openrouter_api_key" "svc" {
  name         = "%[1]s-svc"
  limit        = 0
  limit_reset  = "monthly"
  workspace_id = openrouter_workspace.env.id
}

resource "openrouter_guardrail" "cap" {
  name           = "%[1]s-cap"
  limit_usd      = 1
  reset_interval = "monthly"
  workspace_id   = openrouter_workspace.env.id
}

resource "openrouter_observability_destination" "hook" {
  name    = "%[1]s-hook"
  type    = "webhook"
  enabled = false
  config = {
    url    = jsonencode("https://example.com/tf-acc-graph")
    method = jsonencode("POST")
  }
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_workspace.env":                  {path: "/workspaces", idAttr: "id"},
			"openrouter_api_key.svc":                    {path: "/keys", idAttr: "hash"},
			"openrouter_guardrail.cap":                  {path: "/guardrails", idAttr: "id"},
			"openrouter_observability_destination.hook": {path: "/observability/destinations", idAttr: "id"},
		}),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("openrouter_api_key.svc", "workspace_id", "openrouter_workspace.env", "id"),
					resource.TestCheckResourceAttrPair("openrouter_guardrail.cap", "workspace_id", "openrouter_workspace.env", "id"),
					resource.TestCheckResourceAttrSet("openrouter_observability_destination.hook", "id"),
				),
			},
			// Re-plan of the identical config must be empty across the whole
			// graph: catches any resource whose refresh introduces drift.
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}
