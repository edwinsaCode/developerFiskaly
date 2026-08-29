//go:build integration

package approval_test

// Increment 5 — integration Generic Approval Workflow (real MySQL).
// Membuktikan: konfigurasi workflow (satu aktif per target_type), lifecycle
// request pending→in_review→approved / rejected / cancelled, quorum multi-step,
// threshold min_amount (skip step / auto-approve), role guard, double-act guard,
// satu request aktif per target, append-only actions, dan GATE OPT-IN pada
// konsumen pertama (RAB): tanpa workflow → approve langsung; dengan workflow →
// wajib request APPROVED. Prasyarat: TEST_DB_DSN + DB termigrasi (≥ 000041).

import (
	"context"
	"errors"
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/approval"
	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
)

const apTenant uint64 = 9_900_007

func apConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func apCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"approval_actions", "approval_requests", "approval_steps", "approval_workflows",
		"budget_items", "budget_plans", "projects",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", apTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func apSetup(t *testing.T) (*gorm.DB, *approval.Service) {
	t.Helper()
	db := apConnect(t)
	apCleanup(t, db)
	t.Cleanup(func() { apCleanup(t, db) })
	return db, approval.NewService(approval.NewGORMRepository(db))
}

func mkWorkflow(t *testing.T, svc *approval.Service, target approval.TargetType, steps []approval.StepInput) *approval.Workflow {
	t.Helper()
	w, err := svc.CreateWorkflow(context.Background(), apTenant, approval.CreateWorkflowRequest{
		TargetType: target, Name: "WF " + string(target), Steps: steps,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	return w
}

func uptr(v uint64) *uint64 { return &v }

// ── Engine lifecycle ──────────────────────────────────────────────────────────

func TestIntegration_Approval_MultiStepQuorum(t *testing.T) {
	_, svc := apSetup(t)
	ctx := context.Background()

	// Step 1: accountant (quorum 2) → Step 2: owner.
	mkWorkflow(t, svc, approval.TargetDiscount, []approval.StepInput{
		{Seq: 1, Name: "Review Akunting", ApproverRole: "accountant", Quorum: 2},
		{Seq: 2, Name: "Persetujuan Owner", ApproverRole: "owner"},
	})

	req, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetDiscount, TargetID: 42,
		Amount: domain.FromInt(50_000_000), RequestedBy: uptr(100),
	})
	if err != nil {
		t.Fatalf("SubmitRequest: %v", err)
	}
	if req.Status != approval.StatusPending || req.CurrentSeq != 1 {
		t.Fatalf("status awal = %s seq %d, want pending seq 1", req.Status, req.CurrentSeq)
	}

	// Satu target satu request aktif.
	if _, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetDiscount, TargetID: 42,
	}); !errors.Is(err, approval.ErrActiveRequestExists) {
		t.Errorf("request kedua harus ErrActiveRequestExists, got %v", err)
	}

	// Role salah ditolak.
	if _, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 200, ActorRole: "owner", Decision: approval.DecisionApprove,
	}); !errors.Is(err, approval.ErrActorRoleNotAllowed) {
		t.Errorf("owner di step accountant harus ditolak, got %v", err)
	}

	// Approve pertama (accountant #1): quorum 2 → masih in_review di step 1.
	r1, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 201, ActorRole: "accountant", Decision: approval.DecisionApprove,
	})
	if err != nil {
		t.Fatalf("act 1: %v", err)
	}
	if r1.Status != approval.StatusInReview || r1.CurrentSeq != 1 {
		t.Errorf("setelah approve 1/2: %s seq %d, want in_review seq 1 (quorum)", r1.Status, r1.CurrentSeq)
	}

	// Actor sama tidak boleh memutus dua kali di step yang sama.
	if _, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 201, ActorRole: "accountant", Decision: approval.DecisionApprove,
	}); !errors.Is(err, approval.ErrActorAlreadyActed) {
		t.Errorf("double-act harus ditolak, got %v", err)
	}

	// Approve kedua (accountant #2): quorum terpenuhi → maju ke step 2.
	r2, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 202, ActorRole: "accountant", Decision: approval.DecisionApprove,
	})
	if err != nil {
		t.Fatalf("act 2: %v", err)
	}
	if r2.Status != approval.StatusInReview || r2.CurrentSeq != 2 {
		t.Errorf("setelah quorum step 1: %s seq %d, want in_review seq 2", r2.Status, r2.CurrentSeq)
	}

	// Owner menyetujui step terakhir → APPROVED.
	r3, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 300, ActorRole: "owner", Decision: approval.DecisionApprove,
	})
	if err != nil {
		t.Fatalf("act 3: %v", err)
	}
	if r3.Status != approval.StatusApproved {
		t.Errorf("status akhir = %s, want approved", r3.Status)
	}

	// Terminal: aksi lanjutan ditolak; audit trail lengkap 3 aksi.
	if _, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 301, ActorRole: "owner", Decision: approval.DecisionApprove,
	}); !errors.Is(err, approval.ErrRequestTerminal) {
		t.Errorf("aksi pada request terminal harus ditolak, got %v", err)
	}
	detail, _ := svc.GetRequest(ctx, apTenant, req.ID)
	if len(detail.Actions) != 3 {
		t.Errorf("audit actions = %d, want 3 (append-only)", len(detail.Actions))
	}

	// Setelah terminal, request BARU utk target sama boleh dibuat.
	if _, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetDiscount, TargetID: 42,
	}); err != nil {
		t.Errorf("request baru pasca-terminal harus boleh, got %v", err)
	}
}

func TestIntegration_Approval_RejectAndCancel(t *testing.T) {
	_, svc := apSetup(t)
	ctx := context.Background()
	mkWorkflow(t, svc, approval.TargetCost, []approval.StepInput{
		{Seq: 1, ApproverRole: "owner"},
	})

	// Reject → terminal.
	req, _ := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetCost, TargetID: 7, RequestedBy: uptr(100),
	})
	out, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 300, ActorRole: "owner",
		Decision: approval.DecisionReject, Comment: "budget tidak masuk akal",
	})
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if out.Status != approval.StatusRejected {
		t.Errorf("status = %s, want rejected", out.Status)
	}

	// Cancel: hanya pemohon.
	req2, _ := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetCost, TargetID: 8, RequestedBy: uptr(100),
	})
	if err := svc.Cancel(ctx, apTenant, req2.ID, 999); !errors.Is(err, approval.ErrOnlyRequesterCanCancel) {
		t.Errorf("cancel oleh bukan pemohon harus ditolak, got %v", err)
	}
	if err := svc.Cancel(ctx, apTenant, req2.ID, 100); err != nil {
		t.Errorf("cancel oleh pemohon: %v", err)
	}
}

// Threshold Approval Matrix: step ber-min_amount di-skip untuk nominal kecil.
func TestIntegration_Approval_ThresholdSkipsSteps(t *testing.T) {
	_, svc := apSetup(t)
	ctx := context.Background()
	mkWorkflow(t, svc, approval.TargetPayment, []approval.StepInput{
		{Seq: 1, ApproverRole: "accountant", MinAmount: domain.FromInt(100_000_000)},
		{Seq: 2, ApproverRole: "owner", MinAmount: domain.FromInt(1_000_000_000)},
	})

	// 50jt < semua threshold → auto-approved tanpa approver.
	small, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetPayment, TargetID: 1, Amount: domain.FromInt(50_000_000),
	})
	if err != nil {
		t.Fatalf("submit kecil: %v", err)
	}
	if small.Status != approval.StatusApproved {
		t.Errorf("nominal di bawah semua threshold harus auto-approved, got %s", small.Status)
	}

	// 500jt → hanya step accountant yang berlaku; setelah approve → APPROVED
	// (step owner ter-skip karena < 1M).
	mid, _ := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetPayment, TargetID: 2, Amount: domain.FromInt(500_000_000),
	})
	out, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: mid.ID, ActorID: 201, ActorRole: "accountant", Decision: approval.DecisionApprove,
	})
	if err != nil {
		t.Fatalf("act mid: %v", err)
	}
	if out.Status != approval.StatusApproved {
		t.Errorf("500jt: setelah accountant harus APPROVED (step owner ter-skip), got %s", out.Status)
	}
}

// ── Konsumen pertama: gate RAB (opt-in) ───────────────────────────────────────

func TestIntegration_Approval_RABGate_OptIn(t *testing.T) {
	db, apSvc := apSetup(t)
	ctx := context.Background()

	budgetSvc := budget.NewService(budget.NewGORMRepository(db), budget.NewGORMRealisasiProvider(db))
	budgetSvc.SetApprovalGate(&rabGate{svc: apSvc})

	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		apTenant, "Gated Project", "planning").Error; err != nil {
		t.Fatal(err)
	}
	var projectID uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)

	mkPlan := func(label string) *budget.BudgetPlan {
		plan, err := budgetSvc.CreatePlan(ctx, apTenant, budget.CreatePlanRequest{ProjectID: projectID, Label: label})
		if err != nil {
			t.Fatalf("CreatePlan: %v", err)
		}
		if _, err := budgetSvc.AddItem(ctx, apTenant, budget.AddItemRequest{
			PlanID: plan.ID, Category: budget.BudgetCategoryLand, BudgetedAmount: domain.FromInt(1_000_000_000),
		}); err != nil {
			t.Fatalf("AddItem: %v", err)
		}
		return plan
	}

	// TANPA workflow 'rab' terkonfigurasi → approve langsung (perilaku lama).
	p1 := mkPlan("RAB v1")
	if _, err := budgetSvc.ApprovePlan(ctx, apTenant, budget.ApprovePlanRequest{PlanID: p1.ID, ApprovedBy: "owner"}); err != nil {
		t.Fatalf("tanpa governance, approve harus lolos: %v", err)
	}

	// KONFIGURASI workflow 'rab' → plan berikutnya WAJIB request APPROVED.
	mkWorkflow(t, apSvc, approval.TargetRAB, []approval.StepInput{
		{Seq: 1, Name: "Persetujuan Owner", ApproverRole: "owner"},
	})
	p2 := mkPlan("RAB v2")
	if _, err := budgetSvc.ApprovePlan(ctx, apTenant, budget.ApprovePlanRequest{PlanID: p2.ID, ApprovedBy: "owner"}); !errors.Is(err, approval.ErrApprovalRequired) {
		t.Fatalf("dengan governance, approve tanpa request harus ErrApprovalRequired, got %v", err)
	}

	// Submit request → owner approve → ApprovePlan lolos.
	req, err := apSvc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetRAB, TargetID: p2.ID, RequestedBy: uptr(100),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := apSvc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 300, ActorRole: "owner", Decision: approval.DecisionApprove,
	}); err != nil {
		t.Fatalf("act: %v", err)
	}
	plan, err := budgetSvc.ApprovePlan(ctx, apTenant, budget.ApprovePlanRequest{PlanID: p2.ID, ApprovedBy: "owner"})
	if err != nil {
		t.Fatalf("approve setelah request approved: %v", err)
	}
	if plan.Status != budget.BudgetPlanStatusActive {
		t.Errorf("plan status = %s, want active", plan.Status)
	}
}

// rabGate: adapter test — sama dengan adapter produksi di budget.NewHandler.
type rabGate struct{ svc *approval.Service }

func (g *rabGate) RequireApproved(ctx context.Context, tenantID, planID uint64) error {
	return g.svc.RequireApproved(ctx, tenantID, approval.TargetRAB, planID)
}

// ── Increment 5.1 — Hardening ─────────────────────────────────────────────────

// captureSink merekam event yang dipublikasikan engine (H6).
type captureSink struct{ events []approval.Event }

func (c *captureSink) Publish(e approval.Event) { c.events = append(c.events, e) }

// H1/H3: request in-flight dievaluasi dari SNAPSHOT BEKU — mengubah governance
// (nonaktifkan workflow lama + buat revisi baru dgn role/quorum berbeda) TIDAK
// mengubah request yang sedang berjalan; request BARU memakai revisi baru.
func TestIntegration_Hardening_FrozenSnapshotEvaluation(t *testing.T) {
	_, svc := apSetup(t)
	ctx := context.Background()

	w1 := mkWorkflow(t, svc, approval.TargetDiscount, []approval.StepInput{
		{Seq: 1, ApproverRole: "owner"},
	})
	if w1.Revision != 1 {
		t.Fatalf("revisi workflow pertama = %d, want 1", w1.Revision)
	}

	// Submit di bawah revisi 1 (step: owner).
	req1, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetDiscount, TargetID: 10, RequestedBy: uptr(100),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if req1.WorkflowRevision == nil || *req1.WorkflowRevision != 1 {
		t.Errorf("request harus membekukan workflow_revision=1, got %v", req1.WorkflowRevision)
	}
	snap, err := approval.ParseWorkflowSnapshot(*req1.WorkflowSnapshotJS)
	if err != nil || len(snap.Steps) != 1 || snap.Steps[0].ApproverRole != "owner" {
		t.Fatalf("workflow snapshot beku salah: %+v (%v)", snap, err)
	}

	// GOVERNANCE BERUBAH: nonaktifkan revisi 1, buat revisi 2 (role accountant,
	// quorum 2) — histori & request berjalan tidak boleh terpengaruh (H1/H3).
	if err := svc.SetWorkflowActive(ctx, apTenant, w1.ID, false); err != nil {
		t.Fatal(err)
	}
	w2 := mkWorkflow(t, svc, approval.TargetDiscount, []approval.StepInput{
		{Seq: 1, ApproverRole: "accountant", Quorum: 2},
	})
	if w2.Revision != 2 {
		t.Errorf("workflow pengganti harus revision 2, got %d", w2.Revision)
	}

	// Request lama tetap dievaluasi dengan step BEKU (owner, quorum 1) → APPROVED.
	out, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req1.ID, ActorID: 300, ActorRole: "owner", Decision: approval.DecisionApprove,
	})
	if err != nil {
		t.Fatalf("act pada request beku: %v", err)
	}
	if out.Status != approval.StatusApproved {
		t.Errorf("request revisi-1 harus APPROVED via step beku owner, got %s", out.Status)
	}

	// Request BARU memakai revisi 2 (accountant, quorum 2): owner ditolak.
	req2, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetDiscount, TargetID: 11, RequestedBy: uptr(100),
	})
	if err != nil {
		t.Fatalf("submit revisi 2: %v", err)
	}
	if req2.WorkflowRevision == nil || *req2.WorkflowRevision != 2 {
		t.Errorf("request baru harus revisi 2, got %v", req2.WorkflowRevision)
	}
	if _, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req2.ID, ActorID: 300, ActorRole: "owner", Decision: approval.DecisionApprove,
	}); !errors.Is(err, approval.ErrActorRoleNotAllowed) {
		t.Errorf("revisi 2 butuh accountant; owner harus ditolak, got %v", err)
	}
}

// H2: target snapshot membekukan konteks dokumen saat submit (seam invalidation).
func TestIntegration_Hardening_TargetSnapshot(t *testing.T) {
	_, svc := apSetup(t)
	ctx := context.Background()
	mkWorkflow(t, svc, approval.TargetCost, []approval.StepInput{{Seq: 1, ApproverRole: "owner"}})

	req, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetCost, TargetID: 77,
		Amount: domain.FromInt(250_000_000), TargetVersion: 3, RequestedBy: uptr(100),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	snap, err := approval.ParseTargetSnapshot(*req.TargetSnapshotJS)
	if err != nil {
		t.Fatalf("parse target snapshot: %v", err)
	}
	if snap.TargetID != 77 || snap.TargetVersion != 3 || snap.Amount != "250000000" || snap.Currency != "IDR" {
		t.Errorf("target snapshot salah: %+v", snap)
	}
}

// H4: delegate = seam eksplisit (vocabulary siap, eksekusi belum).
func TestIntegration_Hardening_DelegateSeam(t *testing.T) {
	_, svc := apSetup(t)
	ctx := context.Background()
	mkWorkflow(t, svc, approval.TargetCost, []approval.StepInput{{Seq: 1, ApproverRole: "owner"}})
	req, _ := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetCost, TargetID: 5, RequestedBy: uptr(100),
	})
	if _, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req.ID, ActorID: 300, ActorRole: "owner", Decision: approval.DecisionDelegate,
	}); !errors.Is(err, approval.ErrDelegateNotImplemented) {
		t.Errorf("delegate harus ErrDelegateNotImplemented (seam), got %v", err)
	}
}

// H5 (verifikasi ulang idempotency — kontrak API) + audit cancel + H6 events.
func TestIntegration_Hardening_IdempotencyCancelAuditAndEvents(t *testing.T) {
	_, svc := apSetup(t)
	sink := &captureSink{}
	svc.SetEventSink(sink)
	ctx := context.Background()
	mkWorkflow(t, svc, approval.TargetPayment, []approval.StepInput{{Seq: 1, ApproverRole: "owner"}})

	// H5: submit ulang utk target sama = error eksplisit (409 di API).
	req, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetPayment, TargetID: 1, RequestedBy: uptr(100),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetPayment, TargetID: 1, RequestedBy: uptr(100),
	}); !errors.Is(err, approval.ErrActiveRequestExists) {
		t.Fatalf("submit ulang harus ErrActiveRequestExists, got %v", err)
	}

	// Cancel meninggalkan jejak audit action decision='cancel' (blind spot B).
	if err := svc.Cancel(ctx, apTenant, req.ID, 100); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	detail, _ := svc.GetRequest(ctx, apTenant, req.ID)
	if len(detail.Actions) != 1 || detail.Actions[0].Decision != approval.DecisionCancel {
		t.Errorf("cancel harus tercatat sebagai action, got %+v", detail.Actions)
	}

	// H6: event terminal terpublikasi (cancelled + approved + rejected).
	req2, _ := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetPayment, TargetID: 1, RequestedBy: uptr(100),
	})
	if _, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req2.ID, ActorID: 300, ActorRole: "owner", Decision: approval.DecisionApprove,
	}); err != nil {
		t.Fatal(err)
	}
	req3, _ := svc.SubmitRequest(ctx, apTenant, approval.SubmitRequestInput{
		TargetType: approval.TargetPayment, TargetID: 2, RequestedBy: uptr(100),
	})
	if _, err := svc.Act(ctx, apTenant, approval.ActInput{
		RequestID: req3.ID, ActorID: 300, ActorRole: "owner", Decision: approval.DecisionReject,
	}); err != nil {
		t.Fatal(err)
	}

	want := []approval.EventKind{
		approval.EventApprovalCancelled,
		approval.EventApprovalApproved,
		approval.EventApprovalRejected,
	}
	if len(sink.events) != len(want) {
		t.Fatalf("events = %d, want %d: %+v", len(sink.events), len(want), sink.events)
	}
	for i, k := range want {
		if sink.events[i].Kind != k {
			t.Errorf("event[%d] = %s, want %s", i, sink.events[i].Kind, k)
		}
	}
}
