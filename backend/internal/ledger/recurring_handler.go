package ledger

// PS-5 — HTTP handlers Jurnal Berulang (mounted di /ledger/recurring).

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"esaproperti/internal/platform/auth"
)

func (h *Handler) mountRecurring(r chi.Router) {
	r.Route("/recurring", func(r chi.Router) {
		r.Get("/", h.listRecurring)
		r.With(auth.RequireWrite()).Post("/", h.createRecurring)
		r.With(auth.RequireWrite()).Post("/run-due", h.runDueRecurring)
		r.With(auth.RequireWrite()).Put("/{id}/active", h.setRecurringActive)
	})
}

func (h *Handler) listRecurring(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok || h.recurring == nil {
		writeError(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	rs, err := h.recurring.List(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rs == nil {
		rs = []*RecurringJournal{}
	}
	writeJSON(w, http.StatusOK, rs)
}

func (h *Handler) createRecurring(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok || h.recurring == nil {
		writeError(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	var dto struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		DayOfMonth  int             `json:"day_of_month"`
		Lines       []RecurringLine `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	var actor *uint64
	if uid, uok := auth.UserIDFrom(r.Context()); uok {
		actor = &uid
	}
	rj := &RecurringJournal{
		Name: dto.Name, Description: dto.Description,
		DayOfMonth: dto.DayOfMonth, Lines: dto.Lines,
		IsActive: true, CreatedBy: actor,
	}
	created, err := h.recurring.Create(r.Context(), tenantID, rj)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) runDueRecurring(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok || h.recurring == nil {
		writeError(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	var actor *uint64
	if uid, uok := auth.UserIDFrom(r.Context()); uok {
		actor = &uid
	}
	n, err := h.recurring.RunDue(r.Context(), tenantID, time.Now(), actor)
	if err != nil {
		// partial progress tetap dilaporkan
		writeJSON(w, http.StatusOK, map[string]any{"created": n, "warning": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"created": n})
}

func (h *Handler) setRecurringActive(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok || h.recurring == nil {
		writeError(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		Active bool `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	rj, err := h.recurring.SetActive(r.Context(), tenantID, id, dto.Active)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rj)
}
