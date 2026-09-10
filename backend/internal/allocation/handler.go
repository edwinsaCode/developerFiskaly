package allocation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// allocSvc is the interface the Handler depends on — enables unit testing without a real DB.
type allocSvc interface {
	GetBasis(ctx context.Context, tenantID, projectID uint64) (*AllocationConfig, error)
	SetBasis(ctx context.Context, tenantID, projectID uint64, basis AllocationBasis) error
	ComputeAllocation(ctx context.Context, tenantID, projectID uint64) ([]AllocationResult, error)
	Execute(ctx context.Context, tenantID, projectID, userID uint64) (*AllocationExecution, error)
	ListExecutions(ctx context.Context, tenantID, projectID uint64) ([]*AllocationExecution, error)
}

// Handler menyediakan endpoint HTTP untuk konfigurasi basis alokasi dan kalkulasi alokasi.
type Handler struct {
	svc allocSvc
}

// NewHandler membangun production handler yang terhubung ke GORM.
func NewHandler(db *gorm.DB) *Handler {
	repo := NewGORMRepository(db)
	svc := NewService(repo, repo, repo,
		WithExecutionStore(repo),
		WithUserEmailFinder(repo),
		WithLandPoolSource(repo),
		WithHardPoolSource(repo, repo),
	)
	return &Handler{svc: svc}
}

// newHandlerWithSvc builds a Handler from an injected service — for testing only.
func newHandlerWithSvc(svc allocSvc) *Handler { return &Handler{svc: svc} }

// Mount mendaftarkan semua route alokasi.
// Caller wajib sudah apply auth.Middleware upstream.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/projects/{projectID}/allocation", func(r chi.Router) {
		r.Get("/config", h.getConfig)
		r.With(auth.RequireWrite()).Put("/config", h.setConfig)
		r.Get("/compute", h.compute)
		r.With(auth.RequireWrite()).Post("/execute", h.executeAllocation)
		r.Get("/history", h.listHistory)
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func allocTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeAllocJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAllocError(w http.ResponseWriter, status int, msg string) {
	writeAllocJSON(w, status, map[string]string{"error": msg})
}

func parseAllocUintParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

// ── GET /projects/{projectID}/allocation/config ────────────────────────────────

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, err := allocTenantID(r)
	if err != nil {
		writeAllocError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseAllocUintParam(r, "projectID")
	if err != nil {
		writeAllocError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}

	cfg, err := h.svc.GetBasis(r.Context(), tenantID, projectID)
	if err != nil {
		if errors.Is(err, ErrConfigNotFound) {
			writeAllocError(w, http.StatusNotFound, err.Error())
			return
		}
		writeAllocError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAllocJSON(w, http.StatusOK, cfg)
}

// ── PUT /projects/{projectID}/allocation/config ────────────────────────────────

type setConfigRequest struct {
	Basis AllocationBasis `json:"basis"`
}

func (h *Handler) setConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, err := allocTenantID(r)
	if err != nil {
		writeAllocError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseAllocUintParam(r, "projectID")
	if err != nil {
		writeAllocError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}

	var req setConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAllocError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}

	if err := h.svc.SetBasis(r.Context(), tenantID, projectID, req.Basis); err != nil {
		switch {
		case errors.Is(err, ErrInvalidBasis):
			writeAllocError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrProjectRequired):
			writeAllocError(w, http.StatusBadRequest, err.Error())
		default:
			writeAllocError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	cfg, err := h.svc.GetBasis(r.Context(), tenantID, projectID)
	if err != nil {
		writeAllocError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeAllocJSON(w, http.StatusOK, cfg)
}

// ── GET /projects/{projectID}/allocation/compute ──────────────────────────────

// allocationResultResponse adalah DTO untuk satu unit dalam response komputasi.
type allocationResultResponse struct {
	UnitID uint64               `json:"unit_id"`
	Direct breakdownResponse    `json:"direct"`
	Allocated breakdownResponse `json:"allocated"`
	Total breakdownResponse     `json:"total"`
	// SharePct (S9/R-9): porsi total unit thd grand total (%) — dihitung
	// backend (decimal), FE hanya menampilkan.
	SharePct string `json:"share_pct"`
}

type breakdownResponse struct {
	Land      string `json:"land"`
	Hard      string `json:"hard"`
	Soft      string `json:"soft"`
	Financing string `json:"financing"`
	Total     string `json:"total"`
}

func (h *Handler) compute(w http.ResponseWriter, r *http.Request) {
	tenantID, err := allocTenantID(r)
	if err != nil {
		writeAllocError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseAllocUintParam(r, "projectID")
	if err != nil {
		writeAllocError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}

	results, err := h.svc.ComputeAllocation(r.Context(), tenantID, projectID)
	if err != nil {
		switch {
		case errors.Is(err, ErrConfigNotFound):
			writeAllocError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrNoUnits):
			writeAllocError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrAllWeightsZero):
			writeAllocError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeAllocError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// S9/R-9: roll-up (grand total + share %) dihitung backend — FE display saja.
	grandDirect, grandAllocated, grandTotal := domain.Zero, domain.Zero, domain.Zero
	for _, res := range results {
		grandDirect = grandDirect.Add(res.Direct.Total())
		grandAllocated = grandAllocated.Add(res.Allocated.Total())
		grandTotal = grandTotal.Add(res.Total.Total())
	}
	dtos := make([]allocationResultResponse, len(results))
	for i, res := range results {
		sharePct := "0"
		if !grandTotal.IsZero() {
			sharePct = res.Total.Total().Decimal().
				Div(grandTotal.Decimal()).Mul(decimal.NewFromInt(100)).Round(2).String()
		}
		dtos[i] = allocationResultResponse{
			UnitID:    res.UnitID,
			Direct:    toBreakdownResponse(res.Direct),
			Allocated: toBreakdownResponse(res.Allocated),
			Total:     toBreakdownResponse(res.Total),
			SharePct:  sharePct,
		}
	}
	writeAllocJSON(w, http.StatusOK, map[string]any{
		"data": dtos,
		"totals": map[string]string{
			"direct":    grandDirect.String(),
			"allocated": grandAllocated.String(),
			"total":     grandTotal.String(),
		},
	})
}

func toBreakdownResponse(b domain.UnitCostBreakdown) breakdownResponse {
	return breakdownResponse{
		Land:      b.Land.String(),
		Hard:      b.Hard.String(),
		Soft:      b.Soft.String(),
		Financing: b.Financing.String(),
		Total:     b.Total().String(),
	}
}

// ── POST /projects/{projectID}/allocation/execute ─────────────────────────────
//
// Menjalankan compute alokasi dan menyimpan audit record eksekusi.
// Memerlukan role write (owner/accountant).

func (h *Handler) executeAllocation(w http.ResponseWriter, r *http.Request) {
	tenantID, err := allocTenantID(r)
	if err != nil {
		writeAllocError(w, http.StatusUnauthorized, err.Error())
		return
	}
	userID, ok := auth.UserIDFrom(r.Context())
	if !ok {
		writeAllocError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	projectID, err := parseAllocUintParam(r, "projectID")
	if err != nil {
		writeAllocError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}

	exec, err := h.svc.Execute(r.Context(), tenantID, projectID, userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrConfigNotFound):
			writeAllocError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrNoUnits):
			writeAllocError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrAllWeightsZero):
			writeAllocError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeAllocError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeAllocJSON(w, http.StatusCreated, exec)
}

// ── GET /projects/{projectID}/allocation/history ──────────────────────────────

func (h *Handler) listHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, err := allocTenantID(r)
	if err != nil {
		writeAllocError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseAllocUintParam(r, "projectID")
	if err != nil {
		writeAllocError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}

	history, err := h.svc.ListExecutions(r.Context(), tenantID, projectID)
	if err != nil {
		writeAllocError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeAllocJSON(w, http.StatusOK, map[string]any{"data": history})
}
