package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	gmaWorkspaceID        = "4d1f0c2e-7a3b-4c5d-9e6f-0a1b2c3d4e5f"
	gmaWorkspaceSlug      = "engineering"
	gmaOtherWorkspaceID   = "9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4"
	gmaDefaultGuardrailID = "0f1e2d3c-4b5a-5968-8776-a5b4c3d2e1f0"
	gmaGuardrailID        = "6a5b4c3d-2e1f-4a0b-9c8d-7e6f5a4b3c2d"
	gmaSecondGuardrailID  = "1b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e"
	gmaOtherGuardrailID   = "7c6b5a49-3827-4165-9f4e-3d2c1b0a9f8e"
	gmaLegacyGuardrailID  = "2c3d4e5f-6a7b-4c8d-9e0f-1a2b3c4d5e6f"
	gmaUserID             = "user_2synthMemberAAAAAAAAAAA"
	gmaSecondUserID       = "user_2synthMemberBBBBBBBBBBB"
	gmaOrganizationID     = "org_2synthOrganizationAAAAAAA"
	gmaResourceName       = "openrouter_guardrail_member_assignment.test"
)

type stubMemberAssignmentRow struct {
	userID      string
	workspaceID string
	guardrailID string
}

// Mirrors the guardrail member assignment endpoints: a member has at most one
// direct guardrail per workspace (assigning again moves the member), removal
// only deletes rows of the named guardrail, and default or unscoped
// guardrails reject assignment changes.
type stubMemberAssignments struct {
	mu            sync.Mutex
	guardrails    map[string]*string
	members       map[string]map[string]bool
	rows          []stubMemberAssignmentRow
	assignCalls   []string
	unassignCalls []string
	failList      bool
	failAssign    bool
}

func newStubMemberAssignments(t *testing.T) (*httptest.Server, *stubMemberAssignments) {
	t.Helper()
	ws, other := gmaWorkspaceID, gmaOtherWorkspaceID
	defaultWS := gmaWorkspaceID
	api := &stubMemberAssignments{
		guardrails: map[string]*string{
			gmaDefaultGuardrailID: &defaultWS,
			gmaGuardrailID:        &ws,
			gmaSecondGuardrailID:  &ws,
			gmaOtherGuardrailID:   &other,
			gmaLegacyGuardrailID:  nil,
		},
		members: map[string]map[string]bool{
			gmaWorkspaceID:      {gmaUserID: true, gmaSecondUserID: true},
			gmaOtherWorkspaceID: {gmaUserID: true},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubMemberAssignments) serveHTTP(w http.ResponseWriter, r *http.Request) {
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

	if len(parts) == 2 && parts[0] == "workspaces" && r.Method == http.MethodGet {
		if parts[1] != gmaWorkspaceID && parts[1] != gmaWorkspaceSlug {
			writeError(http.StatusNotFound, "Workspace not found")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"id": gmaWorkspaceID, "name": "Engineering", "slug": gmaWorkspaceSlug,
			"description": nil, "default_guardrail_id": gmaDefaultGuardrailID,
			"default_image_model": nil, "default_provider_sort": nil, "default_text_model": nil,
			"io_logging_api_key_ids": nil, "io_logging_sampling_rate": 1,
			"is_data_discount_logging_enabled": true, "is_observability_broadcast_enabled": false,
			"is_observability_io_logging_enabled": false,
			"created_at":                          "2026-01-15T10:00:00Z", "created_by": nil, "updated_at": nil,
		}})
		return
	}

	if len(parts) < 2 || parts[0] != "guardrails" {
		writeError(http.StatusNotFound, "Not found")
		return
	}
	guardrailID := parts[1]
	workspaceID, exists := s.guardrails[guardrailID]
	if !exists {
		writeError(http.StatusNotFound, "Guardrail not found")
		return
	}

	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"id": guardrailID, "name": "Budget guardrail", "description": nil,
			"limit_usd": 50, "reset_interval": "monthly", "include_byok_in_budgets": false,
			"workspace_id": workspaceID, "created_at": "2026-01-15T10:00:00Z", "updated_at": nil,
		}})
	case len(parts) == 4 && parts[2] == "assignments" && parts[3] == "members" && r.Method == http.MethodGet:
		if s.failList {
			writeError(http.StatusUnauthorized, "Missing Authentication header")
			return
		}
		data := []map[string]any{}
		for _, row := range s.rows {
			if row.guardrailID == guardrailID {
				data = append(data, map[string]any{
					"id": "a1b2c3d4-0000-4000-8000-" + fmt.Sprintf("%012d", len(data)), "user_id": row.userID,
					"organization_id": gmaOrganizationID, "guardrail_id": guardrailID,
					"assigned_by": gmaSecondUserID, "created_at": "2026-01-15T10:00:00Z",
				})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "total_count": len(data)})
	case len(parts) >= 4 && parts[2] == "assignments" && parts[3] == "members" && r.Method == http.MethodPost:
		remove := len(parts) == 5 && parts[4] == "remove"
		if len(parts) == 5 && !remove {
			writeError(http.StatusNotFound, "Not found")
			return
		}
		var body struct {
			MemberUserIDs []string `json:"member_user_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.MemberUserIDs) == 0 {
			writeError(http.StatusBadRequest, "member_user_ids is required")
			return
		}
		if workspaceID == nil {
			writeError(http.StatusBadRequest, "Guardrail has no workspace_id")
			return
		}
		if guardrailID == gmaDefaultGuardrailID {
			writeError(http.StatusBadRequest, "Cannot assign members to a default guardrail. Default guardrails apply to all unassigned members automatically.")
			return
		}
		if remove {
			s.unassignCalls = append(s.unassignCalls, guardrailID+"/"+strings.Join(body.MemberUserIDs, ","))
			count := 0
			kept := s.rows[:0]
			for _, row := range s.rows {
				if row.guardrailID == guardrailID && row.workspaceID == *workspaceID && contains(body.MemberUserIDs, row.userID) {
					count++
					continue
				}
				kept = append(kept, row)
			}
			s.rows = kept
			_ = json.NewEncoder(w).Encode(map[string]any{"unassigned_count": count})
			return
		}
		s.assignCalls = append(s.assignCalls, guardrailID+"/"+strings.Join(body.MemberUserIDs, ","))
		if s.failAssign {
			writeError(http.StatusUnauthorized, "Missing Authentication header")
			return
		}
		for _, userID := range body.MemberUserIDs {
			if !s.members[*workspaceID][userID] {
				writeError(http.StatusBadRequest, "Some users are not members of the workspace")
				return
			}
		}
		for _, userID := range body.MemberUserIDs {
			s.upsert(stubMemberAssignmentRow{userID: userID, workspaceID: *workspaceID, guardrailID: guardrailID})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"assigned_count": len(body.MemberUserIDs)})
	default:
		writeError(http.StatusNotFound, "Not found")
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func (s *stubMemberAssignments) upsert(row stubMemberAssignmentRow) {
	for i := range s.rows {
		if s.rows[i].userID == row.userID && s.rows[i].workspaceID == row.workspaceID {
			s.rows[i].guardrailID = row.guardrailID
			return
		}
	}
	s.rows = append(s.rows, row)
}

func (s *stubMemberAssignments) assign(guardrailID, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsert(stubMemberAssignmentRow{userID: userID, workspaceID: *s.guardrails[guardrailID], guardrailID: guardrailID})
}

func (s *stubMemberAssignments) removeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = nil
}

func (s *stubMemberAssignments) deleteGuardrail(guardrailID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.guardrails, guardrailID)
	kept := s.rows[:0]
	for _, row := range s.rows {
		if row.guardrailID != guardrailID {
			kept = append(kept, row)
		}
	}
	s.rows = kept
}

func (s *stubMemberAssignments) setFailList(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failList = fail
}

func (s *stubMemberAssignments) checkRows(want ...stubMemberAssignmentRow) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.rows) != len(want) {
			return fmt.Errorf("assignments = %+v, want %+v", s.rows, want)
		}
		for i := range want {
			if s.rows[i] != want[i] {
				return fmt.Errorf("assignments = %+v, want %+v", s.rows, want)
			}
		}
		return nil
	}
}

func (s *stubMemberAssignments) checkAssignCalls(want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.assignCalls) != want {
			return fmt.Errorf("assign calls = %v, want %d", s.assignCalls, want)
		}
		return nil
	}
}

func (s *stubMemberAssignments) checkNoAssignCalls(*terraform.State) error {
	return s.checkAssignCalls(0)(nil)
}

func gmaConfig(serverURL, workspaceID, guardrailID, userID string) string {
	return stubBudgetProviderConfig(serverURL) + fmt.Sprintf(`
resource "openrouter_guardrail_member_assignment" "test" {
  workspace_id = %q
  guardrail_id = %q
  user_id      = %q
}
`, workspaceID, guardrailID, userID)
}

func gmaImportID(workspaceID, guardrailID, userID string) string {
	return workspaceID + "/" + guardrailID + "/" + userID
}

// UnitTest intentionally bypasses TF_ACC: every request goes to a local fixture.
func TestStubGuardrailMemberAssignmentLifecycle(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	config := gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkRows(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gmaResourceName, "id", gmaImportID(gmaWorkspaceID, gmaGuardrailID, gmaUserID)),
					resource.TestCheckResourceAttr(gmaResourceName, "workspace_id", gmaWorkspaceID),
					resource.TestCheckResourceAttr(gmaResourceName, "guardrail_id", gmaGuardrailID),
					resource.TestCheckResourceAttr(gmaResourceName, "user_id", gmaUserID),
					api.checkRows(stubMemberAssignmentRow{gmaUserID, gmaWorkspaceID, gmaGuardrailID}),
					api.checkAssignCalls(1),
				),
			},
			{Config: config, PlanOnly: true},
			{
				ResourceName:      gmaResourceName,
				ImportState:       true,
				ImportStateId:     gmaImportID(gmaWorkspaceID, gmaGuardrailID, gmaUserID),
				ImportStateVerify: true,
			},
			{
				Config: gmaConfig(srv.URL, gmaWorkspaceID, gmaSecondGuardrailID, gmaUserID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gmaResourceName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gmaResourceName, "guardrail_id", gmaSecondGuardrailID),
					api.checkRows(stubMemberAssignmentRow{gmaUserID, gmaWorkspaceID, gmaSecondGuardrailID}),
				),
			},
			{
				Config: gmaConfig(srv.URL, gmaWorkspaceID, gmaSecondGuardrailID, gmaSecondUserID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gmaResourceName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: api.checkRows(stubMemberAssignmentRow{gmaSecondUserID, gmaWorkspaceID, gmaSecondGuardrailID}),
			},
		},
	})
}

func TestStubGuardrailMemberAssignmentAlreadyPresent(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	api.assign(gmaGuardrailID, gmaUserID)
	config := gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					api.checkRows(stubMemberAssignmentRow{gmaUserID, gmaWorkspaceID, gmaGuardrailID}),
					api.checkAssignCalls(1),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestStubGuardrailMemberAssignmentImportAdoptsExisting(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	api.assign(gmaGuardrailID, gmaUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID),
				ResourceName:       gmaResourceName,
				ImportState:        true,
				ImportStateId:      gmaImportID(gmaWorkspaceID, gmaGuardrailID, gmaUserID),
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d states, want 1", len(states))
					}
					attrs := states[0].Attributes
					for key, want := range map[string]string{
						"id":           gmaImportID(gmaWorkspaceID, gmaGuardrailID, gmaUserID),
						"workspace_id": gmaWorkspaceID,
						"guardrail_id": gmaGuardrailID,
						"user_id":      gmaUserID,
					} {
						if attrs[key] != want {
							return fmt.Errorf("%s = %q, want %q", key, attrs[key], want)
						}
					}
					return nil
				},
			},
			{
				Config: gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: api.checkNoAssignCalls,
			},
		},
	})
}

func TestStubGuardrailMemberAssignmentImportRejectsInvalidOrMissing(t *testing.T) {
	srv, _ := newStubMemberAssignments(t)
	config := gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:        config,
				ResourceName:  gmaResourceName,
				ImportState:   true,
				ImportStateId: gmaGuardrailID + "/" + gmaUserID,
				ExpectError:   regexp.MustCompile(`<workspace_id>/<guardrail_id>/<user_id>`),
			},
			{
				Config:        config,
				ResourceName:  gmaResourceName,
				ImportState:   true,
				ImportStateId: gmaImportID(gmaWorkspaceID, gmaGuardrailID, gmaUserID),
				ExpectError:   regexp.MustCompile(`Cannot import non-existent remote object`),
			},
			{
				Config:        config,
				ResourceName:  gmaResourceName,
				ImportState:   true,
				ImportStateId: gmaImportID(gmaOtherWorkspaceID, gmaGuardrailID, gmaUserID),
				ExpectError:   regexp.MustCompile(`guardrail belongs to a different workspace`),
			},
		},
	})
}

func TestStubGuardrailMemberAssignmentRecreatesAfterOutOfBandRemoval(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	config := gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: api.removeAll,
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gmaResourceName, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					api.checkRows(stubMemberAssignmentRow{gmaUserID, gmaWorkspaceID, gmaGuardrailID}),
					api.checkAssignCalls(2),
				),
			},
		},
	})
}

// A member moved to another guardrail outside Terraform is moved back, and
// destroying the resource afterwards leaves the other guardrail's rows alone.
func TestStubGuardrailMemberAssignmentOutOfBandReassignment(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	config := gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkRows(stubMemberAssignmentRow{gmaSecondUserID, gmaWorkspaceID, gmaSecondGuardrailID}),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { api.assign(gmaSecondGuardrailID, gmaUserID) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gmaResourceName, plancheck.ResourceActionCreate),
					},
				},
				Check: api.checkRows(stubMemberAssignmentRow{gmaUserID, gmaWorkspaceID, gmaGuardrailID}),
			},
			{
				PreConfig: func() { api.assign(gmaSecondGuardrailID, gmaSecondUserID) },
				Config:    config,
				PlanOnly:  true,
			},
		},
	})
}

func TestStubGuardrailMemberAssignmentGuardrailDeleted(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	config := gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { api.deleteGuardrail(gmaGuardrailID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`guardrail not found`),
			},
			{
				Config:  stubBudgetProviderConfig(srv.URL),
				Destroy: true,
			},
		},
	})
}

func TestStubGuardrailMemberAssignmentMemberNotInWorkspace(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, "user_2synthNonMemberZZZZZZZZ"),
				ExpectError: regexp.MustCompile(`not members of the workspace`),
				Check:       api.checkRows(),
			},
		},
	})
}

func TestStubGuardrailMemberAssignmentRejectsWorkspaceMismatch(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkNoAssignCalls,
		Steps: []resource.TestStep{
			{
				Config:      gmaConfig(srv.URL, gmaWorkspaceID, gmaOtherGuardrailID, gmaUserID),
				ExpectError: regexp.MustCompile(`guardrail belongs to a different workspace`),
			},
			{
				Config:      gmaConfig(srv.URL, gmaWorkspaceID, gmaLegacyGuardrailID, gmaUserID),
				ExpectError: regexp.MustCompile(`guardrail has no workspace`),
			},
			{
				Config:      gmaConfig(srv.URL, gmaWorkspaceSlug, gmaGuardrailID, gmaUserID),
				ExpectError: regexp.MustCompile(`workspace_id must be a workspace ID`),
			},
			{
				Config:      gmaConfig(srv.URL, "5e4d3c2b-1a09-4f8e-8d7c-6b5a49382716", gmaGuardrailID, gmaUserID),
				ExpectError: regexp.MustCompile(`workspace not found`),
			},
			{
				Config:      gmaConfig(srv.URL, gmaWorkspaceID, "3d4e5f6a-7b8c-4d9e-8f0a-1b2c3d4e5f6a", gmaUserID),
				ExpectError: regexp.MustCompile(`guardrail not found`),
			},
		},
	})
}

func TestStubGuardrailMemberAssignmentRejectsDefaultGuardrail(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkNoAssignCalls,
		Steps: []resource.TestStep{
			{
				Config:      gmaConfig(srv.URL, gmaWorkspaceID, gmaDefaultGuardrailID, gmaUserID),
				ExpectError: regexp.MustCompile(`cannot assign the workspace default guardrail`),
			},
		},
	})
}

// Failures use a status the SDK does not retry, so the test does not wait out
// the 5XX backoff.
func TestStubGuardrailMemberAssignmentAPIFailure(t *testing.T) {
	srv, api := newStubMemberAssignments(t)
	config := gmaConfig(srv.URL, gmaWorkspaceID, gmaGuardrailID, gmaUserID)
	api.failAssign = true
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`API returned status 401: Missing Authentication header`),
			},
			{
				PreConfig: func() {
					api.mu.Lock()
					api.failAssign = false
					api.mu.Unlock()
				},
				Config: config,
			},
			{
				// A failed refresh surfaces the error and keeps the resource in state.
				PreConfig:   func() { api.setFailList(true) },
				Config:      config,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)failed to list member assignments.*Missing Authentication header`),
			},
			{
				PreConfig: func() { api.setFailList(false) },
				Config:    config,
				PlanOnly:  true,
			},
		},
	})
}
