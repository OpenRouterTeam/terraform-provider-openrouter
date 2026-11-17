package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/shared"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

func TestParseGuardrailPolicyAssignmentID(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		wantWorkspace string
		want          guardrailPolicyAssignment
		wantErr       bool
	}{
		{
			name: "subject, type and policy, which is the workspace-scoped form",
			id:   "hash-key/moderation/11111111-1111-1111-1111-111111111111",
			want: guardrailPolicyAssignment{
				SubjectID:  "hash-key",
				PolicyType: "moderation",
				PolicyID:   "11111111-1111-1111-1111-111111111111",
			},
		},
		{
			name:          "workspace first, which is the form an organization key needs",
			id:            "22222222-2222-2222-2222-222222222222/hash-key/moderation/11111111-1111-1111-1111-111111111111",
			wantWorkspace: "22222222-2222-2222-2222-222222222222",
			want: guardrailPolicyAssignment{
				SubjectID:  "hash-key",
				PolicyType: "moderation",
				PolicyID:   "11111111-1111-1111-1111-111111111111",
			},
		},
		{
			name:    "every accepted policy type",
			id:      "hash-key/rate_limit/11111111-1111-1111-1111-111111111111",
			want:    guardrailPolicyAssignment{SubjectID: "hash-key", PolicyType: "rate_limit", PolicyID: "11111111-1111-1111-1111-111111111111"},
			wantErr: false,
		},
		{
			name:    "too few parts",
			id:      "hash-key/moderation",
			wantErr: true,
		},
		{
			name:    "too many parts",
			id:      "a/b/c/d/e",
			wantErr: true,
		},
		{
			name:    "an empty part",
			id:      "hash-key//11111111-1111-1111-1111-111111111111",
			wantErr: true,
		},
		{
			name:    "empty",
			id:      "",
			wantErr: true,
		},
		{
			name:    "a policy type the route does not accept",
			id:      "hash-key/ratelimit/11111111-1111-1111-1111-111111111111",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceID, got, err := parseGuardrailPolicyAssignmentID(tt.id)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseGuardrailPolicyAssignmentID(%q) returned no error, want one", tt.id)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGuardrailPolicyAssignmentID(%q) returned %v", tt.id, err)
			}
			if workspaceID != tt.wantWorkspace {
				t.Errorf("workspaceID = %q, want %q", workspaceID, tt.wantWorkspace)
			}
			if got != tt.want {
				t.Errorf("assignment = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestGuardrailPolicyAssignmentIDIsTheTupleAndIsImportable covers the property
// that makes the stored id useful: whatever the resource writes into state has
// to be accepted by import, and it has to be the same string whichever import
// form produced the resource.
func TestGuardrailPolicyAssignmentIDIsTheTupleAndIsImportable(t *testing.T) {
	want := guardrailPolicyAssignment{
		SubjectID:  "hash-key",
		PolicyType: "model_availability",
		PolicyID:   "11111111-1111-1111-1111-111111111111",
	}

	id := guardrailPolicyAssignmentID(want)
	if id != "hash-key/model_availability/11111111-1111-1111-1111-111111111111" {
		t.Fatalf("guardrailPolicyAssignmentID = %q, want the subject, the type and the policy", id)
	}

	workspaceID, got, err := parseGuardrailPolicyAssignmentID(id)
	if err != nil {
		t.Fatalf("the id the resource stores (%q) is not importable: %v", id, err)
	}
	if workspaceID != "" {
		t.Errorf("workspaceID = %q, want empty: the workspace is an attribute, not part of the identifier", workspaceID)
	}
	if got != want {
		t.Errorf("assignment = %+v, want %+v", got, want)
	}
}

// TestGuardrailPolicyAssignmentScopedImportIsTheSameAssignment covers the other
// import form: the workspace prefix supplies the scope a read needs without
// changing which assignment the identifier names.
func TestGuardrailPolicyAssignmentScopedImportIsTheSameAssignment(t *testing.T) {
	const (
		workspaceID = "22222222-2222-2222-2222-222222222222"
		policyID    = "11111111-1111-1111-1111-111111111111"
	)

	bare := "hash-key/model_availability/" + policyID

	gotWorkspaceID, got, err := parseGuardrailPolicyAssignmentID(workspaceID + "/" + bare)
	if err != nil {
		t.Fatalf("parseGuardrailPolicyAssignmentID returned %v", err)
	}
	if gotWorkspaceID != workspaceID {
		t.Errorf("workspaceID = %q, want %q", gotWorkspaceID, workspaceID)
	}
	if guardrailPolicyAssignmentID(got) != bare {
		t.Errorf("the scoped form names %q, want the same assignment as %q", guardrailPolicyAssignmentID(got), bare)
	}
}

// fakeGuardrailPolicyAssignmentAPI answers the two calls from fixed scripts, so
// the read-modify-write can be tested for what it does rather than for how it
// happens to behave under Terraform. The acceptance stubs prove the wiring; this
// proves the state machine, including the cases Terraform's own refresh hides.
type fakeGuardrailPolicyAssignmentAPI struct {
	// reads is what each read answers, in order, with the last entry repeating.
	reads []guardrailPolicyAssignmentRead
	// conflicts is whether each write answers 409, in order, last repeating.
	conflicts []bool

	readCalls      int
	changeCalls    int
	changeAssigned []bool
	changeExpected []shared.GuardrailPolicyAssignmentState
}

func (f *fakeGuardrailPolicyAssignmentAPI) api() guardrailPolicyAssignmentAPI {
	return guardrailPolicyAssignmentAPI{
		subjectPath: path.Root("key_hash"),
		read: func(context.Context, guardrailPolicyAssignment, string) (guardrailPolicyAssignmentRead, diag.Diagnostics) {
			at := min(f.readCalls, len(f.reads)-1)
			f.readCalls++
			return f.reads[at], nil
		},
		change: func(_ context.Context, _ guardrailPolicyAssignment, _ string, assigned bool, expected shared.GuardrailPolicyAssignmentState) (bool, string, diag.Diagnostics) {
			at := min(f.changeCalls, len(f.conflicts)-1)
			conflict := f.conflicts[at]
			f.changeCalls++
			f.changeAssigned = append(f.changeAssigned, assigned)
			f.changeExpected = append(f.changeExpected, expected)
			return conflict, "Assignments changed; review the current assignments before trying again", nil
		},
	}
}

func assignmentState(policyIDs ...string) guardrailPolicyAssignmentRead {
	return guardrailPolicyAssignmentRead{
		State: &shared.GuardrailPolicyAssignmentState{PolicyIds: policyIDs, WorkspaceDefaultPolicyIds: []string{}},
	}
}

func testAssignment() guardrailPolicyAssignment {
	return guardrailPolicyAssignment{SubjectID: "hash", PolicyType: "moderation", PolicyID: "policy"}
}

// TestReconcileGuardrailPolicyAssignmentSkipsAWriteItDoesNotNeed is the property
// that makes a re-run of a partial apply safe: the route answers 409 to a removal
// that was never an assignment, so a write the state already satisfies has to be
// recognized from the read rather than attempted and interpreted.
func TestReconcileGuardrailPolicyAssignmentSkipsAWriteItDoesNotNeed(t *testing.T) {
	for _, tt := range []struct {
		name  string
		want  bool
		reads []guardrailPolicyAssignmentRead
	}{
		{name: "already assigned, assigning", want: true, reads: []guardrailPolicyAssignmentRead{assignmentState("policy")}},
		{name: "not assigned, removing", want: false, reads: []guardrailPolicyAssignmentRead{assignmentState()}},
		{name: "assigned with others, assigning", want: true, reads: []guardrailPolicyAssignmentRead{assignmentState("other", "policy")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeGuardrailPolicyAssignmentAPI{reads: tt.reads, conflicts: []bool{false}}

			diags := reconcileGuardrailPolicyAssignment(context.Background(), fake.api(), testAssignment(), "workspace", tt.want)
			if diags.HasError() {
				t.Fatalf("reconcile returned %v", diags)
			}
			if fake.readCalls != 1 {
				t.Errorf("read %d times, want 1", fake.readCalls)
			}
			if fake.changeCalls != 0 {
				t.Errorf("wrote %d times, want 0: the state already matched", fake.changeCalls)
			}
		})
	}
}

// TestReconcileGuardrailPolicyAssignmentCarriesTheStateItRead covers the
// compare-and-swap: the write has to carry the state the read produced, whole.
func TestReconcileGuardrailPolicyAssignmentCarriesTheStateItRead(t *testing.T) {
	read := guardrailPolicyAssignmentRead{
		State: &shared.GuardrailPolicyAssignmentState{
			PolicyIds:                 []string{"other"},
			WorkspaceDefaultPolicyIds: []string{"inherited"},
			OptedOut:                  true,
		},
	}
	fake := &fakeGuardrailPolicyAssignmentAPI{reads: []guardrailPolicyAssignmentRead{read}, conflicts: []bool{false}}

	if diags := reconcileGuardrailPolicyAssignment(context.Background(), fake.api(), testAssignment(), "workspace", true); diags.HasError() {
		t.Fatalf("reconcile returned %v", diags)
	}
	if fake.changeCalls != 1 {
		t.Fatalf("wrote %d times, want 1", fake.changeCalls)
	}
	if !fake.changeAssigned[0] {
		t.Errorf("write sent assigned=%v, want true", fake.changeAssigned[0])
	}
	got := fake.changeExpected[0]
	if got.OptedOut != true || len(got.PolicyIds) != 1 || len(got.WorkspaceDefaultPolicyIds) != 1 {
		t.Errorf("write carried %+v, want the whole state that was read", got)
	}
}

// TestReconcileGuardrailPolicyAssignmentRetriesOnceAfterAConflict covers the
// resolution of the conflict the route also answers an already-removed
// assignment with: read again and decide, do not interpret the status.
func TestReconcileGuardrailPolicyAssignmentRetriesOnceAfterAConflict(t *testing.T) {
	fake := &fakeGuardrailPolicyAssignmentAPI{
		reads:     []guardrailPolicyAssignmentRead{assignmentState(), assignmentState()},
		conflicts: []bool{true, false},
	}

	if diags := reconcileGuardrailPolicyAssignment(context.Background(), fake.api(), testAssignment(), "workspace", true); diags.HasError() {
		t.Fatalf("reconcile returned %v", diags)
	}
	if fake.readCalls != 2 {
		t.Errorf("read %d times, want 2: a conflict has to be resolved by reading again", fake.readCalls)
	}
	if fake.changeCalls != 2 {
		t.Errorf("wrote %d times, want 2", fake.changeCalls)
	}
}

// TestReconcileGuardrailPolicyAssignmentReportsASecondConflict covers the bound:
// a writer that keeps moving the state is reported rather than retried through,
// and the API's own explanation reaches the report.
func TestReconcileGuardrailPolicyAssignmentReportsASecondConflict(t *testing.T) {
	fake := &fakeGuardrailPolicyAssignmentAPI{
		reads:     []guardrailPolicyAssignmentRead{assignmentState(), assignmentState()},
		conflicts: []bool{true, true},
	}

	diags := reconcileGuardrailPolicyAssignment(context.Background(), fake.api(), testAssignment(), "workspace", true)
	if !diags.HasError() {
		t.Fatal("reconcile returned no error for a conflict that repeated")
	}
	if fake.changeCalls != maxGuardrailPolicyAssignmentAttempts {
		t.Errorf("wrote %d times, want %d: the attempt count is the bound", fake.changeCalls, maxGuardrailPolicyAssignmentAttempts)
	}
	if !strings.Contains(diags[0].Detail(), "Assignments changed") {
		t.Errorf("the report does not quote the API: %q", diags[0].Detail())
	}
}

// TestReconcileGuardrailPolicyAssignmentOnAMissingSubject covers a read that
// reports the subject as gone, which this route answers with a 400 naming the
// subject rather than with a 404: a read 404 is not an absence signal here and
// is reported as an error instead.
func TestReconcileGuardrailPolicyAssignmentOnAMissingSubject(t *testing.T) {
	t.Run("removing is complete", func(t *testing.T) {
		fake := &fakeGuardrailPolicyAssignmentAPI{reads: []guardrailPolicyAssignmentRead{{NotFound: true}}, conflicts: []bool{false}}

		if diags := reconcileGuardrailPolicyAssignment(context.Background(), fake.api(), testAssignment(), "workspace", false); diags.HasError() {
			t.Fatalf("reconcile returned %v, want no error: there is nothing left to remove", diags)
		}
		if fake.changeCalls != 0 {
			t.Errorf("wrote %d times, want 0", fake.changeCalls)
		}
	})

	t.Run("assigning is a failure naming the subject", func(t *testing.T) {
		fake := &fakeGuardrailPolicyAssignmentAPI{reads: []guardrailPolicyAssignmentRead{{NotFound: true}}, conflicts: []bool{false}}

		diags := reconcileGuardrailPolicyAssignment(context.Background(), fake.api(), testAssignment(), "workspace", true)
		if !diags.HasError() {
			t.Fatal("reconcile returned no error for a subject that does not exist")
		}
		if fake.changeCalls != 0 {
			t.Errorf("wrote %d times, want 0: there is nothing to assign to", fake.changeCalls)
		}
		if !strings.Contains(diags[0].Summary(), "subject not found") {
			t.Errorf("the report is not about the subject: %q", diags[0].Summary())
		}
	})
}
