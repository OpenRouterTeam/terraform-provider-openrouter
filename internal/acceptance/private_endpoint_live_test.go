package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// Live private endpoint lifecycle. Activation validates against a real
// upstream, so the organization behind OPENROUTER_MANAGEMENT_KEY needs the
// private endpoints entitlement and a workspace holding a BYOK key for the
// provider:
//
//	OPENROUTER_PRIVATE_ENDPOINT_WORKSPACE_ID  workspace with the BYOK key (test skips when unset)
//	OPENROUTER_PRIVATE_ENDPOINT_MODEL         public model permaslug (default openai/gpt-4o-mini)
//	OPENROUTER_PRIVATE_ENDPOINT_PROVIDER      provider slug (default openai)
//	OPENROUTER_PRIVATE_ENDPOINT_BASE_URL      upstream base URL (default https://api.openai.com/v1)
//	OPENROUTER_PRIVATE_ENDPOINT_UPSTREAM_A/B  two upstream model IDs the key can serve
//	                                          (defaults gpt-4o-mini / gpt-4o-mini-2024-07-18)
//	OPENROUTER_PRIVATE_ENDPOINT_NO_BYOK_WORKSPACE_ID  workspace without a BYOK key, for the failed-activation test
func TestAccPrivateEndpoint_Lifecycle(t *testing.T) {
	workspaceID := os.Getenv("OPENROUTER_PRIVATE_ENDPOINT_WORKSPACE_ID")
	if workspaceID == "" {
		t.Skip("OPENROUTER_PRIVATE_ENDPOINT_WORKSPACE_ID is not set")
	}
	upstreamA := envOr("OPENROUTER_PRIVATE_ENDPOINT_UPSTREAM_A", "gpt-4o-mini")
	upstreamB := envOr("OPENROUTER_PRIVATE_ENDPOINT_UPSTREAM_B", "gpt-4o-mini-2024-07-18")
	const active = "openrouter_private_endpoint.active"
	const draft = "openrouter_private_endpoint.draft"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			active: {path: "/private-endpoints", idAttr: "id"},
			draft:  {path: "/private-endpoints", idAttr: "id"},
		}),
		Steps: []resource.TestStep{
			{
				Config: livePrivateEndpointConfig(workspaceID, upstreamA, "0.00000012"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(active, "status", "active"),
					resource.TestCheckResourceAttr(active, "pricing.prompt", "0.00000012"),
					resource.TestCheckResourceAttrSet(active, "id"),
					resource.TestCheckResourceAttr(draft, "status", "draft"),
				),
			},
			{Config: livePrivateEndpointConfig(workspaceID, upstreamA, "0.00000012"), PlanOnly: true},
			{
				Config: livePrivateEndpointConfig(workspaceID, upstreamA, "0.0000001"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(active, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(active, "pricing.prompt", "0.0000001"),
					resource.TestCheckResourceAttr(active, "status", "active"),
				),
			},
			{
				Config: livePrivateEndpointConfig(workspaceID, upstreamB, "0.0000001"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(active, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(active, "upstream_model_id", upstreamB),
					resource.TestCheckResourceAttr(active, "status", "active"),
				),
			},
			{
				ResourceName:            active,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"activate"},
			},
			{
				ResourceName:            draft,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"draft_only"},
			},
		},
	})
}

func TestAccPrivateEndpoint_FailedActivationLeavesNoDraft(t *testing.T) {
	workspaceID := os.Getenv("OPENROUTER_PRIVATE_ENDPOINT_NO_BYOK_WORKSPACE_ID")
	if workspaceID == "" {
		t.Skip("OPENROUTER_PRIVATE_ENDPOINT_NO_BYOK_WORKSPACE_ID is not set")
	}
	upstream := testName("failed-activation")
	config := strings.Replace(livePrivateEndpointConfig(workspaceID, upstream, "0.00000012"), `resource "openrouter_private_endpoint" "draft"`, `resource "openrouter_private_endpoint" "unused"`, 1)
	config = config[:strings.Index(config, `resource "openrouter_private_endpoint" "unused"`)]
	failedApply := resource.TestStep{Config: config, ExpectError: regexp.MustCompile(`unexpected response code 422`)}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps:                    []resource.TestStep{failedApply, failedApply},
	})
	if leftovers := privateEndpointsWithUpstream(t, upstream); leftovers != 0 {
		t.Fatalf("%d private endpoints with upstream %q survived failed activations, want 0", leftovers, upstream)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func livePrivateEndpointConfig(workspaceID, upstreamModelID, prompt string) string {
	model := envOr("OPENROUTER_PRIVATE_ENDPOINT_MODEL", "openai/gpt-4o-mini")
	provider := envOr("OPENROUTER_PRIVATE_ENDPOINT_PROVIDER", "openai")
	baseURL := envOr("OPENROUTER_PRIVATE_ENDPOINT_BASE_URL", "https://api.openai.com/v1")
	return providerConfig() + fmt.Sprintf(`
resource "openrouter_private_endpoint" "active" {
  model_permaslug   = %[1]q
  provider_slug     = %[2]q
  upstream_model_id = %[3]q
  base_url          = %[4]q
  pricing = {
    prompt     = %[5]q
    completion = "0.00000048"
  }
  activate = {
    workspace_id = %[6]q
  }
}

resource "openrouter_private_endpoint" "draft" {
  model_permaslug   = %[1]q
  provider_slug     = %[2]q
  upstream_model_id = %[7]q
  base_url          = %[4]q
  draft_only        = "true"
  pricing = {
    prompt     = "0.000002"
    completion = "0.000008"
  }
}
`, model, provider, upstreamModelID, baseURL, prompt, workspaceID, testName("draft"))
}

func privateEndpointsWithUpstream(t *testing.T, upstreamModelID string) int {
	t.Helper()
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	getPrivateEndpointJSON(t, "", &list)
	matches := 0
	for _, item := range list.Data {
		var detail struct {
			Data struct {
				UpstreamModelID string `json:"upstream_model_id"`
			} `json:"data"`
		}
		getPrivateEndpointJSON(t, "/"+item.ID, &detail)
		if detail.Data.UpstreamModelID == upstreamModelID {
			matches++
		}
	}
	return matches
}

func getPrivateEndpointJSON(t *testing.T, path string, out any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, testAccAPIBase()+"/private-endpoints"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENROUTER_MANAGEMENT_KEY"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /private-endpoints%s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("GET /private-endpoints%s: decode: %v", path, err)
	}
}
