// Package acceptance contains live acceptance tests for the OpenRouter
// Terraform provider. This package is hand-maintained and lives outside
// internal/provider so Speakeasy regeneration never touches it.
//
// Tests only run when TF_ACC=1 (the terraform-plugin-testing convention);
// otherwise they skip, so `go test ./...` stays safe on PRs and laptops.
//
// Required environment:
//
//	TF_ACC=1
//	OPENROUTER_MANAGEMENT_KEY  a Management API key (sk-or-mgmt-...) issued
//	                           from a DEDICATED TEST ORGANIZATION with $0
//	                           credits. Management keys cannot call inference
//	                           endpoints, and a zero-credit org means any
//	                           inference key minted during tests is unusable.
package acceptance

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/openrouter/terraform-provider-openrouter/internal/provider"
)

// runPrefix namespaces every resource created by this test run so the
// sweeper can find and destroy leftovers from crashed runs.
const runPrefix = "tf-acc"

func protoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"openrouter": providerserver.NewProtocol6WithError(provider.New("acctest")()),
	}
}

// testAccPreCheck validates required configuration before each test.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("OPENROUTER_MANAGEMENT_KEY") == "" {
		t.Fatal("OPENROUTER_MANAGEMENT_KEY must be set for acceptance tests")
	}
}

// providerConfig renders the provider block. The generated provider has no
// environment-variable fallback for api_key, so the key is interpolated
// into configuration here (state stays on the ephemeral runner).
func providerConfig() string {
	return fmt.Sprintf(`
provider "openrouter" {
  api_key = %q
}
`, os.Getenv("OPENROUTER_MANAGEMENT_KEY"))
}

func testName(suffix string) string {
	// GITHUB_RUN_ID keeps names unique across concurrent-ish CI runs; local
	// runs fall back to the PID.
	id := os.Getenv("GITHUB_RUN_ID")
	if id == "" {
		id = fmt.Sprintf("local-%d", os.Getpid())
	}
	return fmt.Sprintf("%s-%s-%s", runPrefix, id, suffix)
}

// TestAccApiKey_Lifecycle exercises create -> import -> update on
// openrouter_api_key, including the two provider behaviors we specifically
// need proven live:
//   - disabled=true at creation drives the create#2 PATCH chain
//   - the one-time secret is captured into state on create
//
// The key is created with limit=0 so it can never spend even if leaked.
func TestAccApiKey_Lifecycle(t *testing.T) {
	name := testName("key")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_api_key" "test" {
  name     = %q
  limit    = 0
  disabled = true
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_api_key.test", "name", name),
					// disabled=true only applies via the create->update chain.
					resource.TestCheckResourceAttr("openrouter_api_key.test", "disabled", "true"),
					resource.TestCheckResourceAttrSet("openrouter_api_key.test", "hash"),
					// One-time secret must be captured at create time.
					resource.TestCheckResourceAttrSet("openrouter_api_key.test", "key"),
				),
			},
			{
				ResourceName:      "openrouter_api_key.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The resource imports by hash (no id attribute), so the
				// harness default (id) cannot resolve the import ID.
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["openrouter_api_key.test"]
					if !ok {
						return "", fmt.Errorf("resource not found in state")
					}
					return rs.Primary.Attributes["hash"], nil
				},
				// key: never returned by GET; only present from the create
				// response.
				ImportStateVerifyIgnore: []string{"key"},
			},
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_api_key" "test" {
  name     = %q
  limit    = 0
  disabled = false
}
`, name+"-renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_api_key.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("openrouter_api_key.test", "disabled", "false"),
				),
			},
		},
	})
}

// TestAccGuardrail_Lifecycle exercises create -> import -> update on
// openrouter_guardrail.
func TestAccGuardrail_Lifecycle(t *testing.T) {
	name := testName("guardrail")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_guardrail" "test" {
  name           = %q
  description    = "acceptance test guardrail"
  limit_usd      = 1
  reset_interval = "monthly"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "name", name),
					resource.TestCheckResourceAttrSet("openrouter_guardrail.test", "id"),
				),
			},
			{
				ResourceName:      "openrouter_guardrail.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_guardrail" "test" {
  name           = %q
  description    = "acceptance test guardrail (updated)"
  limit_usd      = 2
  reset_interval = "monthly"
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "description", "acceptance test guardrail (updated)"),
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "limit_usd", "2"),
				),
			},
		},
	})
}

// TestAccWorkspace_Lifecycle exercises create -> import -> update on
// openrouter_workspace.
func TestAccWorkspace_Lifecycle(t *testing.T) {
	name := testName("ws")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_workspace" "test" {
  name = %q
  slug = %q
}
`, name, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_workspace.test", "name", name),
					resource.TestCheckResourceAttrSet("openrouter_workspace.test", "id"),
				),
			},
			{
				ResourceName:      "openrouter_workspace.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_workspace" "test" {
  name        = %q
  slug        = %q
  description = "acceptance test workspace"
}
`, name, name),
				Check: resource.TestCheckResourceAttr("openrouter_workspace.test", "description", "acceptance test workspace"),
			},
		},
	})
}

// TestAccObservabilityDestination_Lifecycle exercises the discriminated-union
// resource with the webhook variant: no third-party credentials required and
// the endpoint is inert. Also covers the open config map round-trip (the
// input map must not drift against the computed typed variant).
func TestAccObservabilityDestination_Lifecycle(t *testing.T) {
	name := testName("dest")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_observability_destination" "test" {
  name    = %q
  type    = "webhook"
  enabled = false
  config = {
    url    = jsonencode("https://example.com/tf-acc")
    method = jsonencode("POST")
  }
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_observability_destination.test", "type", "webhook"),
					resource.TestCheckResourceAttr("openrouter_observability_destination.test", "enabled", "false"),
					resource.TestCheckResourceAttrSet("openrouter_observability_destination.test", "id"),
				),
			},
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_observability_destination" "test" {
  name    = %q
  type    = "webhook"
  enabled = false
  config = {
    url    = jsonencode("https://example.com/tf-acc-updated")
    method = jsonencode("POST")
  }
}
`, name),
				Check: resource.TestCheckResourceAttrSet("openrouter_observability_destination.test", "id"),
			},
			// Second plan after update must be empty: catches perpetual-diff
			// regressions in the open config map handling.
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_observability_destination" "test" {
  name    = %q
  type    = "webhook"
  enabled = false
  config = {
    url    = jsonencode("https://example.com/tf-acc-updated")
    method = jsonencode("POST")
  }
}
`, name),
				PlanOnly: true,
			},
		},
	})
}

// NOTE: openrouter_byok_key is intentionally NOT covered here. It requires a
// real provider credential (encrypted at rest, never echoed back), which we
// refuse to place in CI secrets. Validate manually per DEV-488.
