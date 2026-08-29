package scheme

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/platform/auth"
)

// Handler menyajikan master Payment Scheme + Financing Source.
type Handler struct {
	svc *Service
}

// NewHandler membangun handler production (registry default).
func NewHandler(db *gorm.DB) *Handler {
	return &Handler{svc: NewService(NewGORMRepository(db), DefaultRegistry())}
}

// Service mengekspos service (dipakai wiring sale handler — registry tunggal).
func (h *Handler) Service() *Service { return h.svc }

func (h *Handler) Mount(r chi.Router) {
	r.Route("/payment-schemes", func(r chi.Router) {
		r.Get("/", h.listSchemes)
		r.With(auth.RequireWrite()).Post("/", h.createScheme)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getScheme)
			r.With(auth.RequireWrite()).Put("/", h.updateScheme)
		})
	})
	r.Route("/financing-sources", func(r chi.Router) {
		r.Get("/", h.listFinSources)
		r.With(auth.RequireWrite()).Post("/", h.createFinSource)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getFinSource)
			r.With(auth.RequireWrite()).Put("/", h.updateFinSource)
		})
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func schemeTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeSchemeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSchemeError(w http.ResponseWriter, status int, msg string) {
	writeSchemeJSON(w, status, map[string]string{"error": msg})
}

func parseSchemeUintParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func writeSchemeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSchemeNotFound), errors.Is(err, ErrFinSourceNotFound):
		writeSchemeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSchemeCodeDup), errors.Is(err, ErrFinSourceCodeDup):
		writeSchemeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidParams), errors.Is(err, ErrUnknownPolicyType),
		errors.Is(err, ErrSchemeCodeRequired), errors.Is(err, ErrFinSourceCodeReq),
		errors.Is(err, ErrFinSourceInvalidType), errors.Is(err, ErrInterestNotSupported):
		writeSchemeError(w, http.StatusBadRequest, err.Error())
	default:
		writeSchemeError(w, http.StatusInternalServerError, err.Error())
	}
}

// ── DTO ───────────────────────────────────────────────────────────────────────

type schemeDTO struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	PolicyType string  `json:"policy_type"`
	Params     *Params `json:"params"`
	IsActive   *bool   `json:"is_active"`
}

type finSourceDTO struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	IsActive *bool  `json:"is_active"`
}

// ── PaymentScheme handlers ────────────────────────────────────────────────────

func (h *Handler) listSchemes(w http.ResponseWriter, r *http.Request) {
	tenantID, err := schemeTenantID(r)
	if err != nil {
		writeSchemeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	out, err := h.svc.ListSchemes(r.Context(), tenantID)
	if err != nil {
		writeSchemeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSchemeJSON(w, http.StatusOK, out)
}

func (h *Handler) getScheme(w http.ResponseWriter, r *http.Request) {
	tenantID, err := schemeTenantID(r)
	if err != nil {
		writeSchemeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSchemeUintParam(r, "id")
	if err != nil {
		writeSchemeError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	out, err := h.svc.GetScheme(r.Context(), tenantID, id)
	if err != nil {
		writeSchemeServiceError(w, err)
		return
	}
	writeSchemeJSON(w, http.StatusOK, out)
}

func (h *Handler) createScheme(w http.ResponseWriter, r *http.Request) {
	tenantID, err := schemeTenantID(r)
	if err != nil {
		writeSchemeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto schemeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeSchemeError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	var params Params
	if dto.Params != nil {
		params = *dto.Params
	}
	out, err := h.svc.CreateScheme(r.Context(), tenantID, CreateSchemeRequest{
		Code:       dto.Code,
		Name:       dto.Name,
		PolicyType: PolicyType(dto.PolicyType),
		Params:     params,
	})
	if err != nil {
		writeSchemeServiceError(w, err)
		return
	}
	writeSchemeJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateScheme(w http.ResponseWriter, r *http.Request) {
	tenantID, err := schemeTenantID(r)
	if err != nil {
		writeSchemeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSchemeUintParam(r, "id")
	if err != nil {
		writeSchemeError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto schemeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeSchemeError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	out, err := h.svc.UpdateScheme(r.Context(), tenantID, id, UpdateSchemeRequest{
		Name:     dto.Name,
		Params:   dto.Params,
		IsActive: dto.IsActive,
	})
	if err != nil {
		writeSchemeServiceError(w, err)
		return
	}
	writeSchemeJSON(w, http.StatusOK, out)
}

// ── FinancingSource handlers ──────────────────────────────────────────────────

func (h *Handler) listFinSources(w http.ResponseWriter, r *http.Request) {
	tenantID, err := schemeTenantID(r)
	if err != nil {
		writeSchemeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	out, err := h.svc.ListFinancingSources(r.Context(), tenantID)
	if err != nil {
		writeSchemeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSchemeJSON(w, http.StatusOK, out)
}

func (h *Handler) getFinSource(w http.ResponseWriter, r *http.Request) {
	tenantID, err := schemeTenantID(r)
	if err != nil {
		writeSchemeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSchemeUintParam(r, "id")
	if err != nil {
		writeSchemeError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	out, err := h.svc.GetFinancingSource(r.Context(), tenantID, id)
	if err != nil {
		writeSchemeServiceError(w, err)
		return
	}
	writeSchemeJSON(w, http.StatusOK, out)
}

func (h *Handler) createFinSource(w http.ResponseWriter, r *http.Request) {
	tenantID, err := schemeTenantID(r)
	if err != nil {
		writeSchemeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto finSourceDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeSchemeError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	out, err := h.svc.CreateFinancingSource(r.Context(), tenantID, CreateFinancingSourceRequest{
		Code: dto.Code,
		Name: dto.Name,
		Type: FinancingSourceType(dto.Type),
	})
	if err != nil {
		writeSchemeServiceError(w, err)
		return
	}
	writeSchemeJSON(w, http.StatusCreated, out)
}

func (h *Handler) updateFinSource(w http.ResponseWriter, r *http.Request) {
	tenantID, err := schemeTenantID(r)
	if err != nil {
		writeSchemeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSchemeUintParam(r, "id")
	if err != nil {
		writeSchemeError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto finSourceDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeSchemeError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	out, err := h.svc.UpdateFinancingSource(r.Context(), tenantID, id, UpdateFinancingSourceRequest{
		Name:     dto.Name,
		Type:     FinancingSourceType(dto.Type),
		IsActive: dto.IsActive,
	})
	if err != nil {
		writeSchemeServiceError(w, err)
		return
	}
	writeSchemeJSON(w, http.StatusOK, out)
}
