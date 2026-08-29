package cancellation

// HTTP handlers Increment 8 (tanpa business logic).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/approval"
	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/project"
)

type Handler struct{ svc *Service }

func NewHandler(db *gorm.DB) *Handler {
	svc := NewService(db)
	// Gate opt-in Generic Approval Workflow (TargetCancellation) — pola budget.
	svc.SetApprovalGate(&approvalGateAdapter{svc: approval.NewService(approval.NewGORMRepository(db))})
	return &Handler{svc: svc}
}

// Svc mengekspos service untuk wiring seam lintas paket (main.go).
func (h *Handler) Svc() *Service { return h.svc }

type approvalGateAdapter struct{ svc *approval.Service }

func (a *approvalGateAdapter) RequireApproved(ctx context.Context, tenantID, cancellationID uint64) error {
	err := a.svc.RequireApproved(ctx, tenantID, approval.TargetCancellation, cancellationID)
	if errors.Is(err, approval.ErrApprovalRequired) {
		return ErrApprovalRequired
	}
	return err
}

func (h *Handler) Mount(r chi.Router) {
	r.With(auth.RequireWrite()).Post("/units/{unitID}/cancellations", h.request)
	r.Route("/cancellations", func(r chi.Router) {
		r.Get("/", h.list)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Get("/preview", h.preview)
			r.With(auth.RequireWrite()).Post("/approve", h.approve)
			r.With(auth.RequireWrite()).Post("/reject", h.reject)
			r.With(auth.RequireWrite()).Post("/process", h.process)
		})
	})
	r.Route("/refunds", func(r chi.Router) {
		r.Get("/", h.listRefunds)
		r.With(auth.RequireWrite()).Post("/from-booking/{bookingID}", h.createBookingRefund)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getRefund)
			r.With(auth.RequireWrite()).Post("/pay", h.payRefund)
		})
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func cxJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func cxErr(w http.ResponseWriter, status int, msg string) {
	cxJSON(w, status, map[string]string{"error": msg})
}

func cxAuth(r *http.Request) (uint64, *uint64, bool) {
	tid, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, nil, false
	}
	var actor *uint64
	if uid, uok := auth.UserIDFrom(r.Context()); uok {
		actor = &uid
	}
	return tid, actor, true
}

func cxParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func mapCxErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrUnitNotFound), errors.Is(err, ErrRefundNotFound):
		cxErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrActiveCancellationExists), errors.Is(err, ErrInvalidStateTransition),
		errors.Is(err, ErrRefundNotPending), errors.Is(err, ErrRefundAlreadyRequested),
		errors.Is(err, project.ErrUnitTransitionConflict):
		cxErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrPenaltyInvalid), errors.Is(err, ErrBankAccountInvalid):
		cxErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrUnitStageInvalid), errors.Is(err, ErrNothingToCancel),
		errors.Is(err, ErrPenaltyExceedsReceived), errors.Is(err, ErrTaxAlreadyPaid),
		errors.Is(err, ErrApprovalRequired), errors.Is(err, ErrNothingToRefund),
		errors.Is(err, ErrBookingNotRefundable):
		cxErr(w, http.StatusUnprocessableEntity, err.Error())
	default:
		cxErr(w, http.StatusInternalServerError, err.Error())
	}
}

// ── Cancellation endpoints ────────────────────────────────────────────────────

type requestDTO struct {
	Reason    string `json:"reason"`
	Penalty   string `json:"penalty,omitempty"`    // string desimal; kosong = 0
	EventDate string `json:"event_date,omitempty"` // YYYY-MM-DD; kosong = hari ini
}

func (h *Handler) request(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	unitID, err := cxParam(r, "unitID")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}
	var dto requestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		cxErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	penalty := domain.Zero
	if dto.Penalty != "" {
		p, perr := domain.NewMoney(dto.Penalty)
		if perr != nil {
			cxErr(w, http.StatusBadRequest, "penalty tidak valid: "+perr.Error())
			return
		}
		penalty = p
	}
	eventDate := time.Now()
	if dto.EventDate != "" {
		t, perr := time.Parse("2006-01-02", dto.EventDate)
		if perr != nil {
			cxErr(w, http.StatusBadRequest, "event_date harus YYYY-MM-DD")
			return
		}
		eventDate = t
	}
	c, err := h.svc.Request(r.Context(), tid, RequestInput{
		UnitID: unitID, Reason: dto.Reason, Penalty: penalty, EventDate: eventDate, ActorID: actor,
	})
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusCreated, c)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	cs, err := h.svc.List(r.Context(), tid, Status(r.URL.Query().Get("status")))
	if err != nil {
		mapCxErr(w, err)
		return
	}
	if cs == nil {
		cs = []*Cancellation{}
	}
	cxJSON(w, http.StatusOK, cs)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cxParam(r, "id")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	c, err := h.svc.Get(r.Context(), tid, id)
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusOK, c)
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cxParam(r, "id")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	p, err := h.svc.Preview(r.Context(), tid, id)
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusOK, p)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cxParam(r, "id")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	c, err := h.svc.Approve(r.Context(), tid, id, actor)
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusOK, c)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cxParam(r, "id")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&dto)
	c, err := h.svc.Reject(r.Context(), tid, id, dto.Reason, actor)
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusOK, c)
}

func (h *Handler) process(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cxParam(r, "id")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	c, err := h.svc.Process(r.Context(), tid, id, actor)
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusOK, c)
}

// ── Refund endpoints ──────────────────────────────────────────────────────────

func (h *Handler) listRefunds(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	rs, err := h.svc.ListRefunds(r.Context(), tid, RefundStatus(r.URL.Query().Get("status")))
	if err != nil {
		mapCxErr(w, err)
		return
	}
	if rs == nil {
		rs = []*Refund{}
	}
	cxJSON(w, http.StatusOK, rs)
}

func (h *Handler) getRefund(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cxParam(r, "id")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	rf, err := h.svc.GetRefund(r.Context(), tid, id)
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusOK, rf)
}

func (h *Handler) createBookingRefund(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	bookingID, err := cxParam(r, "bookingID")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "bookingID tidak valid")
		return
	}
	rf, err := h.svc.CreateBookingRefund(r.Context(), tid, bookingID, actor)
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusCreated, rf)
}

func (h *Handler) payRefund(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cxAuth(r)
	if !ok {
		cxErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cxParam(r, "id")
	if err != nil {
		cxErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		BankAccountCode string `json:"bank_account_code"`
		PayDate         string `json:"pay_date,omitempty"` // YYYY-MM-DD
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		cxErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	payDate := time.Now()
	if dto.PayDate != "" {
		t, perr := time.Parse("2006-01-02", dto.PayDate)
		if perr != nil {
			cxErr(w, http.StatusBadRequest, "pay_date harus YYYY-MM-DD")
			return
		}
		payDate = t
	}
	rf, err := h.svc.PayRefund(r.Context(), tid, id, dto.BankAccountCode, payDate, actor)
	if err != nil {
		mapCxErr(w, err)
		return
	}
	cxJSON(w, http.StatusOK, rf)
}
