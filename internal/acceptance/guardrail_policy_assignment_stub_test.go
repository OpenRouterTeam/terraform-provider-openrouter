package acceptance

// Offline coverage for the two guardrail policy assignment resources
// (openrouter_guardrail_policy_api_key_assignment and
// openrouter_guardrail_policy_member_assignment).
//
// The live acceptance suite cannot exercise these routes yet: the policy
// backend is behind a flag on the test org, so every policy path answers 404
// there and a live test would prove nothing either way. This stub enforces the
// contract the provider has to satisfy and needs no credentials and no network,
// so it runs in the default offline gate.
//
// What it locks down, in the order the design gets it wrong:
//
//   - the write carries the state the provider read, whole. The route is a
//     compare-and-swap, so a PUT with an empty or guessed expected_assignment is
//     refused, and the three fields travel together.
//   - a conflict is resolved by reading again, not by interpreting the status.
//     The route answers 409 both for a stale expected_assignment and for a
//     removal of a policy the subject does not hold, so the two cannot be told
//     apart: a destroy whose pre-read finds itself not a member issues no write
//     at all, and a write refused once is retried after a fresh read rather than
//     reported as a failure.
//   - an inherited policy is not an assignment. The read returns the workspace
//     default policies the subject inherits alongside the ones it holds, and
//     only the second list decides drift. A provider that compared against both
//     would report a diff it cannot resolve, because this route cannot remove an
//     inherited policy.
//   - workspace_id is the scope on the read and travels in the write body. An
//     organization management key cannot read without it, and the API never
//     returns it, so the import identifier is the only channel that can supply
//     it.
//   - the subject kind picks the route, and the member routes carry the member
//     user ID.
//
// Run: go test ./internal/acceptance -run TestStubGuardrailPolicyAssignment -v

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

const (
	stubAssignmentPolicyID = "77777777-7777-7777-7777-777777777777"
	stubAssignmentOtherID  = "88888888-8888-8888-8888-888888888888"
	stubAssignmentKeyHash  = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	stubAssignmentMemberID = "user_00000000000000000000000000"
)

// guardrailPolicyAssignmentStub answers like the Management API's assignment
// routes: one state per subject, policy type and workspace scope, a read that
// returns that state or an empty one, and a write that compares the state it was
// handed with what is stored before changing anything.
type guardrailPolicyAssignmentStub struct {
	mu sync.Mutex
	// orgKey makes every read require a workspace scope and every write require
	// one in the body, the way the live routes behave for an organization
	// management key: without it the answer is 400, not a default.
	orgKey bool
	// ownWorkspace is the workspace a request naming none resolves to. The live
	// route applies a workspace-scoped key's own workspace by default, and that
	// fallback is what lets a bare import identifier work: the read that follows
	// it carries no workspace, and it still has to find the same state.
	ownWorkspace string
	// states is keyed by subject kind, subject id, policy type and scope.
	states map[string]map[string]any
	// subjects is the set the route knows. A subject outside it answers the 400
	// the live route answers for an API key that was deleted or a member who
	// left, which is how a test deletes a subject out of band.
	subjects map[string]bool
	// policies is the workspace's catalogue. A policy id outside it answers the
	// 404 the live route answers for a policy that is not in this workspace,
	// which is a different case from a subject that is gone and must not be
	// conflated with it.
	policies map[string]bool
	// inherited is what every subject is granted by the workspace default, so a
	// test can prove the inherited list is not read as an assignment.
	inherited []string
	optedOut  bool
	// rejectNextPut makes the next write answer 409 once, which is how a
	// concurrent writer is reproduced without a second driver.
	rejectNextPut bool
	getQueries    []url.Values
	puts          []map[string]any
}

func assignmentStubKey(segment, subject, policyType, workspaceID string) string {
	return strings.Join([]string{segment, subject, policyType, workspaceID}, "|")
}

func newGuardrailPolicyAssignmentStub(t *testing.T) (*httptest.Server, *guardrailPolicyAssignmentStub) {
	t.Helper()
	s := &guardrailPolicyAssignmentStub{
		states: map[string]map[string]any{},
		subjects: map[string]bool{
			stubAssignmentKeyHash:  true,
			stubAssignmentMemberID: true,
		},
		policies: map[string]bool{
			stubAssignmentPolicyID: true,
			stubAssignmentOtherID:  true,
		},
		ownWorkspace: stubWorkspaceID,
	}

	writeJSON := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}
	badRequest := func(w http.ResponseWriter, message string) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": 400, "message": message},
		})
	}
	// Both 409s carry the same shape, which is the whole difficulty: the status
	// code alone cannot say whether the caller's state was stale or the removal
	// was never an assignment.
	conflict := func(w http.ResponseWriter, message string) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]any{"code": 409, "message": message},
		})
	}

	mux := http.NewServeMux()

	register := func(segment string) {
		base := "/guardrails/policies/assignments/" + segment + "/{subject}"

		mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			s.getQueries = append(s.getQueries, r.URL.Query())
			orgKey := s.orgKey
			s.mu.Unlock()

			// Measured, and the reason this is not a 404: a deleted API key and
			// a member who is not in the workspace both answer 400 with a
			// message naming the subject. A malformed request answers the same
			// 400, which is what makes the provider's classification by message
			// the only signal the route gives it.
			if !s.knowsSubject(r.PathValue("subject")) {
				badRequest(w, subjectGoneMessage(segment))
				return
			}

			workspaceID := r.URL.Query().Get("workspace_id")
			if workspaceID == "" {
				if orgKey {
					// An organization management key has no workspace to fall
					// back to, so the route refuses rather than guessing.
					badRequest(w, "Invalid guardrail policy request")
					return
				}
				workspaceID = s.defaultWorkspace()
			}

			key := assignmentStubKey(segment, r.PathValue("subject"), r.URL.Query().Get("policy_type"), workspaceID)
			writeJSON(w, http.StatusOK, map[string]any{"data": s.readState(key)})
		})

		mux.HandleFunc("PUT "+base, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				badRequest(w, "Invalid guardrail policy request")
				return
			}

			s.mu.Lock()
			s.puts = append(s.puts, body)
			reject := s.rejectNextPut
			s.rejectNextPut = false
			s.mu.Unlock()

			requestedWorkspace, _ := body["workspace_id"].(string)
			if requestedWorkspace == "" {
				if s.isOrgKey() {
					badRequest(w, "Invalid guardrail policy request")
					return
				}
				requestedWorkspace = s.defaultWorkspace()
			}
			if reject {
				conflict(w, "Assignments changed; review the current assignments before trying again")
				return
			}

			// The subject check comes first because that is the order the live
			// route answers in: a request naming a key that does not exist and a
			// policy that does not exist answers the key's 400, not the policy's
			// 404.
			if !s.knowsSubject(r.PathValue("subject")) {
				badRequest(w, subjectGoneMessage(segment))
				return
			}

			policyType, _ := body["policy_type"].(string)
			policyID, _ := body["policy_id"].(string)
			assigned, _ := body["assigned"].(bool)

			if !s.knowsPolicy(policyID) {
				// Measured: 404 with this message, for a policy that is not in
				// the workspace. It is not the subject-missing answer, which is
				// the 400 above.
				writeJSON(w, http.StatusNotFound, map[string]any{
					"error": map[string]any{"code": 404, "message": "Policy not found in this workspace"},
				})
				return
			}

			key := assignmentStubKey(segment, r.PathValue("subject"), policyType, requestedWorkspace)

			stored := s.readState(key)
			expected, _ := body["expected_assignment"].(map[string]any)
			if !sameAssignmentState(expected, stored) {
				conflict(w, "Assignments changed; review the current assignments before trying again")
				return
			}

			held := containsString(stringList(stored["policy_ids"]), policyID)
			if !assigned && !held {
				// The live route answers this rather than treating the removal
				// as a no-op: a second unassign is a 409, not a 200.
				conflict(w, "Policy is not assigned to this subject")
				return
			}
			if assigned == held {
				writeJSON(w, http.StatusOK, map[string]any{"data": false})
				return
			}

			// Measured: the route sets the assignment for a policy type rather
			// than adding to it. Assigning a second policy of one type to a
			// subject that already held one leaves only the second, so an
			// additive model here would let a configuration look convergent
			// against a route that cannot store what it asks for.
			ids := []string{}
			if assigned {
				ids = []string{policyID}
			}
			s.writeHeld(key, ids)
			writeJSON(w, http.StatusOK, map[string]any{"data": true})
		})
	}

	register("api-keys")
	register("members")

	return httptest.NewServer(mux), s
}

// subjectGoneMessage is the message the live route answers for a subject that no
// longer exists. Measured: the two segments word it differently, which is why the
// message rather than the status code is what the provider classifies on.
func subjectGoneMessage(segment string) string {
	if segment == "api-keys" {
		return "One or more API keys were not found"
	}
	return "The policy assignment subject is unavailable in this workspace"
}

func (s *guardrailPolicyAssignmentStub) knowsSubject(subject string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subjects[subject]
}

func (s *guardrailPolicyAssignmentStub) knowsPolicy(policyID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policies[policyID]
}

// forgetSubject deletes a subject from the route's world, which is how a test
// reproduces an API key that was deleted or a member who left the workspace.
func (s *guardrailPolicyAssignmentStub) forgetSubject(subject string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subjects, subject)
}

func (s *guardrailPolicyAssignmentStub) defaultWorkspace() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ownWorkspace
}

func (s *guardrailPolicyAssignmentStub) isOrgKey() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.orgKey
}

// readState returns the stored state for one key, defaulting to the inherited
// policies with nothing held, which is what the live route answers for a subject
// with no assignment.
func (s *guardrailPolicyAssignmentStub) readState(key string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[key]
	if !ok {
		st = map[string]any{
			"policy_ids":                   []string{},
			"workspace_default_policy_ids": append([]string{}, s.inherited...),
			"opted_out":                    s.optedOut,
		}
		s.states[key] = st
	}
	return st
}

func (s *guardrailPolicyAssignmentStub) writeHeld(key string, ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[key]
	if !ok {
		st = map[string]any{
			"policy_ids":                   []string{},
			"workspace_default_policy_ids": append([]string{}, s.inherited...),
			"opted_out":                    s.optedOut,
		}
		s.states[key] = st
	}
	st["policy_ids"] = ids
}

// hold sets the directly assigned policies for one subject and type, which is
// how a test removes an assignment out of band.
func (s *guardrailPolicyAssignmentStub) hold(segment, subject, policyType, workspaceID string, ids []string) {
	s.writeHeld(assignmentStubKey(segment, subject, policyType, workspaceID), ids)
}

func (s *guardrailPolicyAssignmentStub) inherit(policyIDs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inherited = policyIDs
}

func (s *guardrailPolicyAssignmentStub) rejectNextWrite() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejectNextPut = true
}

func (s *guardrailPolicyAssignmentStub) putBodies() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.puts...)
}

func (s *guardrailPolicyAssignmentStub) reads() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.getQueries...)
}

func sameAssignmentState(expected map[string]any, stored map[string]any) bool {
	if expected == nil {
		return false
	}
	if optedOut, ok := expected["opted_out"].(bool); !ok || optedOut != boolValue(stored["opted_out"]) {
		return false
	}
	return reflect.DeepEqual(stringList(expected["policy_ids"]), stringList(stored["policy_ids"])) &&
		reflect.DeepEqual(stringList(expected["workspace_default_policy_ids"]), stringList(stored["workspace_default_policy_ids"]))
}

func stringList(value any) []string {
	out := []string{}
	switch list := value.(type) {
	case []string:
		out = append(out, list...)
	case []any:
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

func boolValue(value any) bool {
	b, _ := value.(bool)
	return b
}

func assignmentWrites(stub *guardrailPolicyAssignmentStub) (assigned, removed []map[string]any) {
	for _, body := range stub.putBodies() {
		if isAssigned, _ := body["assigned"].(bool); isAssigned {
			assigned = append(assigned, body)
		} else {
			removed = append(removed, body)
		}
	}
	return assigned, removed
}

func apiKeyAssignmentConfig(serverURL, attrs string) string {
	return stubProviderBlock(serverURL) + fmt.Sprintf(`
resource "openrouter_guardrail_policy_api_key_assignment" "test" {
%s}
`, attrs)
}

func memberAssignmentConfig(serverURL, attrs string) string {
	return stubProviderBlock(serverURL) + fmt.Sprintf(`
resource "openrouter_guardrail_policy_member_assignment" "test" {
%s}
`, attrs)
}

const assignmentTestResource = "openrouter_guardrail_policy_api_key_assignment.test"

// TestStubGuardrailPolicyAssignmentSendsTheStateItRead is the regression test
// for the compare-and-swap: the write has to carry the state the provider read,
// and that state is three fields, not the policy ids alone.
func TestStubGuardrailPolicyAssignmentSendsTheStateItRead(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)
	stub.inherit(stubAssignmentOtherID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash     = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentKeyHash, stubAssignmentPolicyID, stubWorkspaceID)),
				Check: resource.TestCheckResourceAttrSet(assignmentTestResource, "id"),
			},
		},
	})

	assigned, _ := assignmentWrites(stub)
	if len(assigned) != 1 {
		t.Fatalf("want exactly one assigning write, got %d", len(assigned))
	}
	body := assigned[0]
	if got := body["assigned"]; got != true {
		t.Errorf("write sent assigned=%v, want true", got)
	}
	if got := body["policy_id"]; got != stubAssignmentPolicyID {
		t.Errorf("write sent policy_id=%v, want %q", got, stubAssignmentPolicyID)
	}
	if got := body["policy_type"]; got != "moderation" {
		t.Errorf("write sent policy_type=%v, want %q", got, "moderation")
	}
	if got, _ := body["workspace_id"].(string); got != stubWorkspaceID {
		t.Errorf("write sent workspace_id=%q, want %q", got, stubWorkspaceID)
	}
	expected, ok := body["expected_assignment"].(map[string]any)
	if !ok {
		t.Fatalf("write carried no expected_assignment: %v", body)
	}
	if got, ok := expected["opted_out"].(bool); !ok || got {
		t.Errorf("write sent expected_assignment.opted_out=%v, want false", expected["opted_out"])
	}
	if ids := stringList(expected["policy_ids"]); len(ids) != 0 {
		t.Errorf("write sent expected_assignment.policy_ids=%v, want empty at create", ids)
	}
	// The inherited default is part of the state the provider read, so it has to
	// travel: the route compares the whole state and refuses a partial one.
	if ids := stringList(expected["workspace_default_policy_ids"]); !reflect.DeepEqual(ids, []string{stubAssignmentOtherID}) {
		t.Errorf("write sent expected_assignment.workspace_default_policy_ids=%v, want [%s]", ids, stubAssignmentOtherID)
	}
}

// TestStubGuardrailPolicyAssignmentInheritedPolicyIsNotAnAssignment covers the
// distinction the read has to make.
//
// The subject inherits, from the workspace default, the very policy this resource
// assigns directly. A provider that counted the inherited list as an assignment
// would find the state already satisfied at create, issue no write, and record an
// assignment the subject does not hold. The inherited policy is not the same
// thing, and the route cannot remove it either.
func TestStubGuardrailPolicyAssignmentInheritedPolicyIsNotAnAssignment(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)
	stub.inherit(stubAssignmentPolicyID)

	cfg := apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash     = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentKeyHash, stubAssignmentPolicyID, stubWorkspaceID))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			// The same configuration again: an empty plan is also an assertion,
			// because the inherited policy must not read as drift either.
			{Config: cfg},
		},
	})

	if assigned, _ := assignmentWrites(stub); len(assigned) != 1 {
		t.Errorf("want one assigning write, got %d: an inherited policy is not an assignment, so create still has to write", len(assigned))
	}
}

// TestStubGuardrailPolicyAssignmentDestroyOfANonMemberIssuesNoWrite covers the
// wiring of a destroy whose assignment is already gone: it must complete and
// write nothing.
//
// Terraform refreshes before it destroys, so this exercises the read that drops
// the resource from state and then no delete at all, rather than the delete's own
// pre-read. That pre-read is pinned by the reconcile unit tests, which drive the
// two calls directly; this test is what shows the whole path holds together.
func TestStubGuardrailPolicyAssignmentDestroyOfANonMemberIssuesNoWrite(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)
	cfg := apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash     = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentKeyHash, stubAssignmentPolicyID, stubWorkspaceID))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				// The assignment is removed out of band, so the destroy's
				// pre-read finds the subject holding nothing.
				PreConfig: func() {
					stub.hold("api-keys", stubAssignmentKeyHash, "moderation", stubWorkspaceID, nil)
				},
				Config: stubProviderBlock(srv.URL),
			},
		},
	})

	assigned, removed := assignmentWrites(stub)
	if len(assigned) != 1 {
		t.Errorf("want one assigning write (the create), got %d", len(assigned))
	}
	// The assertion this test exists for: the closing destroy is the only
	// removal in play, and it must not have written, because its pre-read found
	// the subject holding nothing.
	if len(removed) != 0 {
		t.Errorf("want no removing write, got %d: a destroy whose pre-read found no assignment must not write", len(removed))
	}
}

// TestStubGuardrailPolicyAssignmentRetriesOnceAfterAConflict covers the other
// half of the same problem: a write refused with 409 is retried after a fresh
// read, not surfaced as a failure and not treated as success.
func TestStubGuardrailPolicyAssignmentRetriesOnceAfterAConflict(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)
	stub.rejectNextWrite()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash     = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentKeyHash, stubAssignmentPolicyID, stubWorkspaceID)),
				Check: resource.TestCheckResourceAttrSet(assignmentTestResource, "id"),
			},
		},
	})

	assigned, _ := assignmentWrites(stub)
	if len(assigned) != 2 {
		t.Fatalf("want two assigning writes (the refused one and the retry), got %d", len(assigned))
	}
	// The retry has to carry a freshly read state rather than the one the refused
	// attempt used, which is what makes it a compare-and-swap and not a blind
	// retry.
	for i, body := range assigned {
		if _, ok := body["expected_assignment"].(map[string]any); !ok {
			t.Errorf("write %d carried no expected_assignment: %v", i, body)
		}
	}
}

// TestStubGuardrailPolicyAssignmentDriftLeavesState covers an assignment removed
// out of band: read must drop it from state so the next plan re-creates it.
func TestStubGuardrailPolicyAssignmentDriftLeavesState(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)
	cfg := apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash     = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentKeyHash, stubAssignmentPolicyID, stubWorkspaceID))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					stub.hold("api-keys", stubAssignmentKeyHash, "moderation", stubWorkspaceID, nil)
				},
				// No Config here on purpose: RefreshState works against the
				// config from the previous step, and a step cannot carry both.
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{
					PostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(assignmentTestResource, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

// TestStubGuardrailPolicyAssignmentImportForms covers both import identifiers.
// The four-part form carries the workspace scope an organization management key
// needs; the three-part form relies on the key's own workspace.
func TestStubGuardrailPolicyAssignmentImportForms(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, _ := newGuardrailPolicyAssignmentStub(t)
	cfg := apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash     = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentKeyHash, stubAssignmentPolicyID, stubWorkspaceID))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				ResourceName:      assignmentTestResource,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     stubWorkspaceID + "/" + stubAssignmentKeyHash + "/moderation/" + stubAssignmentPolicyID,
			},
			{
				ResourceName:      assignmentTestResource,
				ImportState:       true,
				ImportStateVerify: true,
				// The bare form has no workspace to recover and no response
				// model carries one, so the attribute is absent afterwards. The
				// identifier itself is the same in both forms, which is why it
				// is not ignored: the workspace is an attribute, not part of
				// the assignment's identity.
				ImportStateVerifyIgnore: []string{"workspace_id"},
				ImportStateId:           stubAssignmentKeyHash + "/moderation/" + stubAssignmentPolicyID,
			},
		},
	})
}

// TestStubGuardrailPolicyAssignmentReplacesOnPolicyChange covers the replacement:
// every configurable attribute is identity, so a change is a removal followed by
// an assignment rather than an in-place edit the route has no operation for.
func TestStubGuardrailPolicyAssignmentReplacesOnPolicyChange(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)
	cfg := func(policyID string) string {
		return apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash     = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentKeyHash, policyID, stubWorkspaceID))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg(stubAssignmentPolicyID)},
			{
				Config: cfg(stubAssignmentOtherID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(assignmentTestResource, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})

	puts := stub.putBodies()
	// The apply under test ends with the create of the new policy; the test
	// framework's closing destroy of it follows and is not asserted here.
	last := 0
	for i, body := range puts {
		if isAssigned, _ := body["assigned"].(bool); isAssigned {
			last = i
		}
	}
	got := puts[:last+1]
	if len(got) != 3 {
		t.Fatalf("want three writes (assign, remove, assign), got %d", len(got))
	}
	want := []struct {
		assigned bool
		policyID string
	}{
		{true, stubAssignmentPolicyID},
		{false, stubAssignmentPolicyID},
		{true, stubAssignmentOtherID},
	}
	for i, expected := range want {
		isAssigned, _ := got[i]["assigned"].(bool)
		if isAssigned != expected.assigned {
			t.Errorf("write %d sent assigned=%v, want %v", i, isAssigned, expected.assigned)
		}
		if id, _ := got[i]["policy_id"].(string); id != expected.policyID {
			t.Errorf("write %d sent policy_id=%q, want %q", i, id, expected.policyID)
		}
	}
}

// TestStubGuardrailPolicyAssignmentMemberRoutes covers the member resource,
// which reaches the same state through the member routes and the same body.
func TestStubGuardrailPolicyAssignmentMemberRoutes(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: memberAssignmentConfig(srv.URL, fmt.Sprintf(`
  member_id    = %q
  policy_type  = "budget"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentMemberID, stubAssignmentPolicyID, stubWorkspaceID)),
				Check: resource.TestCheckResourceAttr("openrouter_guardrail_policy_member_assignment.test", "member_id", stubAssignmentMemberID),
			},
		},
	})

	assigned, _ := assignmentWrites(stub)
	if len(assigned) != 1 {
		t.Fatalf("want one assigning write, got %d", len(assigned))
	}
	if got := assigned[0]["policy_type"]; got != "budget" {
		t.Errorf("write sent policy_type=%v, want %q", got, "budget")
	}
	for i, q := range stub.reads() {
		if got := q.Get("workspace_id"); got != stubWorkspaceID {
			t.Errorf("read %d sent workspace_id=%q, want %q", i, got, stubWorkspaceID)
		}
		if got := q.Get("policy_type"); got != "budget" {
			t.Errorf("read %d sent policy_type=%q, want %q", i, got, "budget")
		}
	}
}

// TestStubGuardrailPolicyAssignmentOrgKeyNeedsAScope covers the scope
// requirement: an organization management key that reads without a workspace
// gets 400, so a resource that dropped the query parameter would fail rather
// than silently read the wrong workspace.
func TestStubGuardrailPolicyAssignmentOrgKeyNeedsAScope(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)
	stub.orgKey = true

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash    = %q
  policy_type = "moderation"
  policy_id   = %q
`, stubAssignmentKeyHash, stubAssignmentPolicyID)),
				ExpectError: regexp.MustCompile(`400`),
			},
		},
	})
}

// TestStubGuardrailPolicyAssignmentDeletedSubjectIsNotABlockedApply covers the
// classification the route forces on the provider. A deleted API key answers 400,
// not 404, with a message naming the subject; a malformed request answers the same
// 400. Reading the status alone would either fail the refresh forever, leaving the
// user a resource they cannot even destroy, or drop a resource from state on a
// misconfiguration. The subject message is read as drift and the destroy completes
// without a write.
func TestStubGuardrailPolicyAssignmentDeletedSubjectIsNotABlockedApply(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	srv, stub := newGuardrailPolicyAssignmentStub(t)
	cfg := apiKeyAssignmentConfig(srv.URL, fmt.Sprintf(`
  key_hash     = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentKeyHash, stubAssignmentPolicyID, stubWorkspaceID))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				// The API key is deleted out of band, so every read of this
				// assignment now answers the subject 400.
				PreConfig: func() { stub.forgetSubject(stubAssignmentKeyHash) },
				Config:    stubProviderBlock(srv.URL),
			},
		},
	})

	if _, removed := assignmentWrites(stub); len(removed) != 0 {
		t.Errorf("want no removing write, got %d: a subject that is gone leaves nothing to remove", len(removed))
	}
}

// TestStubGuardrailPolicyAssignmentPolicyNotInTheWorkspace covers the other half
// of the same taxonomy. A policy that is not in the workspace answers 404, which is
// about the policy and not about the subject, so a create has to fail with a
// message naming the policy rather than reporting a missing subject or a scope
// problem.
func TestStubGuardrailPolicyAssignmentPolicyNotInTheWorkspace(t *testing.T) {
	t.Setenv("TF_ACC", "1")

	const unknownPolicy = "99999999-9999-9999-9999-999999999999"

	srv, stub := newGuardrailPolicyAssignmentStub(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: memberAssignmentConfig(srv.URL, fmt.Sprintf(`
  member_id    = %q
  policy_type  = "moderation"
  policy_id    = %q
  workspace_id = %q
`, stubAssignmentMemberID, unknownPolicy, stubWorkspaceID)),
				ExpectError: regexp.MustCompile(`policy not found in this workspace`),
			},
		},
	})

	assigned, _ := assignmentWrites(stub)
	if len(assigned) != 1 {
		t.Fatalf("want the one assigning write that the route refused, got %d", len(assigned))
	}
	if got := assigned[0]["policy_id"]; got != unknownPolicy {
		t.Errorf("the refused write named policy_id=%v, want %q", got, unknownPolicy)
	}
}
