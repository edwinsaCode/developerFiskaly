package approval

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// Handler menyajikan konfigurasi workflow + runtime request.
type Handler struct {
	svc *Service
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{svc: NewService(NewGORMRepository(db))}
}

// Service mengekspos engine (dipakai wiring modul konsumen — instance tunggal).
func (h *Handler) Service() *Service { return h.svc }

func (h *Handler) Mount(r chi.Router) {
	// Konfigurasi (governance) — write: owner saja (mengatur siapa meng-approve apa).
	r.Route("/approval-workflows", func(r chi.Router) {
		r.Get("/", h.listWorkflows)
		r.With(auth.RequireRole("owner")).Post("/", h.createWorkflow)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getWorkflow)
			r.With(auth.RequireRole("owner")).Put("/active", h.setWorkflowActive)
		})
	})

	// Runtime request — semua role ber-JWT boleh submit; keputusan divalidasi
	// terhadap approver_role step di service.
	r.Route("/approval-requests", func(r chi.Router) {
		r.Get("/", h.listRequestsByTarget) // ?target_type=&target_id=
		r.With(auth.RequireWrite()).Post("/", h.submitRequest)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getRequest)
			r.With(auth.RequireWrite()).Post("/actions", h.act)
			r.With(auth.RequireWrite()).Post("/cancel", h.cancel)
		})
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func approvalTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeApprovalJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeApprovalError(w http.ResponseWriter, status int, msg string) {
	writeApprovalJSON(w, status, map[string]string{"error": msg})
}

func writeApprovalServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrWorkflowNotFound), errors.Is(err, ErrRequestNotFound):
		writeApprovalError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrWorkflowActiveExists), errors.Is(err, ErrActiveRequestExists),
		errors.Is(err, ErrRequestTerminal), errors.Is(err, ErrActorAlreadyActed):
		writeApprovalError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrActorRoleNotAllowed), errors.Is(err, ErrOnlyRequesterCanCancel):
		writeApprovalError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrDelegateNotImplemented):
		writeApprovalError(w, http.StatusNotImplemented, err.Error())
	case errors.Is(err, ErrTargetTypeInvalid), errors.Is(err, ErrWorkflowNameRequired),
		errors.Is(err, ErrStepsRequired), errors.Is(err, ErrStepRoleRequired),
		errors.Is(err, ErrStepSeqInvalid), errors.Is(err, ErrStepQuorumInvalid),
		errors.Is(err, ErrStepMinAmountNegative), errors.Is(err, ErrDecisionInvalid),
		errors.Is(err, ErrNoActiveWorkflow):
		writeApprovalError(w, http.StatusBadRequest, err.Error())
	default:
		writeApprovalError(w, http.StatusInternalServerError, err.Error())
	}
}

func approvalActor(r *http.Request) (id uint64, role string) {
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		id = uid
	}
	if rl, ok := auth.RoleFrom(r.Context()); ok {
		role = rl
	}
	return id, role
}

// ── Workflow handlers ─────────────────────────────────────────────────────────

type stepDTO struct {
	Seq          int    `json:"seq"`
	Name         string `json:"name"`
	ApproverRole string `json:"approver_role"`
	MinAmount    string `json:"min_amount"` // string — hindari float
	Quorum       int    `json:"quorum"`
	// Seam eskalasi (H4) — vocabulary; scheduler menyusul.
	EscalationAfterHours int    `json:"escalation_after_hours"`
	EscalationRole       string `json:"escalation_role"`
}

type createWorkflowDTO struct {
	TargetType string    `json:"target_type"`
	Name       string    `json:"name"`
	Steps      []stepDTO `json:"steps"`
}

func (h *Handler) createWorkflow(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto createWorkflowDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeApprovalError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	steps := make([]StepInput, len(dto.Steps))
	for i, s := range dto.Steps {
		amount := domain.Zero
		if s.MinAmount != "" {
			m, merr := domain.NewMoney(s.MinAmount)
			if merr != nil {
				writeApprovalError(w, http.StatusBadRequest, "min_amount tidak valid: "+merr.Error())
				return
			}
			amount = m
		}
		steps[i] = StepInput{Seq: s.Seq, Name: s.Name, ApproverRole: s.ApproverRole, MinAmount: amount, Quorum: s.Quorum,
			EscalationAfterHours: s.EscalationAfterHours, EscalationRole: s.EscalationRole}
	}
	actorID, _ := approvalActor(r)
	var createdBy *uint64
	if actorID != 0 {
		createdBy = &actorID
	}
	out, err := h.svc.CreateWorkflow(r.Context(), tenantID, CreateWorkflowRequest{
		TargetType: TargetType(dto.TargetType),
		Name:       dto.Name,
		Steps:      steps,
		CreatedBy:  createdBy,
	})
	if err != nil {
		writeApprovalServiceError(w, err)
		return
	}
	writeApprovalJSON(w, http.StatusCreated, out)
}

func (h *Handler) listWorkflows(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	out, err := h.svc.ListWorkflows(r.Context(), tenantID)
	if err != nil {
		writeApprovalError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeApprovalJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getWorkflow(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeApprovalError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	out, err := h.svc.GetWorkflow(r.Context(), tenantID, id)
	if err != nil {
		writeApprovalServiceError(w, err)
		return
	}
	writeApprovalJSON(w, http.StatusOK, out)
}

func (h *Handler) setWorkflowActive(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeApprovalError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		IsActive bool `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeApprovalError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if err := h.svc.SetWorkflowActive(r.Context(), tenantID, id, dto.IsActive); err != nil {
		writeApprovalServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Request handlers ──────────────────────────────────────────────────────────

type submitRequestDTO struct {
	TargetType string `json:"target_type"`
	TargetID   uint64 `json:"target_id"`
	Amount     string `json:"amount"` // opsional; string — hindari float
	// TargetVersion (H2 seam): versi dokumen saat submit (opsional).
	TargetVersion int    `json:"target_version"`
	Currency      string `json:"currency"` // kosong = IDR
	Notes         string `json:"notes"`
}

func (h *Handler) submitRequest(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto submitRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeApprovalError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	amount := domain.Zero
	if dto.Amount != "" {
		m, merr := domain.NewMoney(dto.Amount)
		if merr != nil {
			writeApprovalError(w, http.StatusBadRequest, "amount tidak valid: "+merr.Error())
			return
		}
		amount = m
	}
	actorID, _ := approvalActor(r)
	var requestedBy *uint64
	if actorID != 0 {
		requestedBy = &actorID
	}
	out, err := h.svc.SubmitRequest(r.Context(), tenantID, SubmitRequestInput{
		TargetType:    TargetType(dto.TargetType),
		TargetID:      dto.TargetID,
		Amount:        amount,
		TargetVersion: dto.TargetVersion,
		Currency:      dto.Currency,
		Notes:         dto.Notes,
		RequestedBy:   requestedBy,
	})
	if err != nil {
		writeApprovalServiceError(w, err)
		return
	}
	writeApprovalJSON(w, http.StatusCreated, out)
}

func (h *Handler) listRequestsByTarget(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	target := TargetType(r.URL.Query().Get("target_type"))
	targetID, err := strconv.ParseUint(r.URL.Query().Get("target_id"), 10, 64)
	if err != nil || !target.Valid() {
		writeApprovalError(w, http.StatusBadRequest, "target_type & target_id wajib valid")
		return
	}
	out, err := h.svc.ListRequestsByTarget(r.Context(), tenantID, target, targetID)
	if err != nil {
		writeApprovalError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeApprovalJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getRequest(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeApprovalError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	out, err := h.svc.GetRequest(r.Context(), tenantID, id)
	if err != nil {
		writeApprovalServiceError(w, err)
		return
	}
	writeApprovalJSON(w, http.StatusOK, out)
}

type actDTO struct {
	Decision string `json:"decision"` // approve|reject
	Comment  string `json:"comment"`
}

func (h *Handler) act(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeApprovalError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto actDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeApprovalError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	actorID, actorRole := approvalActor(r)
	out, err := h.svc.Act(r.Context(), tenantID, ActInput{
		RequestID: id,
		ActorID:   actorID,
		ActorRole: actorRole,
		Decision:  Decision(dto.Decision),
		Comment:   dto.Comment,
	})
	if err != nil {
		writeApprovalServiceError(w, err)
		return
	}
	writeApprovalJSON(w, http.StatusOK, out)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	tenantID, err := approvalTenantID(r)
	if err != nil {
		writeApprovalError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeApprovalError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	actorID, _ := approvalActor(r)
	if err := h.svc.Cancel(r.Context(), tenantID, id, actorID); err != nil {
		writeApprovalServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
