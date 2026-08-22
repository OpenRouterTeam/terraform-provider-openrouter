package acceptance

// DEV-709: live CRUD/import coverage for openrouter_scim_group_mapping.
//
// The resource maps an IdP-pushed SCIM group to a workspace role, so the
// test needs a real group in the TEST organization. Groups only exist if an
// identity provider has synced them; when none are present the test skips
// rather than fails (the CRUD surface is still exercised by any org with
// SSO connected).

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// firstScimGroup returns the id of the newest SCIM group in the test org,
// or "" when the org has none.
func firstScimGroup(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := apiRequest(ctx, "GET", "/scim/groups", nil)
	if err != nil {
		t.Skipf("listing SCIM groups failed: %v", err)
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || len(resp.Data) == 0 {
		t.Skip("test org has no SCIM groups; skipping mapping CRUD")
	}
	return resp.Data[0].ID
}

// firstWorkspaceID returns a workspace the mapping can target.
func firstWorkspaceID(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := apiRequest(ctx, "GET", "/workspaces", nil)
	if err != nil {
		t.Skipf("listing workspaces failed: %v", err)
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || len(resp.Data) == 0 {
		t.Skip("test org has no workspaces; skipping mapping CRUD")
	}
	return resp.Data[0].ID
}

func TestAccScimGroupMapping_Lifecycle(t *testing.T) {
	groupID := firstScimGroup(t)
	workspaceID := firstWorkspaceID(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy: testAccCheckDestroy(map[string]destroyTarget{
			"openrouter_scim_group_mapping.test": {path: "/scim/group-mappings", idAttr: "id"},
		}),
		Steps: []resource.TestStep{
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_scim_group_mapping" "test" {
  scim_group_id = %q
  workspace_id  = %q
  role          = "member"
}
`, groupID, workspaceID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("openrouter_scim_group_mapping.test", "scim_group_id", groupID),
					resource.TestCheckResourceAttr("openrouter_scim_group_mapping.test", "workspace_id", workspaceID),
					resource.TestCheckResourceAttr("openrouter_scim_group_mapping.test", "role", "member"),
					resource.TestCheckResourceAttrSet("openrouter_scim_group_mapping.test", "id"),
				),
			},
			{
				ResourceName:      "openrouter_scim_group_mapping.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: providerConfig() + fmt.Sprintf(`
resource "openrouter_scim_group_mapping" "test" {
  scim_group_id = %q
  workspace_id  = %q
  role          = "admin"
}
`, groupID, workspaceID),
				Check: resource.TestCheckResourceAttr("openrouter_scim_group_mapping.test", "role", "admin"),
			},
		},
	})
}
