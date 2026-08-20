package acceptance

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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
// openrouter_observability_destinations lookup. It creates a fixture
// destination in a fresh workspace and reads the list UNFILTERED, then
// proves the fixture's id is present in the result rather than relying on
// the workspace_id query filter.
//
// Spec-vs-live discrepancy: the resource provably sends workspace_id on
// create (ToSharedCreateObservabilityDestinationRequest in
// observabilitydestination_resource_sdk.go maps r.WorkspaceID into the
// request body), and .speakeasy/out.openapi.yaml documents workspace_id as
// a filter on GET /observability/destinations ("Optional workspace ID to
// filter by. Defaults to the authenticated entity's default workspace.").
// Despite that, a live run against a fresh workspace with a destination
// created moments earlier returned total_count=0 when filtered by that
// workspace's id — either create does not persist the requested
// workspace_id, or the list filter does not honor it. Until that is
// resolved server-side, the filter cannot be relied on for a deterministic
// assertion, so this test reads the account-wide list instead.
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

data "openrouter_observability_destinations" "all" {}
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
					resource.TestCheckResourceAttrSet("data.openrouter_observability_destinations.all", "total_count"),
					testAccCheckObservabilityDestinationInList(
						"data.openrouter_observability_destinations.all",
						"openrouter_observability_destination.test",
						name,
						"https://example.com/tf-acc-ds-list",
					),
				),
			},
		},
	})
}

// testAccCheckObservabilityDestinationInList proves the fixture resource's
// id appears somewhere in the data source's list, then checks the matching
// entry's name and webhook config url — the same fields the removed
// filtered assertion checked, just found by scanning instead of indexing.
func testAccCheckObservabilityDestinationInList(dataSourceName, resourceName, wantName, wantURL string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		fixture, ok := s.RootModule().Resources[resourceName]
		if !ok || fixture.Primary == nil {
			return fmt.Errorf("%s not found in state", resourceName)
		}
		wantID := fixture.Primary.Attributes["id"]
		if wantID == "" {
			return fmt.Errorf("%s has no id in state", resourceName)
		}

		list, ok := s.RootModule().Resources[dataSourceName]
		if !ok || list.Primary == nil {
			return fmt.Errorf("%s not found in state", dataSourceName)
		}

		count, err := strconv.Atoi(list.Primary.Attributes["data.#"])
		if err != nil {
			return fmt.Errorf("%s has no valid data.# in state: %w", dataSourceName, err)
		}

		for i := 0; i < count; i++ {
			prefix := fmt.Sprintf("data.%d.", i)
			if list.Primary.Attributes[prefix+"id"] != wantID {
				continue
			}
			if got := list.Primary.Attributes[prefix+"name"]; got != wantName {
				return fmt.Errorf("%s entry for id %s: name = %q, want %q", dataSourceName, wantID, got, wantName)
			}
			if got := list.Primary.Attributes[prefix+"webhook.config.url"]; got != wantURL {
				return fmt.Errorf("%s entry for id %s: webhook.config.url = %q, want %q", dataSourceName, wantID, got, wantURL)
			}
			return nil
		}
		return fmt.Errorf("%s: id %s from %s not found among %d listed destinations", dataSourceName, wantID, resourceName, count)
	}
}
