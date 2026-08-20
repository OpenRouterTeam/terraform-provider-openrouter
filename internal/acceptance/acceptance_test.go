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
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
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

// s3DriftPrefix and s3DriftPathTemplate are the exact customer values from
// Linear DEV-856. Both are deliberately different from the generated
// schema's static Defaults for these fields ("openrouter-traces" and
// "{prefix}/{date}", see s3.config.prefix / s3.config.path_template in
// observabilitydestination_resource.go) -- the drift this test reproduces
// only surfaces when the remote value diverges from the baked-in default.
const (
	s3DriftPrefix       = "nonprod-coreml"
	s3DriftPathTemplate = "{prefix}/{apiKeyName}/{year}/{month}/{day}"
)

// TestAccObservabilityDestination_S3PathTemplateDrift reproduces DEV-856: a
// perpetual (non-converging) plan diff on the S3 observability destination's
// s3.config.prefix / s3.config.path_template. The AWS values below are
// placeholders -- this test only exercises the provider's plan/diff logic,
// never a real S3 write, and the Management API does not validate them at
// creation time.
//
// Both TestSteps assert the OPPOSITE of steady-state on purpose:
// ExpectNonEmptyPlan requires a non-empty refresh plan and fails loudly
// ("Expected a non-empty plan, but got an empty refresh plan") if the
// defect does not reproduce, per DEV-856 step 5. Do not "fix" this test by
// removing ExpectNonEmptyPlan -- once the provider fix lands, this test
// should be rewritten to assert an EMPTY plan instead.
func TestAccObservabilityDestination_S3PathTemplateDrift(t *testing.T) {
	name := testName("s3-drift")
	resourceAddress := "openrouter_observability_destination.s3_drift"

	config := providerConfig() + fmt.Sprintf(`
resource "openrouter_observability_destination" "s3_drift" {
  name    = %q
  type    = "s3"
  enabled = false
  config = {
    bucketName      = jsonencode("tf-acc-dev-856-bucket")
    region          = jsonencode("us-east-1")
    accessKeyId     = jsonencode("AKIADEV856EXAMPLEKEY")
    secretAccessKey = jsonencode("dev856-example-secret-access-key-not-real")
    prefix          = jsonencode(%q)
    pathTemplate    = jsonencode(%q)
  }
}
`, name, s3DriftPrefix, s3DriftPathTemplate)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Create, then capture the remote values the API round-tripped
				// back (proving the API itself stored the customer's exact
				// prefix/path_template correctly, before the provider's own
				// plan/diff logic gets a chance to reintroduce drift).
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceAddress, "type", "s3"),
					resource.TestCheckResourceAttrSet(resourceAddress, "id"),
					resource.TestCheckResourceAttr(resourceAddress, "s3.config.prefix", s3DriftPrefix),
					resource.TestCheckResourceAttr(resourceAddress, "s3.config.path_template", s3DriftPathTemplate),
					logNonSecretDestinationState(t, "post-apply state", resourceAddress),
				),
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{requireS3ConfigDrift(t, "first plan (pre-refresh)", resourceAddress)},
					PostApplyPostRefresh: []plancheck.PlanCheck{requireS3ConfigDrift(t, "first plan (refreshed)", resourceAddress)},
				},
			},
			{
				// A second, independent plan invocation: proves the diff is
				// perpetual (recurs on every plan) rather than a one-off
				// artifact of the create step.
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh:  []plancheck.PlanCheck{requireS3ConfigDrift(t, "repeated plan (pre-refresh)", resourceAddress)},
					PostApplyPostRefresh: []plancheck.PlanCheck{requireS3ConfigDrift(t, "repeated plan (refreshed)", resourceAddress)},
				},
			},
		},
	})
}

// logNonSecretDestinationState logs a fixed allowlist of non-secret
// attributes from post-apply state as DEV-856 evidence. It never reads
// "config" (the flat map holding the AWS credentials) or any s3.config.*
// field the schema marks Sensitive.
func logNonSecretDestinationState(t *testing.T, label, resourceAddress string) resource.TestCheckFunc {
	t.Helper()
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceAddress]
		if !ok {
			return fmt.Errorf("%s: resource %s not found in state", label, resourceAddress)
		}
		attrs := rs.Primary.Attributes
		t.Logf(
			"DEV-856 evidence [%s]: id=%q type=%q enabled=%q s3.config.prefix=%q s3.config.path_template=%q",
			label, attrs["id"], attrs["type"], attrs["enabled"], attrs["s3.config.prefix"], attrs["s3.config.path_template"],
		)
		return nil
	}
}

// requireS3ConfigDrift logs the before/after values of s3.config.prefix and
// s3.config.path_template for resourceAddress, and fails loudly if the plan
// is non-empty for some reason OTHER than those two fields drifting -- a
// non-empty plan caused by something unrelated is not DEV-856 evidence.
//
// It deliberately never touches "config" or any other s3.config.* field:
// Terraform's JSON plan format (unlike its human-readable CLI output) does
// not redact attributes the schema marks Sensitive, so those must never be
// read from req.Plan here.
func requireS3ConfigDrift(t *testing.T, label, resourceAddress string) plancheck.PlanCheck {
	t.Helper()
	return planCheckFunc(func(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
		for _, rc := range req.Plan.ResourceChanges {
			if rc.Address != resourceAddress {
				continue
			}

			prefixBefore := extractS3ConfigField(rc.Change.Before, "prefix")
			prefixAfter := extractS3ConfigField(rc.Change.After, "prefix")
			pathBefore := extractS3ConfigField(rc.Change.Before, "path_template")
			pathAfter := extractS3ConfigField(rc.Change.After, "path_template")

			t.Logf(
				"DEV-856 evidence [%s]: actions=%v s3.config.prefix %q -> %q, s3.config.path_template %q -> %q",
				label, rc.Change.Actions, prefixBefore, prefixAfter, pathBefore, pathAfter,
			)

			if prefixBefore == prefixAfter && pathBefore == pathAfter {
				resp.Error = fmt.Errorf(
					"%s: plan for %s was non-empty, but s3.config.prefix (%q) and s3.config.path_template (%q) were both stable -- this is not the DEV-856 drift, investigate the actual diff",
					label, resourceAddress, prefixAfter, pathAfter,
				)
			}
			return
		}
		resp.Error = fmt.Errorf("%s: resource %s not found in plan", label, resourceAddress)
	})
}

// extractS3ConfigField reads a single field from the s3.config nested object
// inside a raw plan Before/After value (a map[string]interface{} produced by
// unmarshalling `terraform show -json`). Returns "<unavailable>" if any level
// of the path is missing or not the expected shape -- e.g. Before on create,
// or a field that has not yet resolved from unknown.
func extractS3ConfigField(raw interface{}, field string) string {
	root, ok := raw.(map[string]interface{})
	if !ok {
		return "<unavailable>"
	}
	s3, ok := root["s3"].(map[string]interface{})
	if !ok {
		return "<unavailable>"
	}
	config, ok := s3["config"].(map[string]interface{})
	if !ok {
		return "<unavailable>"
	}
	value, ok := config[field].(string)
	if !ok {
		return "<unavailable>"
	}
	return value
}

// planCheckFunc adapts a function literal to the plancheck.PlanCheck
// interface, mirroring the standard library's http.HandlerFunc pattern.
type planCheckFunc func(context.Context, plancheck.CheckPlanRequest, *plancheck.CheckPlanResponse)

func (f planCheckFunc) CheckPlan(ctx context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	f(ctx, req, resp)
}

// NOTE: openrouter_byok_key is intentionally NOT covered here. It requires a
// real provider credential (encrypted at rest, never echoed back), which we
// refuse to place in CI secrets. Validate manually per DEV-488.
