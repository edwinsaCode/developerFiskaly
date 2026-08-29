package closing

// HTTP handlers P0-4 (tanpa business logic — konvensi package).

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

type Handler struct{ svc *Service }

func NewHandler(db *gorm.DB) *Handler { return &Handler{svc: NewService(db)} }

// Svc exposes the service for cross-package wiring (sale.SetFinalizedHPPSource).
func (h *Handler) Svc() *Service { return h.svc }

// Mount mendaftarkan route closing. Router sudah ber-auth middleware.
//
// PENTING: registrasi FLAT (bukan r.Route("/projects/{projectID}", ...)) —
// nested Route/Mount pada pattern /projects/{param} MENIMPA subtree
// /projects/{id} milik project handler (chi mount collision) dan mematikan
// seluruh route detail proyek. Pola flat mengikuti cost/budget handler.
func (h *Handler) Mount(r chi.Router) {
	r.With(auth.RequireWrite()).Post("/projects/{projectID}/completion", h.markCompleted)
	r.With(auth.RequireWrite()).Post("/projects/{projectID}/completion/finalize", h.finalizeCompletion)
	r.Get("/projects/{projectID}/completion", h.getCompletion)
	r.Get("/projects/{projectID}/hpp-variance", h.previewVariance)
	r.With(auth.RequireWrite()).Post("/projects/{projectID}/hpp-trueup/calculate", h.calculate)
	r.Get("/projects/{projectID}/hpp-trueup", h.listRuns)

	r.Get("/hpp-trueup/{runID}", h.getRun)
	r.With(auth.RequireWrite()).Post("/hpp-trueup/{runID}/approve", h.approve)
	r.With(auth.RequireWrite()).Post("/hpp-trueup/{runID}/post", h.post)
	r.With(auth.RequireWrite()).Post("/hpp-trueup/{runID}/cancel", h.cancel)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func tenantAndActor(r *http.Request) (uint64, *uint64, bool) {
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

func param(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

// mapErr memetakan error closing ke status HTTP.
func mapErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrProjectNotFound), errors.Is(err, ErrCompletionNotFound),
		errors.Is(err, ErrRunNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidStateTransition):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrCompletionNotFinalized), errors.Is(err, ErrMixedAllocationBasisVersion),
		errors.Is(err, ErrNoBasisConfigured), errors.Is(err, ErrNoSoldUnits),
		errors.Is(err, ErrTrueupNotPosted):
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// ── Completion ────────────────────────────────────────────────────────────────

func (h *Handler) markCompleted(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	pid, err := param(r, "projectID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	var dto struct {
		CompletedDate string `json:"completed_date,omitempty"` // YYYY-MM-DD; kosong = hari ini
	}
	_ = json.NewDecoder(r.Body).Decode(&dto) // body opsional
	completedAt := time.Now()
	if dto.CompletedDate != "" {
		t, perr := time.Parse("2006-01-02", dto.CompletedDate)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "completed_date harus YYYY-MM-DD")
			return
		}
		completedAt = t
	}
	ev, err := h.svc.MarkCompleted(r.Context(), tid, pid, completedAt, actor)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (h *Handler) finalizeCompletion(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	pid, err := param(r, "projectID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	ev, err := h.svc.FinalizeCompletion(r.Context(), tid, pid, actor)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (h *Handler) getCompletion(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	pid, err := param(r, "projectID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	ev, err := h.svc.GetCompletion(r.Context(), tid, pid)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

// ── Variance & runs ───────────────────────────────────────────────────────────

func (h *Handler) previewVariance(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	pid, err := param(r, "projectID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	v, err := h.svc.PreviewVariance(r.Context(), tid, pid)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) calculate(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	pid, err := param(r, "projectID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	run, err := h.svc.Calculate(r.Context(), tid, pid, actor)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	pid, err := param(r, "projectID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	runs, err := h.svc.ListRuns(r.Context(), tid, pid)
	if err != nil {
		mapErr(w, err)
		return
	}
	if runs == nil {
		runs = []*TrueupRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := param(r, "runID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "runID tidak valid")
		return
	}
	run, err := h.svc.GetRun(r.Context(), tid, id)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := param(r, "runID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "runID tidak valid")
		return
	}
	run, err := h.svc.Approve(r.Context(), tid, id, actor)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) post(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := param(r, "runID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "runID tidak valid")
		return
	}
	var dto struct {
		PostDate string `json:"post_date,omitempty"` // YYYY-MM-DD; kosong = hari ini (D4)
	}
	_ = json.NewDecoder(r.Body).Decode(&dto)
	postDate := time.Now()
	if dto.PostDate != "" {
		t, perr := time.Parse("2006-01-02", dto.PostDate)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "post_date harus YYYY-MM-DD")
			return
		}
		postDate = t
	}
	run, err := h.svc.Post(r.Context(), tid, id, postDate, actor)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := tenantAndActor(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := param(r, "runID")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "runID tidak valid")
		return
	}
	run, err := h.svc.Cancel(r.Context(), tid, id, actor)
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}
