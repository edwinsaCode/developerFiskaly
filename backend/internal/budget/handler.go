package budget

import (
	"context"

	"esaproperti/internal/approval"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// ── Handler ───────────────────────────────────────────────────────────────────

type Handler struct {
	svc *Service
}

// NewHandler wires repository dan service untuk production.
//
// Item 8 (UAT 2026-09-07): RAB (ApprovePlan) TIDAK memposting jurnal apa pun
// — RULE KLIEN 2026-09-04 (kapitalisasi Construction penuh saat approval)
// DICABUT klien. RAB murni budget/planning; Persediaan/HPP berasal dari cost
// entry aktual (lihat internal/cost).
func NewHandler(db *gorm.DB) *Handler {
	store := NewGORMRepository(db)
	realisasi := NewGORMRealisasiProvider(db)
	svc := NewService(store, realisasi)
	// Increment 5: RAB = konsumen pertama Generic Approval Workflow.
	// Gate opt-in — aktif hanya bila tenant mengonfigurasi workflow 'rab'.
	svc.SetApprovalGate(&approvalGateAdapter{svc: approval.NewService(approval.NewGORMRepository(db))})
	return &Handler{svc: svc}
}

// Svc mengekspos budget.Service untuk wiring lintas-modul (S5: dashboard
// membaca total RAB aktif via service kanonik ini).
func (h *Handler) Svc() *Service { return h.svc }

// approvalGateAdapter menjembatani approval.Service ke budget.ApprovalGate.
type approvalGateAdapter struct{ svc *approval.Service }

func (a *approvalGateAdapter) RequireApproved(ctx context.Context, tenantID, planID uint64) error {
	return a.svc.RequireApproved(ctx, tenantID, approval.TargetRAB, planID)
}

// Mount mendaftarkan semua route RAB ke router.
func (h *Handler) Mount(r chi.Router) {
	// Budget plan CRUD
	r.Route("/projects/{projectID}/budget-plans", func(r chi.Router) {
		r.Get("/", h.listPlans)
		r.With(auth.RequireWrite()).Post("/", h.createPlan)
		r.Get("/active", h.getActivePlan)
	})

	r.Route("/budget-plans/{planID}", func(r chi.Router) {
		r.Get("/", h.getPlan)
		r.With(auth.RequireWrite()).Post("/approve", h.approvePlan)

		// Budget items
		r.Route("/items", func(r chi.Router) {
			r.Get("/", h.listItems)
			r.With(auth.RequireWrite()).Post("/", h.addItem)
			r.With(auth.RequireWrite()).Delete("/{itemID}", h.deleteItem)
		})
	})

	// RAB vs Realisasi: per-kategori dan per-item
	r.Get("/projects/{projectID}/budget/rab-vs-realisasi", h.rabVsRealisasi)
	r.Get("/projects/{projectID}/budget/realisasi-per-item", h.realisasiPerItem)
	r.Get("/projects/{projectID}/budget/realisasi-konstruksi", h.realisasiKonstruksi)
}

// ── Plan handlers ─────────────────────────────────────────────────────────────

type createPlanDTO struct {
	PhaseID *uint64 `json:"phase_id"`
	Label   string  `json:"label"`
	Notes   string  `json:"notes"`
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseBudgetUint(r, "projectID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}

	var dto createPlanDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeBudgetError(w, http.StatusBadRequest, "request body tidak valid JSON")
		return
	}

	plan, err := h.svc.CreatePlan(r.Context(), tenantID, CreatePlanRequest{
		ProjectID: projectID,
		PhaseID:   dto.PhaseID,
		Label:     dto.Label,
		Notes:     dto.Notes,
	})
	if err != nil {
		writeBudgetDomainOrInternal(w, err)
		return
	}
	writeBudgetJSON(w, http.StatusCreated, plan)
}

func (h *Handler) getPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	planID, err := parseBudgetUint(r, "planID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "planID tidak valid")
		return
	}

	plan, err := h.svc.GetPlan(r.Context(), tenantID, planID)
	if err != nil {
		if errors.Is(err, ErrPlanNotFound) {
			writeBudgetError(w, http.StatusNotFound, err.Error())
			return
		}
		writeBudgetError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeBudgetJSON(w, http.StatusOK, plan)
}

func (h *Handler) listPlans(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseBudgetUint(r, "projectID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	phaseID := parseOptionalQueryUint(r, "phase_id")

	plans, err := h.svc.ListPlans(r.Context(), tenantID, projectID, phaseID)
	if err != nil {
		writeBudgetError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeBudgetJSON(w, http.StatusOK, plans)
}

func (h *Handler) getActivePlan(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseBudgetUint(r, "projectID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	phaseID := parseOptionalQueryUint(r, "phase_id")

	plan, err := h.svc.GetActivePlan(r.Context(), tenantID, projectID, phaseID)
	if err != nil {
		if errors.Is(err, ErrNoActivePlan) {
			writeBudgetError(w, http.StatusNotFound, err.Error())
			return
		}
		writeBudgetError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeBudgetJSON(w, http.StatusOK, plan)
}

type approvePlanDTO struct {
	ApprovedBy string `json:"approved_by"`
}

func (h *Handler) approvePlan(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	planID, err := parseBudgetUint(r, "planID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "planID tidak valid")
		return
	}

	var dto approvePlanDTO
	_ = json.NewDecoder(r.Body).Decode(&dto) // optional body

	plan, err := h.svc.ApprovePlan(r.Context(), tenantID, ApprovePlanRequest{
		PlanID:     planID,
		ApprovedBy: dto.ApprovedBy,
	})
	if err != nil {
		if errors.Is(err, approval.ErrApprovalRequired) {
			writeBudgetError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeBudgetDomainOrInternal(w, err)
		return
	}
	writeBudgetJSON(w, http.StatusOK, plan)
}

// ── Item handlers ─────────────────────────────────────────────────────────────

type addItemDTO struct {
	Category       string `json:"category"`
	Subcategory    string `json:"subcategory"`
	Description    string `json:"description"`
	BudgetedAmount string `json:"budgeted_amount"`
}

func (h *Handler) addItem(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	planID, err := parseBudgetUint(r, "planID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "planID tidak valid")
		return
	}

	var dto addItemDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeBudgetError(w, http.StatusBadRequest, "request body tidak valid JSON")
		return
	}

	amount, err := budgetMoneyFromString(dto.BudgetedAmount)
	if err != nil {
		writeBudgetError(w, http.StatusUnprocessableEntity, "budgeted_amount tidak valid: "+err.Error())
		return
	}

	item, err := h.svc.AddItem(r.Context(), tenantID, AddItemRequest{
		PlanID:         planID,
		Category:       BudgetCategory(dto.Category),
		Subcategory:    dto.Subcategory,
		Description:    dto.Description,
		BudgetedAmount: amount,
	})
	if err != nil {
		writeBudgetDomainOrInternal(w, err)
		return
	}
	writeBudgetJSON(w, http.StatusCreated, item)
}

func (h *Handler) listItems(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	planID, err := parseBudgetUint(r, "planID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "planID tidak valid")
		return
	}

	items, err := h.svc.ListItems(r.Context(), tenantID, planID)
	if err != nil {
		writeBudgetError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeBudgetJSON(w, http.StatusOK, items)
}

func (h *Handler) deleteItem(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	planID, err := parseBudgetUint(r, "planID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "planID tidak valid")
		return
	}
	itemID, err := parseBudgetUint(r, "itemID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "itemID tidak valid")
		return
	}

	if err := h.svc.DeleteItem(r.Context(), tenantID, planID, itemID); err != nil {
		writeBudgetDomainOrInternal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── RAB vs Realisasi ──────────────────────────────────────────────────────────

func (h *Handler) rabVsRealisasi(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseBudgetUint(r, "projectID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	phaseID := parseOptionalQueryUint(r, "phase_id")

	report, err := h.svc.GetRABvsRealisasi(r.Context(), tenantID, projectID, phaseID)
	if err != nil {
		if errors.Is(err, ErrNoActivePlan) {
			writeBudgetError(w, http.StatusNotFound, err.Error())
			return
		}
		writeBudgetError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeBudgetJSON(w, http.StatusOK, report)
}

func (h *Handler) realisasiPerItem(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseBudgetUint(r, "projectID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	phaseID := parseOptionalQueryUint(r, "phase_id")

	rows, err := h.svc.GetItemsRealisasi(r.Context(), tenantID, projectID, phaseID)
	if err != nil {
		if errors.Is(err, ErrNoActivePlan) {
			writeBudgetError(w, http.StatusNotFound, err.Error())
			return
		}
		writeBudgetError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeBudgetJSON(w, http.StatusOK, rows)
}

func (h *Handler) realisasiKonstruksi(w http.ResponseWriter, r *http.Request) {
	tenantID, err := budgetTenantID(r)
	if err != nil {
		writeBudgetError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseBudgetUint(r, "projectID")
	if err != nil {
		writeBudgetError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	phaseID := parseOptionalQueryUint(r, "phase_id")

	tree, err := h.svc.GetConstructionRealisasiTree(r.Context(), tenantID, projectID, phaseID)
	if err != nil {
		if errors.Is(err, ErrNoActivePlan) {
			writeBudgetError(w, http.StatusNotFound, err.Error())
			return
		}
		writeBudgetError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeBudgetJSON(w, http.StatusOK, tree)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func budgetTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeBudgetJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeBudgetError(w http.ResponseWriter, status int, msg string) {
	writeBudgetJSON(w, status, map[string]string{"error": msg})
}

func parseBudgetUint(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func parseOptionalQueryUint(r *http.Request, name string) *uint64 {
	s := r.URL.Query().Get(name)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func budgetMoneyFromString(s string) (domain.Money, error) {
	if s == "" {
		return domain.Zero, errors.New("budgeted_amount wajib diisi")
	}
	return domain.NewMoney(s)
}

func isBudgetDomainError(err error) bool {
	return errors.Is(err, ErrPlanNotFound) ||
		errors.Is(err, ErrItemNotFound) ||
		errors.Is(err, ErrPlanNotDraft) ||
		errors.Is(err, ErrPlanNotApprovable) ||
		errors.Is(err, ErrNoItemsToApprove) ||
		errors.Is(err, ErrInvalidCategory) ||
		errors.Is(err, ErrAmountFractional) ||
		errors.Is(err, ErrAmountZeroOrNeg) ||
		errors.Is(err, ErrProjectRequired) ||
		errors.Is(err, ErrLabelRequired) ||
		errors.Is(err, ErrNoActivePlan) ||
		errors.Is(err, ErrInvalidConstructionSubcategory)
}

func writeBudgetDomainOrInternal(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrPlanNotFound) || errors.Is(err, ErrItemNotFound) {
		writeBudgetError(w, http.StatusNotFound, err.Error())
		return
	}
	if isBudgetDomainError(err) {
		writeBudgetError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeBudgetError(w, http.StatusInternalServerError, "internal error")
}
