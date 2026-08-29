package tenant

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// Handler wires the tenant/auth HTTP routes.
type Handler struct {
	svc *Service
}

// NewHandler constructs the tenant HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// MountPublic registers routes that do NOT require authentication.
// Call this before mounting protected routes.
func (h *Handler) MountPublic(r chi.Router) {
	r.Post("/auth/register", h.register)
	r.Post("/auth/login", h.login)
}

// MountProtected registers routes that REQUIRE a valid JWT.
// These are already under auth.Middleware in the router.
func (h *Handler) MountProtected(r chi.Router) {
	// Identitas diri boleh dibaca role apa pun — termasuk marketing dan viewer.
	// Tanpa ini app shell tidak punya cara jujur untuk tahu siapa yang login.
	r.Get("/auth/me", h.me)
	r.Get("/users", h.listUsers)
	// Only owners may create/edit users; service enforces this too (defense-in-depth).
	r.With(auth.RequireRole("owner")).Post("/users", h.createUser)
	r.With(auth.RequireRole("owner")).Patch("/users/{userID}", h.updateUser)
}

// ── Public handlers ───────────────────────────────────────────────────────────

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	resp, err := h.svc.Register(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if !isClientError(err) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	resp, err := h.svc.Login(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── Protected handlers (require JWT in context) ────────────────────────────────

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth context")
		return
	}
	users, err := h.svc.ListUsers(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth context")
		return
	}
	userID, ok := auth.UserIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth context")
		return
	}
	me, err := h.svc.Me(r.Context(), tenantID, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Token valid tapi usernya sudah tidak ada (dihapus / pindah tenant):
			// itu sesi yang tidak lagi sah, bukan kesalahan server.
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, me)
}

type createUserDTO struct {
	Email    string      `json:"email"`
	Name     string      `json:"name"`
	Password string      `json:"password"`
	Role     domain.Role `json:"role"`
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth context")
		return
	}
	role, ok := auth.RoleFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth context")
		return
	}

	var dto createUserDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	user, err := h.svc.CreateUser(r.Context(), tenantID, domain.Role(role), CreateUserRequest{
		Email:    dto.Email,
		Name:     dto.Name,
		Password: dto.Password,
		Role:     dto.Role,
	})
	if err != nil {
		writeError(w, userErrStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

// updateUserDTO memakai pointer supaya "field tidak dikirim" berbeda dari
// "field dikosongkan" — PATCH parsial tidak boleh mereset apa pun diam-diam.
type updateUserDTO struct {
	Email    *string      `json:"email"`
	Name     *string      `json:"name"`
	Password *string      `json:"password"`
	Role     *domain.Role `json:"role"`
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth context")
		return
	}
	actorID, ok := auth.UserIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth context")
		return
	}
	role, ok := auth.RoleFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth context")
		return
	}

	targetID, err := strconv.ParseUint(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "userID tidak valid")
		return
	}

	var dto updateUserDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	user, err := h.svc.UpdateUser(r.Context(), tenantID, actorID, domain.Role(role), targetID, UpdateUserRequest{
		Email:    dto.Email,
		Name:     dto.Name,
		Password: dto.Password,
		Role:     dto.Role,
	})
	if err != nil {
		writeError(w, userErrStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func userErrStatus(err error) int {
	switch {
	case errors.Is(err, ErrInsufficientRole):
		return http.StatusForbidden
	case errors.Is(err, ErrEmailAlreadyExists):
		return http.StatusConflict
	case errors.Is(err, ErrUserNotFound):
		return http.StatusNotFound
	case isClientError(err):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func isClientError(err error) bool {
	return errors.Is(err, ErrWeakPassword) ||
		errors.Is(err, ErrInvalidRole) ||
		errors.Is(err, ErrEmailAlreadyExists) ||
		errors.Is(err, ErrInsufficientRole) ||
		errors.Is(err, ErrCannotDemoteSelf) ||
		errors.Is(err, ErrInvalidEmail) ||
		errors.Is(err, ErrTenantNotFound)
}
