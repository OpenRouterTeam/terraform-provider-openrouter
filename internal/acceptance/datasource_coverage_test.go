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
					resource.TestCheckResourceAttrSet("data.openrouter_observability_destination.by_id", "type"),
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
