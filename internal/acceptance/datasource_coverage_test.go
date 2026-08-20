package acceptance

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccDataSources_Singular reads every singular lookup data source against
// a real resource created in the same configuration. These prove the
// required-filter attribute (hash / id / slug) round-trips into a single
// result with computed attributes populated.
func TestAccDataSources_Singular(t *testing.T) {
	name := testName("ds-single")

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

resource "openrouter_observability_destination" "test" {
  name    = %[1]q
  type    = "webhook"
  enabled = false
  config = {
    url    = jsonencode("https://example.com/tf-acc-ds")
    method = jsonencode("POST")
  }
}

data "openrouter_api_key" "by_hash" {
  hash = openrouter_api_key.test.hash
}

data "openrouter_guardrail" "by_id" {
  id = openrouter_guardrail.test.id
}

data "openrouter_workspace" "by_id" {
  id = openrouter_workspace.test.id
}

data "openrouter_observability_destination" "by_id" {
  id = openrouter_observability_destination.test.id
}

data "openrouter_model" "by_slug" {
  author = "openai"
  slug   = "gpt-4o-mini"
}

data "openrouter_preset" "by_slug" {
  slug = data.openrouter_presets.all.data[0].slug
}

data "openrouter_presets" "all" {}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_workspace.test":                 {path: "/workspaces", idAttr: "id"},
			"openrouter_api_key.test":                   {path: "/keys", idAttr: "hash"},
			"openrouter_guardrail.test":                 {path: "/guardrails", idAttr: "id"},
			"openrouter_observability_destination.test": {path: "/observability/destinations", idAttr: "id"},
		}),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Required filter resolves the exact created resource.
					resource.TestCheckResourceAttr("data.openrouter_api_key.by_hash", "name", name),
					resource.TestCheckResourceAttr("data.openrouter_guardrail.by_id", "name", name),
					resource.TestCheckResourceAttr("data.openrouter_workspace.by_id", "name", name),
					resource.TestCheckResourceAttr("data.openrouter_observability_destination.by_id", "name", name),
					// Computed attributes on the singular lookups.
					resource.TestCheckResourceAttrSet("data.openrouter_api_key.by_hash", "label"),
					resource.TestCheckResourceAttrSet("data.openrouter_guardrail.by_id", "limit_usd"),
					resource.TestCheckResourceAttrSet("data.openrouter_workspace.by_id", "slug"),
					// No top-level "type" field on Read: the discriminated union
					// decodes into the matching nested block (here, webhook).
					resource.TestCheckResourceAttr("data.openrouter_observability_destination.by_id", "webhook.config.url", "https://example.com/tf-acc-ds"),
					// Public catalog singular lookups.
					resource.TestCheckResourceAttrSet("data.openrouter_model.by_slug", "data.id"),
					resource.TestCheckResourceAttrSet("data.openrouter_preset.by_slug", "data.name"),
				),
			},
		},
	})
}

// TestAccDataSources_SingularNotFound proves the singular lookups surface a
// clean error (not a panic / empty state) when the target does not exist.
func TestAccDataSources_SingularNotFound(t *testing.T) {
	name := testName("ds-404")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig() + fmt.Sprintf(`
data "openrouter_workspace" "missing" {
  id = "ws_does_not_exist_%[1]s"
}
`, name),
				ExpectError: regexp.MustCompile(`(?i)not found|error`),
			},
			{
				Config: providerConfig() + fmt.Sprintf(`
data "openrouter_guardrail" "missing" {
  id = "gr_does_not_exist_%[1]s"
}
`, name),
				ExpectError: regexp.MustCompile(`(?i)not found|error`),
			},
			{
				Config: providerConfig() + `
data "openrouter_api_key" "missing" {
  hash = "sk-or-v1-doesnotexist00000000000000000000000000000000000000000000000000"
}
`,
				ExpectError: regexp.MustCompile(`(?i)not found|error`),
			},
			{
				Config: providerConfig() + `
data "openrouter_model" "missing" {
  author = "definitely-not-an-org"
  slug   = "definitely-not-a-model"
}
`,
				ExpectError: regexp.MustCompile(`(?i)not found|error`),
			},
			{
				Config: providerConfig() + `
data "openrouter_preset" "missing" {
  slug = "definitely-not-a-preset"
}
`,
				ExpectError: regexp.MustCompile(`(?i)not found|error`),
			},
			{
				Config: providerConfig() + fmt.Sprintf(`
data "openrouter_observability_destination" "missing" {
  id = "dest_does_not_exist_%[1]s"
}
`, name),
				ExpectError: regexp.MustCompile(`(?i)not found|error`),
			},
		},
	})
}

// TestAccDataSources_Byok covers the BYOK singular lookup in its only
// practical form: the org created for acceptance testing has no BYOK keys,
// so the lookup of a non-existent id must produce a clean error. (Creating
// a real BYOK credential requires a third-party provider secret, which we
// refuse to place in CI — see the note in acceptance_test.go.)
func TestAccDataSources_Byok(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig() + `
data "openrouter_byok_key" "missing" {
  id = "byok_does_not_exist"
}
`,
				ExpectError: regexp.MustCompile(`(?i)not found|error`),
			},
		},
	})
}

// TestAccDataSources_ObservabilityDestinationsList covers the plural
// openrouter_observability_destinations lookup, filtered to a workspace
// created fresh in this test. That makes the result set deterministic (no
// dependence on list ordering or destinations left behind by other tests)
// and lets total_count and the cross-reference to the fixture resource's id
// assert an exact, non-vacuous match rather than just "the list is non-empty".
func TestAccDataSources_ObservabilityDestinationsList(t *testing.T) {
	name := testName("ds-list")

	config := providerConfig() + fmt.Sprintf(`
resource "openrouter_workspace" "test" {
  name = %[1]q
  slug = %[1]q
}

resource "openrouter_observability_destination" "test" {
  name         = %[1]q
  type         = "webhook"
  enabled      = false
  workspace_id = openrouter_workspace.test.id
  config = {
    url    = jsonencode("https://example.com/tf-acc-ds-list")
    method = jsonencode("POST")
  }
}

data "openrouter_observability_destinations" "by_workspace" {
  workspace_id = openrouter_workspace.test.id
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_workspace.test":                 {path: "/workspaces", idAttr: "id"},
			"openrouter_observability_destination.test": {path: "/observability/destinations", idAttr: "id"},
		}),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// The fresh workspace has no other destinations, so the
					// workspace_id filter deterministically returns exactly one.
					resource.TestCheckResourceAttr("data.openrouter_observability_destinations.by_workspace", "total_count", "1"),
					resource.TestCheckResourceAttr("data.openrouter_observability_destinations.by_workspace", "data.#", "1"),
					// Cross-reference against the fixture resource's own id,
					// rather than assuming which list index it lands at.
					resource.TestCheckResourceAttrPair(
						"data.openrouter_observability_destinations.by_workspace", "data.0.id",
						"openrouter_observability_destination.test", "id",
					),
					resource.TestCheckResourceAttr("data.openrouter_observability_destinations.by_workspace", "data.0.name", name),
					resource.TestCheckResourceAttr("data.openrouter_observability_destinations.by_workspace", "data.0.webhook.config.url", "https://example.com/tf-acc-ds-list"),
				),
			},
		},
	})
}
