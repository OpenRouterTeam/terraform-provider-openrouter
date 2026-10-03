package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	gkaWorkspaceID        = "4d1f0c2e-7a3b-4c5d-9e6f-0a1b2c3d4e5f"
	gkaOtherWorkspaceID   = "9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4"
	gkaDefaultGuardrailID = "0f1e2d3c-4b5a-5968-8776-a5b4c3d2e1f0"
	gkaGuardrailID        = "6a5b4c3d-2e1f-4a0b-9c8d-7e6f5a4b3c2d"
	gkaSecondGuardrailID  = "1b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e"
	gkaOtherGuardrailID   = "7c6b5a49-3827-4165-9f4e-3d2c1b0a9f8e"
	gkaLegacyGuardrailID  = "2c3d4e5f-6a7b-4c8d-9e0f-1a2b3c4d5e6f"
	gkaKeyHash            = "c56454edb818d6b14bc0d61c46025f1450b0f4012d12304ab40aacb519fcbc93"
	gkaSecondKeyHash      = "0a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9"
	gkaOtherWSKeyHash     = "f9e8d7c6b5a49382716051f4e3d2c1b0a9f8e7d6c5b4a39281706f5e4d3c2b1a"
	gkaUnknownKeyHash     = "1111111111111111111111111111111111111111111111111111111111111111"
	gkaResourceName       = "openrouter_guardrail_key_assignment.test"
)

// Mirrors the guardrail key assignment endpoints: a key holds at most one
// guardrail (assigning again moves the key), unknown hashes are skipped with
// a 200, removal only deletes rows of the named guardrail, a workspace
// guardrail only accepts keys from its workspace, and listing pages over all
// rows but drops rows whose key no longer exists.
type stubKeyAssignments struct {
	mu            sync.Mutex
	guardrails    map[string]*string
	keys          map[string]string
	rows          map[string]string // key hash -> guardrail ID, in insertion order below
	order         []string
	assignCalls   []string
	unassignCalls []string
	failList      bool
	failAssign    bool
}

func newStubKeyAssignments(t *testing.T) (*httptest.Server, *stubKeyAssignments) {
	t.Helper()
	ws, other := gkaWorkspaceID, gkaOtherWorkspaceID
	api := &stubKeyAssignments{
		guardrails: map[string]*string{
			gkaDefaultGuardrailID: &ws,
			gkaGuardrailID:        &ws,
			gkaSecondGuardrailID:  &ws,
			gkaOtherGuardrailID:   &other,
			gkaLegacyGuardrailID:  nil,
		},
		keys: map[string]string{
			gkaKeyHash:        gkaWorkspaceID,
			gkaSecondKeyHash:  gkaWorkspaceID,
			gkaOtherWSKeyHash: gkaOtherWorkspaceID,
		},
		rows: map[string]string{},
	}
	srv := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	t.Cleanup(srv.Close)
	return srv, api
}

func (s *stubKeyAssignments) serveHTTP(w http.ResponseWriter, r *http.Request) {
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
	if len(parts) < 4 || parts[0] != "guardrails" || parts[2] != "assignments" || parts[3] != "keys" {
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
	case len(parts) == 4 && r.Method == http.MethodGet:
		if s.failList {
			writeError(http.StatusUnauthorized, "Missing Authentication header")
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit <= 0 || limit > 100 {
			writeError(http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		var all []string
		for _, hash := range s.order {
			if s.rows[hash] == guardrailID {
				all = append(all, hash)
			}
		}
		total := len(all)
		page := all[min(offset, total):min(offset+limit, total)]
		data := []map[string]any{}
		for i, hash := range page {
			if _, ok := s.keys[hash]; !ok {
				continue
			}
			data = append(data, map[string]any{
				"id": fmt.Sprintf("a1b2c3d4-0000-4000-8000-%012d", offset+i), "key_hash": hash,
				"guardrail_id": guardrailID, "key_name": "Key " + hash[:6], "key_label": "sk-or-v1-" + hash[:3] + "...",
				"assigned_by": "user_2synthMemberAAAAAAAAAAA", "created_at": "2026-01-15T10:00:00Z",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "total_count": total})
	case len(parts) <= 5 && r.Method == http.MethodPost:
		remove := len(parts) == 5 && parts[4] == "remove"
		if len(parts) == 5 && !remove {
			writeError(http.StatusNotFound, "Not found")
			return
		}
		var body struct {
			KeyHashes []string `json:"key_hashes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.KeyHashes) == 0 {
			writeError(http.StatusBadRequest, "key_hashes is required")
			return
		}
		if guardrailID == gkaDefaultGuardrailID {
			writeError(http.StatusBadRequest, "Cannot assign keys to a default guardrail. Default guardrails apply to all unassigned keys automatically.")
			return
		}
		var found []string
		for _, hash := range body.KeyHashes {
			if _, ok := s.keys[hash]; ok {
				found = append(found, hash)
			}
		}
		if remove {
			s.unassignCalls = append(s.unassignCalls, guardrailID+"/"+strings.Join(body.KeyHashes, ","))
			count := 0
			for _, hash := range found {
				if s.rows[hash] == guardrailID {
					s.drop(hash)
					count++
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"unassigned_count": count})
			return
		}
		s.assignCalls = append(s.assignCalls, guardrailID+"/"+strings.Join(body.KeyHashes, ","))
		if s.failAssign {
			writeError(http.StatusUnauthorized, "Missing Authentication header")
			return
		}
		if workspaceID != nil {
			for _, hash := range found {
				if s.keys[hash] != *workspaceID {
					writeError(http.StatusBadRequest, "Some keys do not belong to the same workspace as the guardrail")
					return
				}
			}
		}
		for _, hash := range found {
			s.upsert(hash, guardrailID)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"assigned_count": len(found)})
	default:
		writeError(http.StatusNotFound, "Not found")
	}
}

func (s *stubKeyAssignments) upsert(hash, guardrailID string) {
	if _, ok := s.rows[hash]; !ok {
		s.order = append(s.order, hash)
	}
	s.rows[hash] = guardrailID
}

func (s *stubKeyAssignments) drop(hash string) {
	delete(s.rows, hash)
	kept := s.order[:0]
	for _, h := range s.order {
		if h != hash {
			kept = append(kept, h)
		}
	}
	s.order = kept
}

func (s *stubKeyAssignments) assign(guardrailID, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsert(hash, guardrailID)
}

func (s *stubKeyAssignments) removeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = map[string]string{}
	s.order = nil
}

func (s *stubKeyAssignments) deleteGuardrail(guardrailID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.guardrails, guardrailID)
	for hash, gr := range s.rows {
		if gr == guardrailID {
			s.drop(hash)
		}
	}
}

// deleteKey removes the key but, like the real table, leaves its assignment
// row behind for listing to skip.
func (s *stubKeyAssignments) deleteKey(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, hash)
}

func (s *stubKeyAssignments) setFailList(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failList = fail
}

func (s *stubKeyAssignments) checkRows(want map[string]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.rows) != len(want) {
			return fmt.Errorf("assignments = %v, want %v", s.rows, want)
		}
		for hash, gr := range want {
			if s.rows[hash] != gr {
				return fmt.Errorf("assignments = %v, want %v", s.rows, want)
			}
		}
		return nil
	}
}

func (s *stubKeyAssignments) checkAssignCalls(want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.assignCalls) != want {
			return fmt.Errorf("assign calls = %v, want %d", s.assignCalls, want)
		}
		return nil
	}
}

func gkaConfig(serverURL, guardrailID, keyHash string) string {
	return stubBudgetProviderConfig(serverURL) + fmt.Sprintf(`
resource "openrouter_guardrail_key_assignment" "test" {
  guardrail_id = %q
  key_hash     = %q
}
`, guardrailID, keyHash)
}

// UnitTest intentionally bypasses TF_ACC: every request goes to a local fixture.
func TestStubGuardrailKeyAssignmentLifecycle(t *testing.T) {
	srv, api := newStubKeyAssignments(t)
	config := gkaConfig(srv.URL, gkaGuardrailID, gkaKeyHash)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkRows(nil),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gkaResourceName, "id", gkaGuardrailID+"/"+gkaKeyHash),
					resource.TestCheckResourceAttr(gkaResourceName, "guardrail_id", gkaGuardrailID),
					resource.TestCheckResourceAttr(gkaResourceName, "key_hash", gkaKeyHash),
					api.checkRows(map[string]string{gkaKeyHash: gkaGuardrailID}),
					api.checkAssignCalls(1),
				),
			},
			{Config: config, PlanOnly: true},
			{
				ResourceName:      gkaResourceName,
				ImportState:       true,
				ImportStateId:     gkaGuardrailID + "/" + gkaKeyHash,
				ImportStateVerify: true,
			},
			{
				Config: gkaConfig(srv.URL, gkaSecondGuardrailID, gkaKeyHash),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gkaResourceName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: api.checkRows(map[string]string{gkaKeyHash: gkaSecondGuardrailID}),
			},
			{
				Config: gkaConfig(srv.URL, gkaSecondGuardrailID, gkaSecondKeyHash),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gkaResourceName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: api.checkRows(map[string]string{gkaSecondKeyHash: gkaSecondGuardrailID}),
			},
		},
	})
}

func TestStubGuardrailKeyAssignmentLegacyGuardrail(t *testing.T) {
	srv, api := newStubKeyAssignments(t)
	config := gkaConfig(srv.URL, gkaLegacyGuardrailID, gkaKeyHash)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkRows(nil),
		Steps: []resource.TestStep{
			{Config: config, Check: api.checkRows(map[string]string{gkaKeyHash: gkaLegacyGuardrailID})},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestStubGuardrailKeyAssignmentAlreadyPresentAndImport(t *testing.T) {
	srv, api := newStubKeyAssignments(t)
	api.assign(gkaGuardrailID, gkaKeyHash)
	config := gkaConfig(srv.URL, gkaGuardrailID, gkaKeyHash)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       gkaResourceName,
				ImportState:        true,
				ImportStateId:      gkaGuardrailID + "/" + gkaKeyHash,
				ImportStatePersist: true,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: api.checkAssignCalls(0),
			},
		},
	})
}

func TestStubGuardrailKeyAssignmentImportRejectsInvalidOrMissing(t *testing.T) {
	srv, _ := newStubKeyAssignments(t)
	config := gkaConfig(srv.URL, gkaGuardrailID, gkaKeyHash)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:        config,
				ResourceName:  gkaResourceName,
				ImportState:   true,
				ImportStateId: gkaKeyHash,
				ExpectError:   regexp.MustCompile(`<guardrail_id>/<key_hash>`),
			},
			{
				Config:        config,
				ResourceName:  gkaResourceName,
				ImportState:   true,
				ImportStateId: gkaGuardrailID + "/" + gkaKeyHash,
				ExpectError:   regexp.MustCompile(`Cannot import non-existent remote object`),
			},
		},
	})
}

// A key moved to another guardrail outside Terraform is moved back. After it
// is moved away again, destroying leaves the other guardrail's row alone.
func TestStubGuardrailKeyAssignmentOutOfBandChanges(t *testing.T) {
	srv, api := newStubKeyAssignments(t)
	config := gkaConfig(srv.URL, gkaGuardrailID, gkaKeyHash)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkRows(map[string]string{gkaKeyHash: gkaSecondGuardrailID}),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: api.removeAll,
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gkaResourceName, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					api.checkRows(map[string]string{gkaKeyHash: gkaGuardrailID}),
					api.checkAssignCalls(2),
				),
			},
			{
				PreConfig: func() { api.assign(gkaSecondGuardrailID, gkaKeyHash) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gkaResourceName, plancheck.ResourceActionCreate),
					},
				},
				Check: api.checkRows(map[string]string{gkaKeyHash: gkaGuardrailID}),
			},
			{
				PreConfig:          func() { api.assign(gkaSecondGuardrailID, gkaKeyHash) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestStubGuardrailKeyAssignmentGuardrailOrKeyDeleted(t *testing.T) {
	srv, api := newStubKeyAssignments(t)
	config := gkaConfig(srv.URL, gkaGuardrailID, gkaKeyHash)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { api.deleteKey(gkaKeyHash) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`API key not found`),
			},
			{
				Config: gkaConfig(srv.URL, gkaGuardrailID, gkaSecondKeyHash),
			},
			{
				PreConfig:          func() { api.deleteGuardrail(gkaGuardrailID) },
				Config:             gkaConfig(srv.URL, gkaGuardrailID, gkaSecondKeyHash),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config:      gkaConfig(srv.URL, gkaGuardrailID, gkaSecondKeyHash),
				ExpectError: regexp.MustCompile(`guardrail not found`),
			},
			{
				Config:  stubBudgetProviderConfig(srv.URL),
				Destroy: true,
			},
		},
	})
}

// The target row sits on the second page and the first page has rows whose
// key was deleted, so the API returns a short first page.
func TestStubGuardrailKeyAssignmentPagesPastDeletedKeys(t *testing.T) {
	srv, api := newStubKeyAssignments(t)
	for i := 0; i < 150; i++ {
		hash := fmt.Sprintf("%064x", i+1)
		api.keys[hash] = gkaWorkspaceID
		api.assign(gkaGuardrailID, hash)
		if i < 120 {
			api.deleteKey(hash)
		}
	}
	api.assign(gkaGuardrailID, gkaKeyHash)
	config := gkaConfig(srv.URL, gkaGuardrailID, gkaKeyHash)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       gkaResourceName,
				ImportState:        true,
				ImportStateId:      gkaGuardrailID + "/" + gkaKeyHash,
				ImportStatePersist: true,
			},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestStubGuardrailKeyAssignmentRejectedByAPI(t *testing.T) {
	srv, api := newStubKeyAssignments(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		CheckDestroy:             api.checkRows(nil),
		Steps: []resource.TestStep{
			{
				Config:      gkaConfig(srv.URL, gkaGuardrailID, gkaUnknownKeyHash),
				ExpectError: regexp.MustCompile(`API key not found`),
			},
			{
				Config:      gkaConfig(srv.URL, gkaDefaultGuardrailID, gkaKeyHash),
				ExpectError: regexp.MustCompile(`Cannot assign keys to a default guardrail`),
			},
			{
				Config:      gkaConfig(srv.URL, gkaGuardrailID, gkaOtherWSKeyHash),
				ExpectError: regexp.MustCompile(`do not belong to the same workspace`),
			},
			{
				Config:      gkaConfig(srv.URL, "3d4e5f6a-7b8c-4d9e-8f0a-1b2c3d4e5f6a", gkaKeyHash),
				ExpectError: regexp.MustCompile(`guardrail not found`),
			},
			{
				Config:      gkaConfig(srv.URL, gkaGuardrailID, "sk-or-v1-0123456789abcdef"),
				ExpectError: regexp.MustCompile(`secret API key given instead of its hash`),
			},
		},
	})
}

// Failures use a status the SDK does not retry, so the test does not wait out
// the 5XX backoff.
func TestStubGuardrailKeyAssignmentAPIFailure(t *testing.T) {
	srv, api := newStubKeyAssignments(t)
	config := gkaConfig(srv.URL, gkaGuardrailID, gkaKeyHash)
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
				ExpectError: regexp.MustCompile(`(?s)failed to list key assignments.*Missing Authentication header`),
			},
			{
				PreConfig: func() { api.setFailList(false) },
				Config:    config,
				PlanOnly:  true,
			},
		},
	})
}
