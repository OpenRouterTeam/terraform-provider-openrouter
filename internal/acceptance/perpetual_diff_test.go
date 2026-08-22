package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccObservabilityDestination_NoPerpetualDiff is a regression test for
// DEV-856: openrouter_observability_destination produced a non-empty plan
// immediately after apply (a perpetual diff). Step 1 applies; the harness's
// implicit post-apply plan must be empty. Step 2 is an explicit no-op
// PlanOnly of the identical config, which the harness also requires to be
// empty (ExpectNonEmptyPlan is intentionally omitted).
func TestAccObservabilityDestination_NoPerpetualDiff(t *testing.T) {
	name := testName("dest-diff")

	config := providerConfig() + fmt.Sprintf(`
resource "openrouter_observability_destination" "test" {
  name    = %q
  type    = "webhook"
  enabled = false
  config = {
    url    = jsonencode("https://example.com/tf-acc-diff")
    method = jsonencode("POST")
  }
}
`, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			// Step 1: full apply of the config. The harness's implicit
			// post-apply plan must be empty — a non-empty one is the
			// DEV-856 perpetual diff and fails this step.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("openrouter_observability_destination.test", "id"),
			},
			// Step 2: explicit no-op plan of the identical config; must be
			// empty. (PlanOnly as a FIRST step would always fail with a
			// create plan, since nothing has been applied yet.)
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}
