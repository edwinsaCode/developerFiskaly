package customer

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/platform/auth"
)

type Handler struct {
	svc *Service
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{svc: NewService(NewGORMRepository(db))}
}

func (h *Handler) Mount(r chi.Router) {
	r.Route("/customers", func(r chi.Router) {
		r.Get("/", h.list)
		r.With(auth.RequireSalesWrite()).Post("/", h.create)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.get)
			r.With(auth.RequireSalesWrite()).Put("/", h.update)
		})
	})
}

// ── DTO ────────────────────────────────────────────────────────────────────────

type customerDTO struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Segment  string `json:"segment"`
	IDNumber string `json:"id_number"`
	NPWP     string `json:"npwp"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	Address  string `json:"address"`
	IsActive *bool  `json:"is_active"`
}

// ── Handlers ───────────────────────────────────────────────────────────────────

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, err := customerTenantID(r)
	if err != nil {
		writeCustomerError(w, http.StatusUnauthorized, err.Error())
		return
	}
	cs, err := h.svc.List(r.Context(), tenantID)
	if err != nil {
		writeCustomerError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCustomerJSON(w, http.StatusOK, cs)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tenantID, err := customerTenantID(r)
	if err != nil {
		writeCustomerError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeCustomerError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	c, err := h.svc.Get(r.Context(), tenantID, id)
	if err != nil {
		writeCustomerServiceError(w, err)
		return
	}
	writeCustomerJSON(w, http.StatusOK, c)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tenantID, err := customerTenantID(r)
	if err != nil {
		writeCustomerError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto customerDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeCustomerError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	var createdBy *uint64
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		createdBy = &uid
	}
	c, err := h.svc.Create(r.Context(), tenantID, CreateCustomerRequest{
		Code:      dto.Code,
		Name:      dto.Name,
		Type:      CustomerType(dto.Type),
		Segment:   dto.Segment,
		IDNumber:  dto.IDNumber,
		NPWP:      dto.NPWP,
		Phone:     dto.Phone,
		Email:     dto.Email,
		Address:   dto.Address,
		CreatedBy: createdBy,
	})
	if err != nil {
		writeCustomerServiceError(w, err)
		return
	}
	writeCustomerJSON(w, http.StatusCreated, c)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	tenantID, err := customerTenantID(r)
	if err != nil {
		writeCustomerError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeCustomerError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto customerDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeCustomerError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	c, err := h.svc.Update(r.Context(), tenantID, id, UpdateCustomerRequest{
		Name:     dto.Name,
		Type:     CustomerType(dto.Type),
		Segment:  dto.Segment,
		IDNumber: dto.IDNumber,
		NPWP:     dto.NPWP,
		Phone:    dto.Phone,
		Email:    dto.Email,
		Address:  dto.Address,
		IsActive: dto.IsActive,
	})
	if err != nil {
		writeCustomerServiceError(w, err)
		return
	}
	writeCustomerJSON(w, http.StatusOK, c)
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func customerTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("konteks autentikasi tidak ada")
	}
	return id, nil
}

func parseUintParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func writeCustomerJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeCustomerError(w http.ResponseWriter, status int, msg string) {
	writeCustomerJSON(w, status, map[string]string{"error": msg})
}

func writeCustomerServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCustomerNotFound):
		writeCustomerError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrCustomerCodeDuplicate):
		writeCustomerError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrCustomerCodeRequired),
		errors.Is(err, ErrCustomerNameRequired),
		errors.Is(err, ErrCustomerTypeInvalid):
		writeCustomerError(w, http.StatusBadRequest, err.Error())
	default:
		writeCustomerError(w, http.StatusInternalServerError, err.Error())
	}
}
