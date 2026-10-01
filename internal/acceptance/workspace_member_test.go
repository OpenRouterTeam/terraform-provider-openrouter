package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	wmWorkspaceID        = "4d1f0c2e-7a3b-4c5d-9e6f-0a1b2c3d4e5f"
	wmWorkspaceSlug      = "engineering"
	wmDefaultWorkspaceID = "0a0b0c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d"
	wmAdminUserID        = "user_2synthAdminAAAAAAAAAAAA"
	wmMemberUserID       = "user_2synthMemberBBBBBBBBBBB"
	wmScimUserID         = "user_2synthScimCCCCCCCCCCCCC"
	wmNonMemberUserID    = "user_2synthNonMemberZZZZZZZZ"
	wmResourceName       = "openrouter_workspace_member.test"
)

// Mirrors the workspace member endpoints: members must belong to the
// organization and take their organization role, adding is idempotent, the
// default workspace rejects changes, and removal is refused for SCIM-managed
// members and members owning API keys in the workspace.
type stubWorkspaceMembers struct {
	mu         sync.Mutex
	orgRoles   map[string]string
	scim       map[string]bool
	keys       map[string]bool
	workspaces map[string]map[string]bool
	addCalls   int
	pageSize   int
}

func newStubWorkspaceMembers(t *testing.T) (*httptest.Server, *stubWorkspaceMembers) {
	t.Helper()
	api := &stubWorkspaceMembers{
		orgRoles: map[string]string{
			wmAdminUserID:  "admin",
			wmMemberUserID: "member",
			wmScimUserID:   "member",
		},
		scim:       map[string]bool{},
		keys:       map[string]bool{},
		workspaces: map[string]map[string]bool{wmWorkspaceID: {}},
	}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubWorkspaceMembers) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	writeError := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": status, "message": message},
		})
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "workspaces" {
		writeError(http.StatusNotFound, "Not found")
		return
	}
	ref := parts[1]
	workspaceID := ref
	if ref == wmWorkspaceSlug {
		workspaceID = wmWorkspaceID
	}
	isDefault := workspaceID == wmDefaultWorkspaceID
	members, exists := s.workspaces[workspaceID]
	if !exists && !isDefault {
		writeError(http.StatusNotFound, "Workspace not found")
		return
	}

	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"id": workspaceID, "name": "Engineering", "slug": wmWorkspaceSlug,
			"description": nil, "default_guardrail_id": nil,
			"default_image_model": nil, "default_provider_sort": nil, "default_text_model": nil,
			"io_logging_api_key_ids": nil, "io_logging_sampling_rate": 1,
			"is_data_discount_logging_enabled": true, "is_observability_broadcast_enabled": false,
			"is_observability_io_logging_enabled": false,
			"created_at":                          "2026-01-15T10:00:00Z", "created_by": nil, "updated_at": nil,
		}})
	case len(parts) == 3 && parts[2] == "members" && r.Method == http.MethodGet:
		userIDs := []string{}
		if isDefault {
			for userID := range s.orgRoles {
				userIDs = append(userIDs, userID)
			}
		} else {
			for userID := range members {
				userIDs = append(userIDs, userID)
			}
		}
		sort.Strings(userIDs)
		total := len(userIDs)
		offset, limit := 0, 100
		_, _ = fmt.Sscan(r.URL.Query().Get("offset"), &offset)
		_, _ = fmt.Sscan(r.URL.Query().Get("limit"), &limit)
		if s.pageSize > 0 && s.pageSize < limit {
			limit = s.pageSize
		}
		if offset > total {
			offset = total
		}
		end := min(offset+limit, total)
		data := []map[string]any{}
		for _, userID := range userIDs[offset:end] {
			data = append(data, s.row(workspaceID, userID))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "total_count": total})
	case len(parts) == 4 && parts[2] == "members" && r.Method == http.MethodPost && (parts[3] == "add" || parts[3] == "remove"):
		var body struct {
			UserIDs []string `json:"user_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.UserIDs) == 0 {
			writeError(http.StatusBadRequest, "user_ids is required")
			return
		}
		if isDefault {
			writeError(http.StatusForbidden, "Cannot modify members of the default workspace")
			return
		}
		if parts[3] == "add" {
			s.addCalls++
			for _, userID := range body.UserIDs {
				if _, ok := s.orgRoles[userID]; !ok {
					writeError(http.StatusBadRequest, "Some users are not members of the organization")
					return
				}
			}
			data := []map[string]any{}
			for _, userID := range body.UserIDs {
				members[userID] = true
				data = append(data, s.row(workspaceID, userID))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"added_count": len(data), "data": data})
			return
		}
		for _, userID := range body.UserIDs {
			if members[userID] && s.scim[userID] {
				writeError(http.StatusForbidden, fmt.Sprintf("The following members are SCIM-managed and cannot be removed directly: %s. Changes must be made in your identity provider.", userID))
				return
			}
			if s.keys[userID] {
				writeError(http.StatusBadRequest, fmt.Sprintf("The following members have active API keys in this workspace and cannot be removed: %s. Their keys must be deleted first.", userID))
				return
			}
		}
		count := 0
		for _, userID := range body.UserIDs {
			if members[userID] {
				delete(members, userID)
				count++
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"removed_count": count})
	default:
		writeError(http.StatusNotFound, "Not found")
	}
}

func (s *stubWorkspaceMembers) row(workspaceID, userID string) map[string]any {
	return map[string]any{
		"id": "a1b2c3d4-0000-4000-8000-000000000000", "workspace_id": workspaceID, "user_id": userID,
		"role": s.orgRoles[userID], "created_at": "2026-01-15T10:00:00Z",
	}
}

func (s *stubWorkspaceMembers) update(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

func (s *stubWorkspaceMembers) checkMembers(want ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		got := []string{}
		for userID := range s.workspaces[wmWorkspaceID] {
			got = append(got, userID)
		}
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			return fmt.Errorf("members = %v, want %v", got, want)
		}
		return nil
	}
}

func (s *stubWorkspaceMembers) checkAddCalls(want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.addCalls != want {
			return fmt.Errorf("add calls = %d, want %d", s.addCalls, want)
		}
		return nil
	}
}

func wmConfig(serverURL, workspaceID, userID string) string {
	return stubBudgetProviderConfig(serverURL) + fmt.Sprintf(`
resource "openrouter_workspace_member" "test" {
  workspace_id = %q
  user_id      = %q
}
`, workspaceID, userID)
}

// UnitTest intentionally bypasses TF_ACC: every request goes to a local fixture.
func TestStubWorkspaceMemberLifecycle(t *testing.T) {
	srv, api := newStubWorkspaceMembers(t)
	config := wmConfig(srv.URL, wmWorkspaceID, wmAdminUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkMembers(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(wmResourceName, "id", wmWorkspaceID+"/"+wmAdminUserID),
					resource.TestCheckResourceAttr(wmResourceName, "workspace_id", wmWorkspaceID),
					resource.TestCheckResourceAttr(wmResourceName, "user_id", wmAdminUserID),
					resource.TestCheckResourceAttr(wmResourceName, "role", "admin"),
					resource.TestCheckResourceAttr(wmResourceName, "created_at", "2026-01-15T10:00:00Z"),
					api.checkMembers(wmAdminUserID),
					api.checkAddCalls(1),
				),
			},
			{Config: config, PlanOnly: true},
			{
				ResourceName:      wmResourceName,
				ImportState:       true,
				ImportStateId:     wmWorkspaceID + "/" + wmAdminUserID,
				ImportStateVerify: true,
			},
			{
				Config: wmConfig(srv.URL, wmWorkspaceID, wmMemberUserID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(wmResourceName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(wmResourceName, "role", "member"),
					api.checkMembers(wmMemberUserID),
				),
			},
		},
	})
}

// Existing members are adopted, and members the configuration does not declare
// are left alone on destroy.
func TestStubWorkspaceMemberLeavesUndeclaredMembers(t *testing.T) {
	srv, api := newStubWorkspaceMembers(t)
	api.update(func() {
		api.workspaces[wmWorkspaceID][wmAdminUserID] = true
		api.workspaces[wmWorkspaceID][wmMemberUserID] = true
	})
	config := wmConfig(srv.URL, wmWorkspaceID, wmAdminUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkMembers(wmMemberUserID),
		Steps: []resource.TestStep{
			{Config: config, Check: api.checkMembers(wmAdminUserID, wmMemberUserID)},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestStubWorkspaceMemberPaginates(t *testing.T) {
	srv, api := newStubWorkspaceMembers(t)
	api.update(func() {
		api.pageSize = 1
		api.workspaces[wmWorkspaceID][wmAdminUserID] = true
		api.workspaces[wmWorkspaceID][wmMemberUserID] = true
	})
	config := wmConfig(srv.URL, wmWorkspaceID, wmScimUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestStubWorkspaceMemberRecreatesAfterOutOfBandRemoval(t *testing.T) {
	srv, api := newStubWorkspaceMembers(t)
	config := wmConfig(srv.URL, wmWorkspaceID, wmAdminUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { api.update(func() { delete(api.workspaces[wmWorkspaceID], wmAdminUserID) }) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(wmResourceName, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					api.checkMembers(wmAdminUserID),
					api.checkAddCalls(2),
				),
			},
		},
	})
}

func TestStubWorkspaceMemberWorkspaceDeleted(t *testing.T) {
	srv, api := newStubWorkspaceMembers(t)
	config := wmConfig(srv.URL, wmWorkspaceID, wmAdminUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { api.update(func() { delete(api.workspaces, wmWorkspaceID) }) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`workspace not found`),
			},
			{
				Config:  stubBudgetProviderConfig(srv.URL),
				Destroy: true,
			},
		},
	})
}

func TestStubWorkspaceMemberRejectsInvalidInput(t *testing.T) {
	srv, api := newStubWorkspaceMembers(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      wmConfig(srv.URL, wmWorkspaceID, wmNonMemberUserID),
				ExpectError: regexp.MustCompile(`not members of the organization`),
			},
			{
				Config:      wmConfig(srv.URL, wmWorkspaceSlug, wmAdminUserID),
				ExpectError: regexp.MustCompile(`workspace_id must be a workspace ID`),
			},
			{
				Config:      wmConfig(srv.URL, wmDefaultWorkspaceID, wmAdminUserID),
				ExpectError: regexp.MustCompile(`Cannot modify members of the default workspace`),
				Check:       api.checkMembers(),
			},
			{
				Config:      wmConfig(srv.URL, "", wmAdminUserID),
				ExpectError: regexp.MustCompile(`string length must be at least 1`),
			},
		},
	})
}

func TestStubWorkspaceMemberImportRejectsInvalidOrMissing(t *testing.T) {
	srv, api := newStubWorkspaceMembers(t)
	api.update(func() { api.workspaces[wmWorkspaceID][wmAdminUserID] = true })
	config := wmConfig(srv.URL, wmWorkspaceID, wmAdminUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:        config,
				ResourceName:  wmResourceName,
				ImportState:   true,
				ImportStateId: wmAdminUserID,
				ExpectError:   regexp.MustCompile(`<workspace_id>/<user_id>`),
			},
			{
				Config:        config,
				ResourceName:  wmResourceName,
				ImportState:   true,
				ImportStateId: wmWorkspaceID + "/" + wmMemberUserID,
				ExpectError:   regexp.MustCompile(`Cannot import non-existent remote object`),
			},
			{
				Config:        config,
				ResourceName:  wmResourceName,
				ImportState:   true,
				ImportStateId: wmWorkspaceSlug + "/" + wmAdminUserID,
				ExpectError:   regexp.MustCompile(`workspace_id must be a workspace ID`),
			},
			{
				Config:        stubBudgetProviderConfig(srv.URL) + "\nresource \"openrouter_workspace_member\" \"missing_workspace\" {\n  workspace_id = \"00000000-0000-4000-8000-000000000000\"\n  user_id      = \"" + wmAdminUserID + "\"\n}\n",
				ResourceName:  "openrouter_workspace_member.missing_workspace",
				ImportState:   true,
				ImportStateId: "00000000-0000-4000-8000-000000000000/" + wmAdminUserID,
				ExpectError:   regexp.MustCompile(`workspace not found`),
			},
		},
	})
}

func TestStubWorkspaceMemberImportAdoptsExisting(t *testing.T) {
	srv, api := newStubWorkspaceMembers(t)
	api.update(func() { api.workspaces[wmWorkspaceID][wmAdminUserID] = true })
	config := wmConfig(srv.URL, wmWorkspaceID, wmAdminUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       wmResourceName,
				ImportState:        true,
				ImportStateId:      wmWorkspaceID + "/" + wmAdminUserID,
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d states, want 1", len(states))
					}
					if got := states[0].Attributes["workspace_id"]; got != wmWorkspaceID {
						return fmt.Errorf("workspace_id = %q, want %q", got, wmWorkspaceID)
					}
					return nil
				},
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: api.checkAddCalls(0),
			},
		},
	})
}

// Destroy surfaces the API's refusal to remove members who own API keys in the
// workspace or whose membership is managed by SCIM.
func TestStubWorkspaceMemberDeleteErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		mark func(*stubWorkspaceMembers)
		want string
	}{
		"active keys": {func(s *stubWorkspaceMembers) { s.keys[wmAdminUserID] = true }, `have active API keys`},
		"scim":        {func(s *stubWorkspaceMembers) { s.scim[wmAdminUserID] = true }, `SCIM-managed`},
	} {
		t.Run(name, func(t *testing.T) {
			srv, api := newStubWorkspaceMembers(t)
			config := wmConfig(srv.URL, wmWorkspaceID, wmAdminUserID)
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: protoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{Config: config},
					{
						PreConfig:   func() { api.update(func() { tc.mark(api) }) },
						Config:      stubBudgetProviderConfig(srv.URL),
						ExpectError: regexp.MustCompile(tc.want),
					},
					{
						PreConfig: func() {
							api.update(func() {
								delete(api.keys, wmAdminUserID)
								delete(api.scim, wmAdminUserID)
							})
						},
						Config: stubBudgetProviderConfig(srv.URL),
						Check:  api.checkMembers(),
					},
				},
			})
		})
	}
}
