package approval_test

// Unit test validasi konfigurasi workflow (pure — store tidak tersentuh saat
// input tidak valid). Lifecycle engine penuh diuji di integration test.

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/approval"
	"esaproperti/internal/domain"
)

// nilStore: Store yang tidak boleh tersentuh (panic bila dipanggil) — memastikan
// validasi menolak SEBELUM ada akses persistence.
type nilStore struct{ approval.Store }

func TestCreateWorkflow_Validation(t *testing.T) {
	svc := approval.NewService(nilStore{})
	ctx := context.Background()

	step := func(seq int, role string) approval.StepInput {
		return approval.StepInput{Seq: seq, ApproverRole: role}
	}

	cases := []struct {
		name string
		req  approval.CreateWorkflowRequest
		want error
	}{
		{"target_tak_dikenal", approval.CreateWorkflowRequest{TargetType: "gaji", Name: "X",
			Steps: []approval.StepInput{step(1, "owner")}}, approval.ErrTargetTypeInvalid},
		{"tanpa_nama", approval.CreateWorkflowRequest{TargetType: approval.TargetRAB,
			Steps: []approval.StepInput{step(1, "owner")}}, approval.ErrWorkflowNameRequired},
		{"tanpa_step", approval.CreateWorkflowRequest{TargetType: approval.TargetRAB, Name: "X"},
			approval.ErrStepsRequired},
		{"seq_tidak_berurutan", approval.CreateWorkflowRequest{TargetType: approval.TargetRAB, Name: "X",
			Steps: []approval.StepInput{step(1, "owner"), step(3, "owner")}}, approval.ErrStepSeqInvalid},
		{"step_tanpa_role", approval.CreateWorkflowRequest{TargetType: approval.TargetRAB, Name: "X",
			Steps: []approval.StepInput{step(1, "")}}, approval.ErrStepRoleRequired},
		{"quorum_negatif", approval.CreateWorkflowRequest{TargetType: approval.TargetRAB, Name: "X",
			Steps: []approval.StepInput{{Seq: 1, ApproverRole: "owner", Quorum: -1}}}, approval.ErrStepQuorumInvalid},
		{"min_amount_negatif", approval.CreateWorkflowRequest{TargetType: approval.TargetRAB, Name: "X",
			Steps: []approval.StepInput{{Seq: 1, ApproverRole: "owner", MinAmount: domain.MustParse("-1")}}},
			approval.ErrStepMinAmountNegative},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.CreateWorkflow(ctx, 1, tc.req); !errors.Is(err, tc.want) {
				t.Errorf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestRequestStatus_Terminal(t *testing.T) {
	terminal := []approval.RequestStatus{approval.StatusApproved, approval.StatusRejected, approval.StatusCancelled}
	active := []approval.RequestStatus{approval.StatusPending, approval.StatusInReview}
	for _, s := range terminal {
		if !s.Terminal() {
			t.Errorf("%s harus terminal", s)
		}
	}
	for _, s := range active {
		if s.Terminal() {
			t.Errorf("%s tidak boleh terminal", s)
		}
	}
}
