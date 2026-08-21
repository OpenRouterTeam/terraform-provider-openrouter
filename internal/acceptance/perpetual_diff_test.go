package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccObservabilityDestination_NoPerpetualDiff is a regression test for
// DEV-856: openrouter_observability_destination produces a non-empty plan
// immediately after apply (a perpetual diff). The harness FAILS this test
// whenever a plan step yields a non-empty plan for a config that was just
// applied, so this test fails against the current provider and passes once
// the diff bug is fixed.
//
// Note: the PlanOnly steps below intentionally omit ExpectNonEmptyPlan; the
// terraform-plugin-testing harness defaults to expecting an empty plan, and
// PlanOnly on the apply step keeps the same state so the second step is a
// true no-op refresh.
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
			// Step 1: apply and immediately plan the exact same config.
			// DEV-856: the plan is non-empty, failing this step.
			{
				Config:   config,
				PlanOnly: true,
			},
			// Step 2: full apply of the same config, to leave a real
			// resource behind and prove step 1 was a true no-op.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("openrouter_observability_destination.test", "id"),
			},
			// Step 3: no-op refresh after apply; same assertion as step 1.
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}
