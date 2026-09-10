package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// Handler wires the project HTTP routes.
type Handler struct {
	svc *Service
}

// NewHandler constructs the production project handler backed by GORM.
func NewHandler(db *gorm.DB) *Handler {
	repo := NewGORMRepository(db)
	svc := NewService(repo, repo, repo)
	svc.SetProductTypeStore(repo) // UAT Batch 2 §2 — katalog produk
	return &Handler{svc: svc}
}

// Svc exposes the underlying service for cross-package wiring (mis. SetApprovalGate).
func (h *Handler) Svc() *Service { return h.svc }

// Mount registers all project routes on the provided router.
// The router must already have auth.Middleware applied upstream.
func (h *Handler) Mount(r chi.Router) {
	// UAT Batch 2 §2 — master Product Catalog (rumah/ruko/kavling/…).
	r.Route("/product-types", func(r chi.Router) {
		r.Get("/", h.listProductTypes)
		r.With(auth.RequireWrite()).Post("/", h.createProductType)
		r.With(auth.RequireWrite()).Patch("/{productTypeID}", h.updateProductType)
	})
	r.Route("/projects", func(r chi.Router) {
		r.Get("/", h.listProjects)
		r.With(auth.RequireWrite()).Post("/", h.createProject)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getProject)
			r.With(auth.RequireWrite()).Post("/transition", h.transitionProject)
			r.With(auth.RequireWrite()).Put("/tax-category", h.updateTaxCategory)
			r.Get("/transitions", h.listProjectTransitions)

			r.Route("/phases", func(r chi.Router) {
				r.Get("/", h.listPhases)
				r.With(auth.RequireWrite()).Post("/", h.createPhase)
				r.Route("/{phaseID}", func(r chi.Router) {
					r.Get("/", h.getPhase)
					r.With(auth.RequireWrite()).Post("/transition", h.transitionPhase)
					r.Get("/units", h.listUnitsByPhase)
				})
			})

			r.Route("/units", func(r chi.Router) {
				r.Get("/", h.listUnitsByProject)
				r.With(auth.RequireWrite()).Post("/", h.createUnit)
				// UAT Batch 2 §4 — wizard pembuatan unit per blok (atomik).
				r.With(auth.RequireWrite()).Post("/bulk", h.bulkCreateUnits)
			})
		})
	})

	r.Route("/units/{id}", func(r chi.Router) {
		r.Get("/", h.getUnit)
		r.Get("/transitions", h.listUnitTransitions)
		r.With(auth.RequireWrite()).Post("/transition", h.transitionUnit)
		// LT-2 (kelebihan-tanah-final-architecture §B.5): koreksi eksplisit-admin
		// luas tanah, terpisah dari status transition.
		r.With(auth.RequireWrite()).Patch("/land-area", h.updateUnitLandArea)
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func parseUintParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func projectTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeProjectJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProjectError(w http.ResponseWriter, status int, msg string) {
	writeProjectJSON(w, status, map[string]string{"error": msg})
}

func parseProjectID(r *http.Request) (uint64, error) {
	return parseUintParam(r, "id")
}

func parseProjectUintParam(r *http.Request, name string) (uint64, error) {
	return parseUintParam(r, name)
}

// isDomainProjectError maps domain errors to 4xx responses.
func isDomainProjectError(err error) bool {
	return errors.Is(err, ErrProjectInvalidTransition) ||
		errors.Is(err, ErrPhaseInvalidTransition) ||
		errors.Is(err, ErrUnitInvalidTransition) ||
		errors.Is(err, ErrUnitCodeDuplicate) ||
		errors.Is(err, ErrListPriceFractional) ||
		errors.Is(err, ErrSalePriceFractional) ||
		errors.Is(err, ErrUnknownTransitionEvent) ||
		errors.Is(err, ErrEventTransitionMismatch) ||
		errors.Is(err, ErrPostBASTCancellation) ||
		// Master produk: penolakan aturan, bukan kegagalan server. Tanpa ini
		// pengguna hanya melihat 500 dan tidak pernah tahu apa yang harus
		// diperbaiki.
		errors.Is(err, ErrUnitTypeUnknown) ||
		errors.Is(err, ErrUnitTypeNotProperty) ||
		errors.Is(err, ErrLandAreaNegative)
}

// isApprovalRequired reports whether err is the opt-in approval gate rejection.
func isApprovalRequired(err error) bool {
	return errors.Is(err, ErrApprovalRequired)
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrProjectNotFound) ||
		errors.Is(err, ErrPhaseNotFound) ||
		errors.Is(err, ErrUnitNotFound)
}

// ── Project handlers ──────────────────────────────────────────────────────────

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	ps, err := h.svc.ListProjects(r.Context(), tenantID)
	if err != nil {
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, ps)
}

type createProjectDTO struct {
	Name      string  `json:"name"`
	LandArea  string  `json:"land_area"`
	Notes     string  `json:"notes"`
	StartDate *string `json:"start_date"`
	// TaxCategory: subsidi|komersial (kosong = komersial). Penentu tarif pajak
	// proyek (Increment 4).
	TaxCategory string `json:"tax_category"`
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto createProjectDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if dto.Name == "" {
		writeProjectError(w, http.StatusBadRequest, "name required")
		return
	}
	var landArea decimal.Decimal
	if dto.LandArea != "" {
		landArea, err = decimal.NewFromString(dto.LandArea)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "land_area harus angka")
			return
		}
	}
	var startDate *time.Time
	if dto.StartDate != nil && *dto.StartDate != "" {
		t, err := time.Parse("2006-01-02", *dto.StartDate)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "start_date harus YYYY-MM-DD")
			return
		}
		startDate = &t
	}
	p, err := h.svc.CreateProject(r.Context(), tenantID, CreateProjectRequest{
		Name:        dto.Name,
		LandArea:    landArea,
		Notes:       dto.Notes,
		StartDate:   startDate,
		TaxCategory: domain.TaxCategory(dto.TaxCategory),
	})
	if err != nil {
		if errors.Is(err, ErrInvalidTaxCategory) {
			writeProjectError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusCreated, p)
}

// updateTaxCategory — PUT /projects/{id}/tax-category (Increment 4).
// Perubahan hanya berefek pada akrual pajak BERIKUTNYA (append-only).
func (h *Handler) updateTaxCategory(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		TaxCategory string `json:"tax_category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	p, err := h.svc.UpdateTaxCategory(r.Context(), tenantID, id, domain.TaxCategory(dto.TaxCategory))
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidTaxCategory):
			writeProjectError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrProjectNotFound):
			writeProjectError(w, http.StatusNotFound, err.Error())
		default:
			writeProjectError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeProjectJSON(w, http.StatusOK, p)
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := h.svc.GetProject(r.Context(), tenantID, id)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, p)
}

type transitionDTO struct {
	Status string `json:"status"`
}

func (h *Handler) transitionProject(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto transitionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	next := ProjectStatus(dto.Status)
	if !next.Valid() {
		writeProjectError(w, http.StatusBadRequest, "status tidak dikenal")
		return
	}
	p, err := h.svc.TransitionProject(r.Context(), tenantID, id, next)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainProjectError(err) {
			writeProjectError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, p)
}

// ── Phase handlers ────────────────────────────────────────────────────────────

type createPhaseDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	TargetUnits int    `json:"target_units"`
}

func (h *Handler) createPhase(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var dto createPhaseDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if dto.Name == "" {
		writeProjectError(w, http.StatusBadRequest, "name required")
		return
	}
	ph, err := h.svc.CreatePhase(r.Context(), tenantID, CreatePhaseRequest{
		ProjectID:   projectID,
		Name:        dto.Name,
		Description: dto.Description,
		TargetUnits: dto.TargetUnits,
	})
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusCreated, ph)
}

func (h *Handler) listPhases(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	phs, err := h.svc.ListPhasesByProject(r.Context(), tenantID, projectID)
	if err != nil {
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, phs)
}

func (h *Handler) getPhase(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	phaseID, err := parseProjectUintParam(r, "phaseID")
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid phase id")
		return
	}
	ph, err := h.svc.GetPhase(r.Context(), tenantID, phaseID)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, ph)
}

func (h *Handler) transitionPhase(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	phaseID, err := parseProjectUintParam(r, "phaseID")
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid phase id")
		return
	}
	var dto transitionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	next := PhaseStatus(dto.Status)
	if !next.Valid() {
		writeProjectError(w, http.StatusBadRequest, "status fase tidak dikenal")
		return
	}
	ph, err := h.svc.TransitionPhase(r.Context(), tenantID, phaseID, next)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainProjectError(err) {
			writeProjectError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, ph)
}

// ── Unit handlers ─────────────────────────────────────────────────────────────

type createUnitDTO struct {
	Code         string  `json:"code"`
	UnitType     string  `json:"unit_type"`
	TypeLabel    *string `json:"type_label,omitempty"`
	SaleableArea string  `json:"saleable_area"`
	// LandArea (LT-2): opsional saat create — kosong berarti diisi admin
	// belakangan lewat PATCH /units/{id}/land-area.
	LandArea  string  `json:"land_area,omitempty"`
	ListPrice string  `json:"list_price"`
	PhaseID   *uint64 `json:"phase_id,omitempty"`
}

// bulkCreateUnitsDTO (UAT Batch 2 §4): wizard pembuatan unit per blok.
type bulkCreateUnitsDTO struct {
	Block        string  `json:"block"`
	UnitStart    int     `json:"unit_start"`
	UnitEnd      int     `json:"unit_end"`
	UnitType     string  `json:"unit_type"`
	TypeLabel    *string `json:"type_label,omitempty"`
	SaleableArea string  `json:"saleable_area"`
	// LandArea: luas tanah per unit, diterapkan sama ke seluruh unit dalam blok.
	LandArea  string  `json:"land_area,omitempty"`
	ListPrice string  `json:"list_price"`
	PhaseID   *uint64 `json:"phase_id,omitempty"`
}

type transitionUnitDTO struct {
	Status    string  `json:"status"`
	Event     string  `json:"event,omitempty"`      // typed trigger; kosong → manual
	EventDate *string `json:"event_date,omitempty"` // tanggal kejadian bisnis (YYYY-MM-DD); kosong → sekarang
	Notes     string  `json:"notes,omitempty"`
	BuyerRef  *string `json:"buyer_ref,omitempty"`
	SaleDate  *string `json:"sale_date,omitempty"`
	SalePrice *string `json:"sale_price,omitempty"`
}

func (h *Handler) createUnit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var dto createUnitDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if dto.Code == "" || dto.UnitType == "" {
		writeProjectError(w, http.StatusBadRequest, "code dan unit_type wajib diisi")
		return
	}
	var area decimal.Decimal
	if dto.SaleableArea != "" {
		area, err = decimal.NewFromString(dto.SaleableArea)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "saleable_area harus angka")
			return
		}
	}
	var landArea decimal.Decimal
	if dto.LandArea != "" {
		landArea, err = decimal.NewFromString(dto.LandArea)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "land_area harus angka")
			return
		}
	}
	lp, err := domain.NewMoney(dto.ListPrice)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "list_price tidak valid: "+err.Error())
		return
	}
	u, err := h.svc.CreateUnit(r.Context(), tenantID, CreateUnitRequest{
		ProjectID:    projectID,
		PhaseID:      dto.PhaseID,
		Code:         dto.Code,
		UnitType:     dto.UnitType,
		TypeLabel:    dto.TypeLabel,
		SaleableArea: area,
		LandArea:     landArea,
		ListPrice:    lp,
	})
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainProjectError(err) {
			writeProjectError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusCreated, u)
}

// ── Product Catalog handlers (UAT Batch 2 §2) ────────────────────────────────

func (h *Handler) listProductTypes(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	out, err := h.svc.ListProductTypes(r.Context(), tenantID)
	if err != nil {
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, out)
}

type productTypeDTO struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	Category           string `json:"category"` // property | non_property
	RevenueAccountCode string `json:"revenue_account_code,omitempty"`
	// TaxCategory (rule klien UAT #3): subsidi|komersial, opsional. Kosong =
	// produk ini ikut projects.tax_category (jalur legacy) — lihat product_type.go.
	TaxCategory string `json:"tax_category,omitempty"`
}

func (h *Handler) createProductType(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto productTypeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	var taxCategory *domain.TaxCategory
	if strings.TrimSpace(dto.TaxCategory) != "" {
		tc := domain.TaxCategory(strings.TrimSpace(dto.TaxCategory))
		if !tc.Valid() {
			writeProjectError(w, http.StatusBadRequest, "tax_category tidak valid: gunakan subsidi atau komersial")
			return
		}
		taxCategory = &tc
	}
	pt, err := h.svc.CreateProductType(r.Context(), tenantID, &ProductType{
		Code:               dto.Code,
		Name:               dto.Name,
		Category:           ProductCategory(dto.Category),
		RevenueAccountCode: dto.RevenueAccountCode,
		TaxCategory:        taxCategory,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrProductTypeDuplicate):
			writeProjectError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrProductTypeInvalid):
			writeProjectError(w, http.StatusBadRequest, err.Error())
		default:
			writeProjectError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeProjectJSON(w, http.StatusCreated, pt)
}

type updateProductTypeDTO struct {
	Name               *string `json:"name,omitempty"`
	RevenueAccountCode *string `json:"revenue_account_code,omitempty"`
	IsActive           *bool   `json:"is_active,omitempty"`
	// TaxCategory: pointer-ke-pointer semantics via string biasa — "" mengosongkan
	// (kembali ke jalur legacy projects.tax_category), nil (field absen) = tidak diubah.
	TaxCategory *string `json:"tax_category,omitempty"`
}

func (h *Handler) updateProductType(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "productTypeID"), 10, 64)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "productTypeID tidak valid")
		return
	}
	var dto updateProductTypeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	pt, err := h.svc.UpdateProductType(r.Context(), tenantID, id, dto.Name, dto.RevenueAccountCode, dto.IsActive, dto.TaxCategory)
	if err != nil {
		switch {
		case errors.Is(err, ErrProductTypeNotFound):
			writeProjectError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrProductTypeInvalid):
			writeProjectError(w, http.StatusBadRequest, err.Error())
		default:
			writeProjectError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeProjectJSON(w, http.StatusOK, pt)
}

// bulkCreateUnits — POST /projects/{id}/units/bulk (UAT Batch 2 §4).
func (h *Handler) bulkCreateUnits(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var dto bulkCreateUnitsDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	var area decimal.Decimal
	if dto.SaleableArea != "" {
		area, err = decimal.NewFromString(dto.SaleableArea)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "saleable_area harus angka")
			return
		}
	}
	var landArea decimal.Decimal
	if dto.LandArea != "" {
		landArea, err = decimal.NewFromString(dto.LandArea)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "land_area harus angka")
			return
		}
	}
	lp, err := domain.NewMoney(dto.ListPrice)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "list_price tidak valid: "+err.Error())
		return
	}
	units, err := h.svc.BulkCreateUnits(r.Context(), tenantID, BulkCreateUnitsRequest{
		ProjectID:    projectID,
		PhaseID:      dto.PhaseID,
		Block:        dto.Block,
		UnitStart:    dto.UnitStart,
		UnitEnd:      dto.UnitEnd,
		UnitType:     dto.UnitType,
		TypeLabel:    dto.TypeLabel,
		SaleableArea: area,
		LandArea:     landArea,
		ListPrice:    lp,
	})
	if err != nil {
		switch {
		case isNotFound(err):
			writeProjectError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrBulkDuplicateCode):
			writeProjectError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrBulkBlockRequired), errors.Is(err, ErrBulkRangeInvalid),
			errors.Is(err, ErrBulkTooMany), errors.Is(err, ErrListPriceFractional):
			writeProjectError(w, http.StatusBadRequest, err.Error())
		default:
			writeProjectError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeProjectJSON(w, http.StatusCreated, map[string]any{
		"created": len(units),
		"units":   units,
	})
}

func (h *Handler) listUnitsByProject(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	us, err := h.svc.ListUnitsByProject(r.Context(), tenantID, projectID)
	if err != nil {
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, us)
}

func (h *Handler) listUnitsByPhase(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	phaseID, err := parseProjectUintParam(r, "phaseID")
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid phase id")
		return
	}
	us, err := h.svc.ListUnitsByPhase(r.Context(), tenantID, phaseID)
	if err != nil {
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, us)
}

func (h *Handler) getUnit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid id")
		return
	}
	u, err := h.svc.GetUnit(r.Context(), tenantID, id)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, u)
}

func (h *Handler) transitionUnit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto transitionUnitDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	next := UnitStatus(dto.Status)
	if !next.Valid() {
		writeProjectError(w, http.StatusBadRequest, "status unit tidak dikenal")
		return
	}

	opts := UpdateUnitOpts{BuyerRef: dto.BuyerRef}
	if dto.SaleDate != nil && *dto.SaleDate != "" {
		t, err := time.Parse("2006-01-02", *dto.SaleDate)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "sale_date harus YYYY-MM-DD")
			return
		}
		opts.SaleDate = &t
	}
	if dto.SalePrice != nil && *dto.SalePrice != "" {
		d, err := decimal.NewFromString(*dto.SalePrice)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "sale_price harus angka")
			return
		}
		opts.SalePrice = &d
	}

	req := TransitionRequest{Next: next, Event: TransitionEvent(dto.Event), Notes: dto.Notes, UpdateUnitOpts: opts}
	if dto.EventDate != nil && *dto.EventDate != "" {
		t, err := time.Parse("2006-01-02", *dto.EventDate)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "event_date harus YYYY-MM-DD")
			return
		}
		req.EventDate = &t
	}
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		req.ActorID = &uid
	}

	u, err := h.svc.Transition(r.Context(), tenantID, id, req)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		if isApprovalRequired(err) {
			writeProjectError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if errors.Is(err, ErrUnitTransitionConflict) {
			writeProjectError(w, http.StatusConflict, err.Error())
			return
		}
		if isDomainProjectError(err) {
			writeProjectError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, u)
}

type updateUnitLandAreaDTO struct {
	LandArea string `json:"land_area"`
}

// updateUnitLandArea (LT-2): koreksi eksplisit-admin luas tanah unit, terpisah
// dari transitionUnit — tidak menyentuh status/lifecycle unit sama sekali.
func (h *Handler) updateUnitLandArea(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto updateUnitLandAreaDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	landArea, err := decimal.NewFromString(dto.LandArea)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "land_area harus angka")
		return
	}
	u, err := h.svc.UpdateUnitLandArea(r.Context(), tenantID, id, landArea)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainProjectError(err) {
			writeProjectError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, u)
}

func (h *Handler) listUnitTransitions(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ts, err := h.svc.ListTransitions(r.Context(), tenantID, id)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ts == nil {
		ts = []*UnitStatusTransition{}
	}
	writeProjectJSON(w, http.StatusOK, ts)
}

// listProjectTransitions — Timeline workspace: transisi terbaru lintas unit proyek.
func (h *Handler) listProjectTransitions(w http.ResponseWriter, r *http.Request) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseProjectID(r)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "invalid id")
		return
	}
	limit := 0
	if q := r.URL.Query().Get("limit"); q != "" {
		limit, _ = strconv.Atoi(q)
	}
	ts, err := h.svc.ListProjectTransitions(r.Context(), tenantID, id, limit)
	if err != nil {
		if isNotFound(err) {
			writeProjectError(w, http.StatusNotFound, err.Error())
			return
		}
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ts == nil {
		ts = []*UnitStatusTransition{}
	}
	writeProjectJSON(w, http.StatusOK, ts)
}
