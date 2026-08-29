package salesorg

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/platform/auth"
)

type Handler struct {
	svc *Service
}

func NewHandler(db *gorm.DB) *Handler {
	repo := NewGORMRepository(db)
	return &Handler{svc: NewService(repo, repo)}
}

func (h *Handler) Mount(r chi.Router) {
	r.Route("/sales-teams", func(r chi.Router) {
		r.Get("/", h.listTeams)
		r.With(auth.RequireWrite()).Post("/", h.createTeam)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getTeam)
			r.With(auth.RequireWrite()).Put("/", h.updateTeam)
		})
	})
	r.Route("/sales-persons", func(r chi.Router) {
		r.Get("/", h.listPersons)
		r.With(auth.RequireWrite()).Post("/", h.createPerson)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getPerson)
			r.With(auth.RequireWrite()).Put("/", h.updatePerson)
		})
	})
}

// ── DTO ────────────────────────────────────────────────────────────────────────

type teamDTO struct {
	Code                string  `json:"code"`
	Name                string  `json:"name"`
	LeaderSalesPersonID *uint64 `json:"leader_sales_person_id"`
	IsActive            *bool   `json:"is_active"`
}

type personDTO struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	SalesTeamID *uint64 `json:"sales_team_id"`
	UserID      *uint64 `json:"user_id"`
	Phone       string  `json:"phone"`
	Email       string  `json:"email"`
	JoinDate    string  `json:"join_date"` // "2006-01-02"
	IsActive    *bool   `json:"is_active"`
}

// ── Team handlers ──────────────────────────────────────────────────────────────

func (h *Handler) listTeams(w http.ResponseWriter, r *http.Request) {
	tenantID, err := soTenantID(r)
	if err != nil {
		soErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	ts, err := h.svc.ListTeams(r.Context(), tenantID)
	if err != nil {
		soErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	soJSON(w, http.StatusOK, ts)
}

func (h *Handler) getTeam(w http.ResponseWriter, r *http.Request) {
	tenantID, err := soTenantID(r)
	if err != nil {
		soErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := soParam(r, "id")
	if err != nil {
		soErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	t, err := h.svc.GetTeam(r.Context(), tenantID, id)
	if err != nil {
		soServiceErr(w, err)
		return
	}
	soJSON(w, http.StatusOK, t)
}

func (h *Handler) createTeam(w http.ResponseWriter, r *http.Request) {
	tenantID, err := soTenantID(r)
	if err != nil {
		soErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto teamDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		soErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	t, err := h.svc.CreateTeam(r.Context(), tenantID, CreateTeamRequest{
		Code: dto.Code, Name: dto.Name, LeaderSalesPersonID: dto.LeaderSalesPersonID,
	})
	if err != nil {
		soServiceErr(w, err)
		return
	}
	soJSON(w, http.StatusCreated, t)
}

func (h *Handler) updateTeam(w http.ResponseWriter, r *http.Request) {
	tenantID, err := soTenantID(r)
	if err != nil {
		soErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := soParam(r, "id")
	if err != nil {
		soErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto teamDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		soErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	t, err := h.svc.UpdateTeam(r.Context(), tenantID, id, UpdateTeamRequest{
		Name: dto.Name, LeaderSalesPersonID: dto.LeaderSalesPersonID, IsActive: dto.IsActive,
	})
	if err != nil {
		soServiceErr(w, err)
		return
	}
	soJSON(w, http.StatusOK, t)
}

// ── Person handlers ────────────────────────────────────────────────────────────

func (h *Handler) listPersons(w http.ResponseWriter, r *http.Request) {
	tenantID, err := soTenantID(r)
	if err != nil {
		soErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	ps, err := h.svc.ListPersons(r.Context(), tenantID)
	if err != nil {
		soErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	soJSON(w, http.StatusOK, ps)
}

func (h *Handler) getPerson(w http.ResponseWriter, r *http.Request) {
	tenantID, err := soTenantID(r)
	if err != nil {
		soErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := soParam(r, "id")
	if err != nil {
		soErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	p, err := h.svc.GetPerson(r.Context(), tenantID, id)
	if err != nil {
		soServiceErr(w, err)
		return
	}
	soJSON(w, http.StatusOK, p)
}

func (h *Handler) createPerson(w http.ResponseWriter, r *http.Request) {
	tenantID, err := soTenantID(r)
	if err != nil {
		soErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto personDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		soErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	joinDate, err := parseOptionalDate(dto.JoinDate)
	if err != nil {
		soErr(w, http.StatusBadRequest, "join_date harus format YYYY-MM-DD")
		return
	}
	p, err := h.svc.CreatePerson(r.Context(), tenantID, CreatePersonRequest{
		Code: dto.Code, Name: dto.Name, SalesTeamID: dto.SalesTeamID, UserID: dto.UserID,
		Phone: dto.Phone, Email: dto.Email, JoinDate: joinDate,
	})
	if err != nil {
		soServiceErr(w, err)
		return
	}
	soJSON(w, http.StatusCreated, p)
}

func (h *Handler) updatePerson(w http.ResponseWriter, r *http.Request) {
	tenantID, err := soTenantID(r)
	if err != nil {
		soErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := soParam(r, "id")
	if err != nil {
		soErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto personDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		soErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	joinDate, err := parseOptionalDate(dto.JoinDate)
	if err != nil {
		soErr(w, http.StatusBadRequest, "join_date harus format YYYY-MM-DD")
		return
	}
	p, err := h.svc.UpdatePerson(r.Context(), tenantID, id, UpdatePersonRequest{
		Name: dto.Name, SalesTeamID: dto.SalesTeamID, Phone: dto.Phone, Email: dto.Email,
		JoinDate: joinDate, IsActive: dto.IsActive,
	})
	if err != nil {
		soServiceErr(w, err)
		return
	}
	soJSON(w, http.StatusOK, p)
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func soTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("konteks autentikasi tidak ada")
	}
	return id, nil
}

func soParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func parseOptionalDate(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func soJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func soErr(w http.ResponseWriter, status int, msg string) {
	soJSON(w, status, map[string]string{"error": msg})
}

func soServiceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTeamNotFound), errors.Is(err, ErrPersonNotFound):
		soErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrTeamCodeDup), errors.Is(err, ErrPersonCodeDup):
		soErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrCodeRequired), errors.Is(err, ErrNameRequired):
		soErr(w, http.StatusBadRequest, err.Error())
	default:
		soErr(w, http.StatusInternalServerError, err.Error())
	}
}
