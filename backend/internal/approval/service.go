package approval

import (
	"context"
	"errors"
	"fmt"

	"esaproperti/internal/domain"
)

// ── Store interface ───────────────────────────────────────────────────────────

type Store interface {
	CreateWorkflowWithSteps(ctx context.Context, w *Workflow, steps []Step) error
	FindActiveWorkflow(ctx context.Context, tenantID uint64, target TargetType) (*Workflow, error)
	FindWorkflowByID(ctx context.Context, tenantID, id uint64) (*Workflow, error)
	ListWorkflows(ctx context.Context, tenantID uint64) ([]*Workflow, error)
	SetWorkflowActive(ctx context.Context, tenantID, id uint64, active bool) error

	CreateRequest(ctx context.Context, req *Request) error
	FindRequestByID(ctx context.Context, tenantID, id uint64) (*Request, error)
	ListRequestsByTarget(ctx context.Context, tenantID uint64, target TargetType, targetID uint64) ([]*Request, error)
	HasApprovedRequest(ctx context.Context, tenantID uint64, target TargetType, targetID uint64) (bool, error)
	ApplyAction(ctx context.Context, tenantID, requestID uint64, act *Action,
		evaluate func(req *Request, stepActions []Action) (RequestStatus, int, error)) (*Request, error)
	CancelRequest(ctx context.Context, tenantID, requestID uint64, actorID uint64) error
	NextWorkflowRevision(ctx context.Context, tenantID uint64, target TargetType) (int, error)
}

// Service adalah ENGINE approval generik (blueprint §10). Reusable: modul
// melampirkan Request via target polimorfik; transisi domain digate hasil
// approval — bukan logika approve terpisah per modul.
type Service struct {
	store Store
	// events (H6): seam publikasi domain event. Engine TIDAK PERNAH memanggil
	// modul lain langsung; default = noop. Kegagalan sink tidak membatalkan
	// keputusan (fakta sudah persist).
	events EventSink
}

func NewService(store Store) *Service {
	return &Service{store: store, events: noopSink{}}
}

// SetEventSink memasang sink event (wiring; nil diabaikan).
func (s *Service) SetEventSink(sink EventSink) {
	if sink != nil {
		s.events = sink
	}
}

// ── Workflow config ───────────────────────────────────────────────────────────

type StepInput struct {
	Seq          int
	Name         string
	ApproverRole string
	MinAmount    domain.Money
	Quorum       int
	// Seam eskalasi (H4) — vocabulary; scheduler menyusul.
	EscalationAfterHours int
	EscalationRole       string
}

type CreateWorkflowRequest struct {
	TargetType TargetType
	Name       string
	Steps      []StepInput
	CreatedBy  *uint64
}

// CreateWorkflow membuat workflow AKTIF baru untuk sebuah target_type.
// Hanya satu workflow aktif per target_type (nonaktifkan yang lama dulu).
func (s *Service) CreateWorkflow(ctx context.Context, tenantID uint64, req CreateWorkflowRequest) (*Workflow, error) {
	if !req.TargetType.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrTargetTypeInvalid, req.TargetType)
	}
	if req.Name == "" {
		return nil, ErrWorkflowNameRequired
	}
	if len(req.Steps) == 0 {
		return nil, ErrStepsRequired
	}
	steps := make([]Step, len(req.Steps))
	for i, in := range req.Steps {
		if in.Seq != i+1 {
			return nil, ErrStepSeqInvalid // wajib 1..n berurutan
		}
		if in.ApproverRole == "" {
			return nil, ErrStepRoleRequired
		}
		if in.Quorum == 0 {
			in.Quorum = 1
		}
		if in.Quorum < 1 {
			return nil, ErrStepQuorumInvalid
		}
		if in.MinAmount.IsNeg() {
			return nil, ErrStepMinAmountNegative
		}
		steps[i] = Step{
			Seq: in.Seq, Name: in.Name, ApproverRole: in.ApproverRole,
			MinAmount: in.MinAmount, Quorum: in.Quorum,
			EscalationAfterHours: in.EscalationAfterHours,
			EscalationRole:       in.EscalationRole,
		}
	}
	// H3: workflow pengganti utk target_type sama = revision berikutnya.
	revision, err := s.store.NextWorkflowRevision(ctx, tenantID, req.TargetType)
	if err != nil {
		return nil, err
	}
	active := "Y"
	w := &Workflow{
		TenantID:   tenantID,
		TargetType: req.TargetType,
		Name:       req.Name,
		Revision:   revision,
		IsActive:   true,
		ActiveKey:  &active,
		CreatedBy:  req.CreatedBy,
	}
	if err := s.store.CreateWorkflowWithSteps(ctx, w, steps); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *Service) GetWorkflow(ctx context.Context, tenantID, id uint64) (*Workflow, error) {
	return s.store.FindWorkflowByID(ctx, tenantID, id)
}

func (s *Service) ListWorkflows(ctx context.Context, tenantID uint64) ([]*Workflow, error) {
	return s.store.ListWorkflows(ctx, tenantID)
}

func (s *Service) SetWorkflowActive(ctx context.Context, tenantID, id uint64, active bool) error {
	return s.store.SetWorkflowActive(ctx, tenantID, id, active)
}

// ── Runtime: Submit / Act / Cancel ────────────────────────────────────────────

type SubmitRequestInput struct {
	TargetType TargetType
	TargetID   uint64
	Amount     domain.Money // konteks threshold step (0 bila tak relevan)
	// TargetVersion (H2 seam): versi dokumen saat submit (0 = tidak diketahui).
	TargetVersion int
	Currency      string // kosong = IDR
	Notes         string
	RequestedBy   *uint64
}

// applicableSteps: step yang berlaku untuk amount ini (threshold Approval Matrix).
func applicableSteps(w *Workflow, amount domain.Money) []Step {
	var out []Step
	for _, st := range w.Steps {
		if st.AppliesTo(amount) {
			out = append(out, st)
		}
	}
	return out
}

// SubmitRequest membuat Approval Request untuk sebuah dokumen target.
// Bila SELURUH step ter-skip oleh threshold (amount di bawah semua min_amount),
// request langsung APPROVED (tidak ada yang perlu memutus).
func (s *Service) SubmitRequest(ctx context.Context, tenantID uint64, in SubmitRequestInput) (*Request, error) {
	if !in.TargetType.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrTargetTypeInvalid, in.TargetType)
	}
	w, err := s.store.FindActiveWorkflow(ctx, tenantID, in.TargetType)
	if err != nil {
		return nil, err
	}
	steps := applicableSteps(w, in.Amount)

	// H1: bekukan snapshot workflow (nama, revision, steps: role/quorum/
	// threshold/escalation) — evaluasi keputusan membaca SNAPSHOT, bukan
	// workflow hidup (pola Terms Snapshot / Tax Rule revision).
	wfSnap, err := NewWorkflowSnapshot(w).JSON()
	if err != nil {
		return nil, fmt.Errorf("bekukan workflow snapshot: %w", err)
	}
	// H2: bekukan konteks minimum target (invalidation = seam, belum rule).
	currency := in.Currency
	if currency == "" {
		currency = "IDR"
	}
	tgtSnap, err := (TargetSnapshot{
		TargetType:    in.TargetType,
		TargetID:      in.TargetID,
		TargetVersion: in.TargetVersion,
		Amount:        in.Amount.String(),
		Currency:      currency,
		Notes:         in.Notes,
	}).JSON()
	if err != nil {
		return nil, fmt.Errorf("bekukan target snapshot: %w", err)
	}
	revision := w.Revision

	req := &Request{
		TenantID:           tenantID,
		WorkflowID:         w.ID,
		TargetType:         in.TargetType,
		TargetID:           in.TargetID,
		Amount:             in.Amount,
		Notes:              in.Notes,
		RequestedBy:        in.RequestedBy,
		WorkflowRevision:   &revision,
		WorkflowSnapshotJS: &wfSnap,
		TargetSnapshotJS:   &tgtSnap,
	}
	if len(steps) == 0 {
		// Semua step di bawah threshold → auto-approved (tanpa active_key:
		// terminal sejak lahir; UNIQUE tidak menghalangi request berikutnya).
		req.Status = StatusApproved
		req.CurrentSeq = 0
	} else {
		active := "Y"
		req.Status = StatusPending
		req.CurrentSeq = steps[0].Seq
		req.ActiveKey = &active
	}
	if err := s.store.CreateRequest(ctx, req); err != nil {
		return nil, err
	}
	return req, nil
}

type ActInput struct {
	RequestID uint64
	ActorID   uint64
	ActorRole string
	Decision  Decision
	Comment   string
}

// Act memproses satu keputusan approver pada step berjalan:
//   - reject → request REJECTED (terminal).
//   - approve → bila jumlah approve pada step mencapai quorum → maju ke step
//     berlaku berikutnya, atau APPROVED bila step terakhir.
//
// Validasi: request aktif, role actor = approver_role step, satu actor satu
// keputusan per step. Atomik + row-lock (repo.ApplyAction).
func (s *Service) Act(ctx context.Context, tenantID uint64, in ActInput) (*Request, error) {
	if !in.Decision.Valid() {
		return nil, ErrDecisionInvalid
	}
	if in.Decision == DecisionDelegate {
		// H4: vocabulary siap; eksekusi delegasi menyusul (Approval Matrix P2).
		return nil, ErrDelegateNotImplemented
	}
	current, err := s.store.FindRequestByID(ctx, tenantID, in.RequestID)
	if err != nil {
		return nil, err
	}
	// H1: step untuk evaluasi berasal dari SNAPSHOT beku request. Fallback ke
	// workflow hidup HANYA untuk request pra-hardening (snapshot NULL).
	steps, err := s.stepsForRequest(ctx, tenantID, current)
	if err != nil {
		return nil, err
	}

	act := &Action{
		ActorID:   in.ActorID,
		ActorRole: in.ActorRole,
		Decision:  in.Decision,
		Comment:   in.Comment,
	}
	result, err := s.store.ApplyAction(ctx, tenantID, in.RequestID, act,
		func(req *Request, stepActions []Action) (RequestStatus, int, error) {
			if req.Status.Terminal() {
				return "", 0, ErrRequestTerminal
			}
			step := findStep(steps, req.CurrentSeq)
			if step == nil {
				return "", 0, fmt.Errorf("step %d tidak ditemukan pada snapshot request %d", req.CurrentSeq, req.ID)
			}
			if in.ActorRole != step.ApproverRole {
				return "", 0, fmt.Errorf("%w: step %d butuh role %q", ErrActorRoleNotAllowed, step.Seq, step.ApproverRole)
			}
			for _, a := range stepActions {
				if a.ActorID == in.ActorID {
					return "", 0, ErrActorAlreadyActed
				}
			}

			if in.Decision == DecisionReject {
				return StatusRejected, req.CurrentSeq, nil
			}

			// approve: hitung quorum (aksi approve step ini + aksi baru ini).
			approvals := 1
			for _, a := range stepActions {
				if a.Decision == DecisionApprove {
					approvals++
				}
			}
			if approvals < step.Quorum {
				return StatusInReview, req.CurrentSeq, nil // menunggu approver lain
			}
			// Quorum terpenuhi → step berlaku berikutnya, atau APPROVED.
			if next := nextStep(steps, req.CurrentSeq); next != nil {
				return StatusInReview, next.Seq, nil
			}
			return StatusApproved, req.CurrentSeq, nil
		})
	if err != nil {
		return nil, err
	}
	// H6: publikasi domain event pada status terminal (setelah persist).
	switch result.Status {
	case StatusApproved:
		s.events.Publish(Event{Kind: EventApprovalApproved, TenantID: tenantID,
			RequestID: result.ID, TargetType: result.TargetType, TargetID: result.TargetID})
	case StatusRejected:
		s.events.Publish(Event{Kind: EventApprovalRejected, TenantID: tenantID,
			RequestID: result.ID, TargetType: result.TargetType, TargetID: result.TargetID})
	}
	return result, nil
}

// stepsForRequest (H1): step evaluasi dari SNAPSHOT beku; request pra-hardening
// (snapshot NULL) fallback ke workflow hidup — kompat, didokumentasikan.
func (s *Service) stepsForRequest(ctx context.Context, tenantID uint64, req *Request) ([]Step, error) {
	if req.WorkflowSnapshotJS != nil && *req.WorkflowSnapshotJS != "" {
		snap, err := ParseWorkflowSnapshot(*req.WorkflowSnapshotJS)
		if err != nil {
			return nil, err
		}
		steps := make([]Step, 0, len(snap.Steps))
		for _, st := range snap.Steps {
			minAmt, merr := domain.NewMoney(st.MinAmount)
			if merr != nil {
				return nil, fmt.Errorf("snapshot min_amount rusak: %w", merr)
			}
			step := Step{
				Seq: st.Seq, Name: st.Name, ApproverRole: st.ApproverRole,
				MinAmount: minAmt, Quorum: st.Quorum,
				EscalationAfterHours: st.EscalationAfterHours,
				EscalationRole:       st.EscalationRole,
			}
			if step.AppliesTo(req.Amount) {
				steps = append(steps, step)
			}
		}
		return steps, nil
	}
	// Legacy fallback (request dibuat sebelum hardening 5.1).
	w, err := s.store.FindWorkflowByID(ctx, tenantID, req.WorkflowID)
	if err != nil {
		return nil, err
	}
	return applicableSteps(w, req.Amount), nil
}

func findStep(steps []Step, seq int) *Step {
	for i := range steps {
		if steps[i].Seq == seq {
			return &steps[i]
		}
	}
	return nil
}

func nextStep(steps []Step, seq int) *Step {
	for i := range steps {
		if steps[i].Seq > seq {
			return &steps[i]
		}
	}
	return nil
}

// Cancel membatalkan request aktif — hanya oleh pemohonnya.
func (s *Service) Cancel(ctx context.Context, tenantID, requestID uint64, actorID uint64) error {
	req, err := s.store.FindRequestByID(ctx, tenantID, requestID)
	if err != nil {
		return err
	}
	if req.Status.Terminal() {
		return ErrRequestTerminal
	}
	if req.RequestedBy == nil || *req.RequestedBy != actorID {
		return ErrOnlyRequesterCanCancel
	}
	if err := s.store.CancelRequest(ctx, tenantID, requestID, actorID); err != nil {
		return err
	}
	// H6: publikasi event pembatalan.
	s.events.Publish(Event{Kind: EventApprovalCancelled, TenantID: tenantID,
		RequestID: requestID, TargetType: req.TargetType, TargetID: req.TargetID})
	return nil
}

func (s *Service) GetRequest(ctx context.Context, tenantID, id uint64) (*Request, error) {
	return s.store.FindRequestByID(ctx, tenantID, id)
}

func (s *Service) ListRequestsByTarget(ctx context.Context, tenantID uint64, target TargetType, targetID uint64) ([]*Request, error) {
	return s.store.ListRequestsByTarget(ctx, tenantID, target, targetID)
}

// ── Gate untuk modul konsumen (blueprint §10: "digate hasil approval") ────────

// RequireApproved menegakkan gate OPT-IN pada dokumen target:
//   - TIDAK ada workflow aktif utk target_type → nil (governance belum
//     dikonfigurasi; perilaku modul seperti sebelumnya — additive).
//   - Ada workflow aktif → dokumen WAJIB punya request APPROVED, selain itu
//     ErrApprovalRequired.
func (s *Service) RequireApproved(ctx context.Context, tenantID uint64, target TargetType, targetID uint64) error {
	_, err := s.store.FindActiveWorkflow(ctx, tenantID, target)
	if err != nil {
		if errors.Is(err, ErrNoActiveWorkflow) {
			return nil // opt-in: tanpa konfigurasi = tanpa gate
		}
		return err
	}
	ok, err := s.store.HasApprovedRequest(ctx, tenantID, target, targetID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrApprovalRequired
	}
	return nil
}
