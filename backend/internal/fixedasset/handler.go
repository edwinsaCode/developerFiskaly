package fixedasset

// HTTP untuk Fixed Asset Register.
//
// Perolehan aset (create) TIDAK punya endpoint di sini — itu lewat toggle
// "Jenis Pembelian" di /expenses (internal/cost/expense_handler.go), satu
// pintu masuk pengeluaran yang sudah ada. Paket ini hanya menyediakan:
//  1. master Kategori Aset Tetap (CRUD admin),
//  2. Register untuk dibaca (daftar + detail, dengan akumulasi & nilai buku
//     yang diturunkan, bukan disimpan), dan
//  3. eksekusi penyusutan bulanan (RunDepreciation).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/auth"
)

// Handler wires internal/fixedasset HTTP routes.
type Handler struct {
	svc        *Service
	categories *CategoryService
	repo       *GORMRepository
}

// NewHandler constructs the production fixed asset handler backed by GORM.
func NewHandler(db *gorm.DB) *Handler {
	repo := NewGORMRepository(db)
	svc := NewService(NewGORMTxRunner(db), repo, repo)
	return &Handler{svc: svc, categories: NewCategoryService(db), repo: repo}
}

// Service exposes the underlying *Service so other packages (cost, via
// SetFixedAssetService) can wire the toggle without this package importing
// them back.
func (h *Handler) Service() *Service { return h.svc }

func (h *Handler) Mount(r chi.Router) {
	r.Route("/fixed-asset-categories", func(r chi.Router) {
		r.Get("/", h.listCategories)
		r.With(auth.RequireWrite()).Post("/", h.createCategory)
		r.With(auth.RequireWrite()).Patch("/{id}", h.updateCategory)
	})

	r.Route("/fixed-assets", func(r chi.Router) {
		r.Get("/", h.listAssets)
		r.Get("/{id}", h.getAsset)
		r.Get("/{id}/depreciation-lines", h.listDepreciationLines)
	})

	r.With(auth.RequireWrite()).Post("/fixed-assets/depreciation-runs", h.runDepreciation)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func faTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeFAJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFAError(w http.ResponseWriter, status int, msg string) {
	writeFAJSON(w, status, map[string]string{"error": msg})
}

func parseFAUintParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func actorID(r *http.Request) *uint64 {
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		return &uid
	}
	return nil
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCategoryNotFound), errors.Is(err, ErrAssetNotFound):
		writeFAError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrCategoryDuplicate), errors.Is(err, ErrDepreciationAlreadyPosted):
		writeFAError(w, http.StatusConflict, err.Error())
	default:
		writeFAError(w, http.StatusBadRequest, err.Error())
	}
}

// ── Kategori ─────────────────────────────────────────────────────────────────

func (h *Handler) listCategories(w http.ResponseWriter, r *http.Request) {
	tenantID, err := faTenantID(r)
	if err != nil {
		writeFAError(w, http.StatusUnauthorized, err.Error())
		return
	}
	cats, err := h.categories.List(r.Context(), tenantID)
	if err != nil {
		writeFAError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeFAJSON(w, http.StatusOK, cats)
}

type categoryDTO struct {
	Code                               string `json:"code"`
	Name                               string `json:"name"`
	AssetAccountCode                   string `json:"asset_account_code"`
	AccumulatedDepreciationAccountCode string `json:"accumulated_depreciation_account_code"`
	DepreciationExpenseAccountCode     string `json:"depreciation_expense_account_code"`
	DefaultUsefulLifeMonths            uint   `json:"default_useful_life_months"`
}

func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) {
	tenantID, err := faTenantID(r)
	if err != nil {
		writeFAError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto categoryDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeFAError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	cat, err := h.categories.Create(r.Context(), tenantID, CategoryInput{
		Code: dto.Code, Name: dto.Name,
		AssetAccountCode:                   dto.AssetAccountCode,
		AccumulatedDepreciationAccountCode: dto.AccumulatedDepreciationAccountCode,
		DepreciationExpenseAccountCode:     dto.DepreciationExpenseAccountCode,
		DefaultUsefulLifeMonths:            dto.DefaultUsefulLifeMonths,
	}, actorID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeFAJSON(w, http.StatusCreated, cat)
}

func (h *Handler) updateCategory(w http.ResponseWriter, r *http.Request) {
	tenantID, err := faTenantID(r)
	if err != nil {
		writeFAError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseFAUintParam(r, "id")
	if err != nil {
		writeFAError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var body struct {
		Name                               *string `json:"name"`
		AssetAccountCode                   *string `json:"asset_account_code"`
		AccumulatedDepreciationAccountCode *string `json:"accumulated_depreciation_account_code"`
		DepreciationExpenseAccountCode     *string `json:"depreciation_expense_account_code"`
		DefaultUsefulLifeMonths            *uint   `json:"default_useful_life_months"`
		IsActive                           *bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeFAError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	cat, err := h.categories.Update(r.Context(), tenantID, id, CategoryUpdate{
		Name:                               body.Name,
		AssetAccountCode:                   body.AssetAccountCode,
		AccumulatedDepreciationAccountCode: body.AccumulatedDepreciationAccountCode,
		DepreciationExpenseAccountCode:     body.DepreciationExpenseAccountCode,
		DefaultUsefulLifeMonths:            body.DefaultUsefulLifeMonths,
		IsActive:                           body.IsActive,
	}, actorID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeFAJSON(w, http.StatusOK, cat)
}

// ── Register ─────────────────────────────────────────────────────────────────

// assetView menambahkan akumulasi penyusutan + nilai buku yang DITURUNKAN
// (SUM garis penyusutan) ke setiap respons — bukan kolom yang disimpan
// (lihat catatan di model.go dan migrasi 000087).
type assetView struct {
	*FixedAsset
	AccumulatedDepreciation domain.Money `json:"accumulated_depreciation"`
	BookValue               domain.Money `json:"book_value"`
}

func (h *Handler) listAssets(w http.ResponseWriter, r *http.Request) {
	tenantID, err := faTenantID(r)
	if err != nil {
		writeFAError(w, http.StatusUnauthorized, err.Error())
		return
	}
	assets, err := h.repo.ListAssets(r.Context(), tenantID)
	if err != nil {
		writeFAError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]assetView, 0, len(assets))
	for _, a := range assets {
		sum, err := h.repo.SumDepreciation(r.Context(), tenantID, a.ID)
		if err != nil {
			writeFAError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, assetView{FixedAsset: a, AccumulatedDepreciation: sum, BookValue: a.AcquisitionCost.Sub(sum)})
	}
	writeFAJSON(w, http.StatusOK, out)
}

func (h *Handler) getAsset(w http.ResponseWriter, r *http.Request) {
	tenantID, err := faTenantID(r)
	if err != nil {
		writeFAError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseFAUintParam(r, "id")
	if err != nil {
		writeFAError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	a, err := h.repo.GetAsset(r.Context(), tenantID, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	sum, err := h.repo.SumDepreciation(r.Context(), tenantID, a.ID)
	if err != nil {
		writeFAError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeFAJSON(w, http.StatusOK, assetView{FixedAsset: a, AccumulatedDepreciation: sum, BookValue: a.AcquisitionCost.Sub(sum)})
}

func (h *Handler) listDepreciationLines(w http.ResponseWriter, r *http.Request) {
	tenantID, err := faTenantID(r)
	if err != nil {
		writeFAError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseFAUintParam(r, "id")
	if err != nil {
		writeFAError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	lines, err := h.repo.ListDepreciationLines(r.Context(), tenantID, id)
	if err != nil {
		writeFAError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeFAJSON(w, http.StatusOK, lines)
}

// ── Penyusutan ───────────────────────────────────────────────────────────────

type depreciationRunDTO struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}

func (h *Handler) runDepreciation(w http.ResponseWriter, r *http.Request) {
	tenantID, err := faTenantID(r)
	if err != nil {
		writeFAError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto depreciationRunDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeFAError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	result, err := h.svc.RunDepreciation(r.Context(), tenantID, dto.Year, dto.Month)
	if err != nil {
		if errors.Is(err, ErrInvalidPeriod) {
			writeFAError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, ledger.ErrPeriodClosed) {
			writeFAError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeFAError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeFAJSON(w, http.StatusOK, result)
}
