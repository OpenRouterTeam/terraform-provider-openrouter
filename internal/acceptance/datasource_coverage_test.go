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

// No positive openrouter_preset lookup here: the provider has no resource
// that can create a preset, and the account's preset list is not guaranteed
// non-empty. The not-found case is covered by TestAccDataSources_SingularNotFound.
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
// openrouter_observability_destinations lookup, filtered by workspace_id.
//
// The data source read is split into its own step, after the fixture
// workspace and destination are fully created and applied in step 1. A data
// source with no dependency edge to a resource can be read during that
// resource's own plan/apply, before it exists — which is what actually
// produced total_count=0 in two earlier variants of this test: the original
// filtered variant (whose data source depended only on
// openrouter_workspace.test.id, never on the destination) and the
// unfiltered variant that replaced it (which depended on neither resource).
// Neither failure was evidence that create drops workspace_id or that the
// workspace_id filter is broken; both were the same ordering race. Putting
// the data source in step 2 guarantees it is read against a state where
// step 1 has already applied (proven live) rather than racing it.
//
// Residual assumption for the protected run to confirm: that, ordering
// fixed, the workspace_id filter on GET /observability/destinations
// (documented in .speakeasy/out.openapi.yaml as "Optional workspace ID to
// filter by") returns exactly the destinations created in that workspace.
func TestAccDataSources_ObservabilityDestinationsList(t *testing.T) {
	name := testName("ds-list")

	fixtureConfig := providerConfig() + fmt.Sprintf(`
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
`, name)

	listConfig := fixtureConfig + `
data "openrouter_observability_destinations" "by_workspace" {
  workspace_id = openrouter_workspace.test.id
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_workspace.test":                 {path: "/workspaces", idAttr: "id"},
			"openrouter_observability_destination.test": {path: "/observability/destinations", idAttr: "id"},
		}),
		Steps: []resource.TestStep{
			{
				// No data source in this step: the workspace and destination are
				// created and applied against the live API before anything reads
				// the list in step 2 below.
				Config: fixtureConfig,
			},
			{
				Config: listConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.openrouter_observability_destinations.by_workspace", "total_count", "1"),
					resource.TestCheckResourceAttr("data.openrouter_observability_destinations.by_workspace", "data.#", "1"),
					resource.TestCheckResourceAttrPair("data.openrouter_observability_destinations.by_workspace", "data.0.webhook.id", "openrouter_observability_destination.test", "id"),
					resource.TestCheckResourceAttr("data.openrouter_observability_destinations.by_workspace", "data.0.webhook.name", name),
					resource.TestCheckResourceAttr("data.openrouter_observability_destinations.by_workspace", "data.0.webhook.config.url", "https://example.com/tf-acc-ds-list"),
				),
			},
		},
	})
}
