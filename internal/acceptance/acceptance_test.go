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
//	OPENROUTER_BASE_URL        optional; point the harness (provider config
//	                           and sweeper) at a staging API base URL.
package acceptance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/provider"
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

// testAccServerURL returns the optional OPENROUTER_BASE_URL override for
// pointing acceptance tests at staging (empty string = production default).
func testAccServerURL() string {
	return os.Getenv("OPENROUTER_BASE_URL")
}

// testAccAPIBase returns the base URL used for direct API calls (sweeper),
// mirroring the provider's server_url default.
func testAccAPIBase() string {
	if u := testAccServerURL(); u != "" {
		return u
	}
	return "https://openrouter.ai/api/v1"
}

// providerConfig renders the provider block. The generated provider has no
// environment-variable fallback for api_key, so the key is interpolated
// into configuration here (state stays on the ephemeral runner). When
// OPENROUTER_BASE_URL is set, server_url is rendered too so the whole
// harness can be pointed at a staging environment.
func providerConfig() string {
	serverURL := ""
	if u := testAccServerURL(); u != "" {
		serverURL = fmt.Sprintf("\n  server_url = %q", u)
	}
	return fmt.Sprintf(`
provider "openrouter" {
  api_key = %q%s
}
`, os.Getenv("OPENROUTER_MANAGEMENT_KEY"), serverURL)
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

// testAccCheckDestroy verifies, against the live Management API, that every
// resource Terraform destroyed really is gone. terraform-plugin-testing's
// built-in destroy check only inspects state; this closes the loop by
// re-reading each tracked object and requiring a 404. Response bodies are
// drained but never inspected or logged: some resource types echo
// configuration that other tests mark sensitive.
func testAccCheckDestroy(resources map[string]destroyTarget) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		for stateName, target := range resources {
			rs, ok := s.RootModule().Resources[stateName]
			if !ok || rs.Primary == nil {
				continue
			}
			idValue := rs.Primary.Attributes[target.idAttr]
			if idValue == "" {
				return fmt.Errorf("CheckDestroy: %s has no %q in state", stateName, target.idAttr)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet,
				testAccAPIBase()+target.path+"/"+idValue, nil)
			if err != nil {
				return fmt.Errorf("CheckDestroy: build request for %s: %w", stateName, err)
			}
			req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENROUTER_MANAGEMENT_KEY"))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("CheckDestroy: %s: %w", stateName, err)
			}
			// Drain before closing (never log): the body may echo
			// sensitive configuration for other resource types.
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				return fmt.Errorf("CheckDestroy: resource still exists after destroy (state=%s management_path=%s public_id=%s status=%d)",
					stateName, target.path, idValue, resp.StatusCode)
			}
		}
		return nil
	}
}

// destroyTarget describes how to re-read one deleted resource through the
// Management API for CheckDestroy.
type destroyTarget struct {
	path   string // collection path, e.g. "/keys"
	idAttr string // state attribute holding the object identifier
}

/*
 * The TestCheckResourceAttr lines in each create step below only prove
 * config/state consistency, not a live-API round trip: Create's
 * refreshPlan (internal/provider/utils.go) copies known plan values back
 * over whatever the API returned, before State.Set. A field set from a
 * config literal therefore always reads back as that literal, regardless
 * of what the server actually stored. The ImportStateVerify step right
 * after each create is what proves the value survived the live API,
 * because Read never calls refreshPlan. Do not add these fields to
 * ImportStateVerifyIgnore, and do not remove the import steps as
 * redundant with the create-step checks above them.
 */

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
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_api_key.test": {path: "/keys", idAttr: "hash"},
		}),
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
					// limit=0 so this key can never spend even if leaked.
					resource.TestCheckResourceAttr("openrouter_api_key.test", "limit", "0"),
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
				// The resource's identifier attribute is hash, not id.
				ImportStateVerifyIdentifierAttribute: "hash",
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
//
// reset_interval is always paired with limit_usd: the API enforces this via
// a server-side rule that the OpenAPI spec does not declare (Linear
// ENT-1743). If ENT-1743 lands as a spec constraint, generated validation
// will enforce it client-side too.
func TestAccGuardrail_Lifecycle(t *testing.T) {
	name := testName("guardrail")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_guardrail.test": {path: "/guardrails", idAttr: "id"},
		}),
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
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "description", "acceptance test guardrail"),
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "limit_usd", "1"),
					resource.TestCheckResourceAttr("openrouter_guardrail.test", "reset_interval", "monthly"),
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
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_workspace.test": {path: "/workspaces", idAttr: "id"},
		}),
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
					resource.TestCheckResourceAttr("openrouter_workspace.test", "slug", name),
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
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_observability_destination.test": {path: "/observability/destinations", idAttr: "id"},
		}),
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
					resource.TestCheckResourceAttr("openrouter_observability_destination.test", "name", name),
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

// TestAccObservabilityDestination_Import is split out of
// TestAccObservabilityDestination_Lifecycle as a whole-test skip for a
// tracked product bug (DEV-872), not an ImportStateVerifyIgnore entry: the
// API contract does return top-level "type" on GET, so ignoring it here
// would convert a real defect into a silent pass.
func TestAccObservabilityDestination_Import(t *testing.T) {
	t.Skip("DEV-872: generated Read mapper never assigns 'type', so import produces incomplete state and ImportStateVerify fails; remove this skip when DEV-872 lands — https://linear.app/openrouter/issue/DEV-872")

	name := testName("dest")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_observability_destination.test": {path: "/observability/destinations", idAttr: "id"},
		}),
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
					resource.TestCheckResourceAttr("openrouter_observability_destination.test", "name", name),
					resource.TestCheckResourceAttr("openrouter_observability_destination.test", "type", "webhook"),
					resource.TestCheckResourceAttr("openrouter_observability_destination.test", "enabled", "false"),
					resource.TestCheckResourceAttrSet("openrouter_observability_destination.test", "id"),
				),
			},
			{
				ResourceName:      "openrouter_observability_destination.test",
				ImportState:       true,
				ImportStateVerify: true,
				// config is an open map: the API echoes typed structure that
				// may not byte-match the import's sparse state, so verify the
				// stable fields only.
				ImportStateVerifyIgnore: []string{"config"},
			},
		},
	})
}

// NOTE: openrouter_byok_key is intentionally NOT covered here. It requires a
// real provider credential (encrypted at rest, never echoed back), which we
// refuse to place in CI secrets. Validate manually per DEV-488.
