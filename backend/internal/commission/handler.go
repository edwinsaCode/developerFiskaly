package commission

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/approval"
	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

type Handler struct{ svc *Service }

func NewHandler(db *gorm.DB) *Handler {
	svc := NewService(db)
	svc.SetApprovalGate(&gateAdapter{svc: approval.NewService(approval.NewGORMRepository(db))})
	return &Handler{svc: svc}
}

type gateAdapter struct{ svc *approval.Service }

func (a *gateAdapter) RequireApproved(ctx context.Context, tenantID, commissionID uint64) error {
	err := a.svc.RequireApproved(ctx, tenantID, approval.TargetCommission, commissionID)
	if errors.Is(err, approval.ErrApprovalRequired) {
		return ErrApprovalRequired
	}
	return err
}

func (h *Handler) Mount(r chi.Router) {
	r.Route("/commission-rules", func(r chi.Router) {
		r.Get("/", h.listRules)
		r.With(auth.RequireWrite()).Post("/", h.createRule)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getRule)
			r.With(auth.RequireWrite()).Put("/active", h.setRuleActive)
		})
	})
	r.Route("/commissions", func(r chi.Router) {
		r.Get("/", h.list)
		r.With(auth.RequireWrite()).Post("/calculate", h.calculate)
		r.With(auth.RequireWrite()).Post("/sync-cancellations", h.syncCancellations)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.get)
			r.With(auth.RequireWrite()).Post("/approve", h.approve)
			r.With(auth.RequireWrite()).Post("/make-payable", h.makePayable)
			r.With(auth.RequireWrite()).Post("/pay", h.pay)
			r.With(auth.RequireWrite()).Post("/cancel", h.cancel)
		})
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func cmJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func cmErr(w http.ResponseWriter, status int, msg string) {
	cmJSON(w, status, map[string]string{"error": msg})
}

func cmAuth(r *http.Request) (uint64, *uint64, bool) {
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

func cmParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func mapCmErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrRuleNotFound):
		cmErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidStateTransition):
		cmErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrRuleInvalid), errors.Is(err, ErrBankAccountInvalid):
		cmErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrBasisNotImplemented), errors.Is(err, ErrTriggerNotImplemented),
		errors.Is(err, ErrApprovalRequired):
		cmErr(w, http.StatusUnprocessableEntity, err.Error())
	default:
		cmErr(w, http.StatusInternalServerError, err.Error())
	}
}

// ── Rules ─────────────────────────────────────────────────────────────────────

type ruleDTO struct {
	Name           string  `json:"name"`
	Basis          string  `json:"basis"`
	Rate           string  `json:"rate,omitempty"`        // "0.025" = 2.5%
	FlatAmount     string  `json:"flat_amount,omitempty"` // rupiah bulat
	TriggerEvent   string  `json:"trigger_event,omitempty"`
	SalesPersonID  *uint64 `json:"sales_person_id,omitempty"`
	ProjectID      *uint64 `json:"project_id,omitempty"`
	UnitType       *string `json:"unit_type,omitempty"`
	ExpenseAccount string  `json:"expense_account,omitempty"`
	PayableAccount string  `json:"payable_account,omitempty"`
	EffectiveFrom  string  `json:"effective_from"` // YYYY-MM-DD
	EffectiveTo    *string `json:"effective_to,omitempty"`
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	var dto ruleDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		cmErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	rule := &Rule{
		Name: dto.Name, Basis: Basis(dto.Basis),
		TriggerEvent:  Trigger(dto.TriggerEvent),
		SalesPersonID: dto.SalesPersonID, ProjectID: dto.ProjectID, UnitType: dto.UnitType,
		ExpenseAccount: dto.ExpenseAccount, PayableAccount: dto.PayableAccount,
		IsActive: true, CreatedBy: actor,
	}
	if rule.TriggerEvent == "" {
		rule.TriggerEvent = TriggerAtBAST
	}
	if dto.Rate != "" {
		d, err := decimal.NewFromString(dto.Rate)
		if err != nil {
			cmErr(w, http.StatusBadRequest, "rate tidak valid")
			return
		}
		rule.Rate = d
	}
	if dto.FlatAmount != "" {
		m, err := domain.NewMoney(dto.FlatAmount)
		if err != nil {
			cmErr(w, http.StatusBadRequest, "flat_amount tidak valid: "+err.Error())
			return
		}
		rule.FlatAmount = m
	}
	ef, err := time.Parse("2006-01-02", dto.EffectiveFrom)
	if err != nil {
		cmErr(w, http.StatusBadRequest, "effective_from harus YYYY-MM-DD")
		return
	}
	rule.EffectiveFrom = ef
	if dto.EffectiveTo != nil && *dto.EffectiveTo != "" {
		et, perr := time.Parse("2006-01-02", *dto.EffectiveTo)
		if perr != nil {
			cmErr(w, http.StatusBadRequest, "effective_to harus YYYY-MM-DD")
			return
		}
		rule.EffectiveTo = &et
	}
	created, err := h.svc.CreateRule(r.Context(), tid, rule)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusCreated, created)
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	rules, err := h.svc.ListRules(r.Context(), tid)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	if rules == nil {
		rules = []*Rule{}
	}
	cmJSON(w, http.StatusOK, rules)
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cmParam(r, "id")
	if err != nil {
		cmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	rule, err := h.svc.GetRule(r.Context(), tid, id)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, rule)
}

func (h *Handler) setRuleActive(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cmParam(r, "id")
	if err != nil {
		cmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		cmErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	rule, err := h.svc.SetRuleActive(r.Context(), tid, id, dto.Active)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, rule)
}

// ── Commissions ───────────────────────────────────────────────────────────────

func (h *Handler) calculate(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	n, err := h.svc.Calculate(r.Context(), tid, actor)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, map[string]int{"created": n})
}

func (h *Handler) syncCancellations(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	cancelled, clawed, err := h.svc.SyncCancellations(r.Context(), tid, actor)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, map[string]int{"cancelled": cancelled, "clawed_back": clawed})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	var spID uint64
	if q := r.URL.Query().Get("sales_person_id"); q != "" {
		spID, _ = strconv.ParseUint(q, 10, 64)
	}
	cs, err := h.svc.List(r.Context(), tid, Status(r.URL.Query().Get("status")), spID)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	if cs == nil {
		cs = []*Commission{}
	}
	cmJSON(w, http.StatusOK, cs)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cmParam(r, "id")
	if err != nil {
		cmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	c, err := h.svc.Get(r.Context(), tid, id)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, c)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cmParam(r, "id")
	if err != nil {
		cmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	c, err := h.svc.Approve(r.Context(), tid, id, actor)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, c)
}

func (h *Handler) makePayable(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cmParam(r, "id")
	if err != nil {
		cmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		AccrualDate string `json:"accrual_date,omitempty"` // YYYY-MM-DD
	}
	_ = json.NewDecoder(r.Body).Decode(&dto)
	date := time.Now()
	if dto.AccrualDate != "" {
		t, perr := time.Parse("2006-01-02", dto.AccrualDate)
		if perr != nil {
			cmErr(w, http.StatusBadRequest, "accrual_date harus YYYY-MM-DD")
			return
		}
		date = t
	}
	c, err := h.svc.MakePayable(r.Context(), tid, id, date, actor)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, c)
}

func (h *Handler) pay(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cmParam(r, "id")
	if err != nil {
		cmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		BankAccountCode string `json:"bank_account_code"`
		PayDate         string `json:"pay_date,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		cmErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	date := time.Now()
	if dto.PayDate != "" {
		t, perr := time.Parse("2006-01-02", dto.PayDate)
		if perr != nil {
			cmErr(w, http.StatusBadRequest, "pay_date harus YYYY-MM-DD")
			return
		}
		date = t
	}
	c, err := h.svc.Pay(r.Context(), tid, id, dto.BankAccountCode, date, actor)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, c)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := cmAuth(r)
	if !ok {
		cmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := cmParam(r, "id")
	if err != nil {
		cmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&dto)
	c, err := h.svc.Cancel(r.Context(), tid, id, dto.Reason, actor)
	if err != nil {
		mapCmErr(w, err)
		return
	}
	cmJSON(w, http.StatusOK, c)
}
