package histfin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// Handler — HTTP untuk W-6 Snapshot Keuangan Historis.
type Handler struct {
	svc *Service
}

func NewHandler(db *gorm.DB) *Handler { return &Handler{svc: NewService(db)} }

func (h *Handler) Svc() *Service { return h.svc }

func (h *Handler) Mount(r chi.Router) {
	r.Route("/historical-financials", func(r chi.Router) {
		r.Get("/snapshots", h.list)
		r.Get("/snapshots/{year}", h.get)
		r.Get("/snapshots/{year}/neraca", h.neraca)
		r.Get("/snapshots/{year}/laba-rugi", h.labaRugi)
		r.Get("/snapshots/{year}/audits", h.audits)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireWrite())
			r.Post("/snapshots", h.create)
			r.Put("/snapshots/{year}", h.save)
			r.Post("/snapshots/{year}/finalize", h.finalize)
			r.Delete("/snapshots/{year}", h.remove)
		})
		// Membuka kembali angka yang sudah disahkan adalah wewenang pemilik.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole("owner"))
			r.Post("/snapshots/{year}/reopen", h.reopen)
		})
	})
}

// ── DTO ──────────────────────────────────────────────────────────────────────

type createDTO struct {
	FiscalYear int    `json:"fiscal_year"`
	Notes      string `json:"notes,omitempty"`
}

type lineDTO struct {
	AccountID uint64 `json:"account_id"`
	Amount    string `json:"amount"`
	SortOrder int    `json:"sort_order,omitempty"`
}

type saveDTO struct {
	Notes string    `json:"notes,omitempty"`
	Lines []lineDTO `json:"lines"`
}

type reopenDTO struct {
	Reason string `json:"reason"`
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func ctxIDs(w http.ResponseWriter, r *http.Request) (tenantID uint64, actor *uint64, ok bool) {
	tenantID, ok = auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return 0, nil, false
	}
	if uid, uok := auth.UserIDFrom(r.Context()); uok {
		actor = &uid
	}
	return tenantID, actor, true
}

func urlYear(w http.ResponseWriter, r *http.Request) (int, bool) {
	y, err := strconv.Atoi(chi.URLParam(r, "year"))
	if err != nil || y == 0 {
		writeErr(w, http.StatusBadRequest, "tahun buku tidak valid")
		return 0, false
	}
	return y, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// ── Endpoints ────────────────────────────────────────────────────────────────

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	out, err := h.svc.ListYears(r.Context(), tenantID)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	year, ok := urlYear(w, r)
	if !ok {
		return
	}
	out, err := h.svc.Get(r.Context(), tenantID, year)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) neraca(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	year, ok := urlYear(w, r)
	if !ok {
		return
	}
	out, err := h.svc.Neraca(r.Context(), tenantID, year)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) labaRugi(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	year, ok := urlYear(w, r)
	if !ok {
		return
	}
	out, err := h.svc.LabaRugi(r.Context(), tenantID, year)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) audits(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	year, ok := urlYear(w, r)
	if !ok {
		return
	}
	out, err := h.svc.Audits(r.Context(), tenantID, year)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	var dto createDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	out, err := h.svc.Create(r.Context(), tenantID, dto.FiscalYear, actor)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	// Catatan awal (kalau ada) disimpan lewat jalur save yang sama, supaya
	// hanya ada satu tempat yang menulis isi snapshot.
	if dto.Notes != "" {
		out, err = h.svc.Save(r.Context(), tenantID, dto.FiscalYear, SaveRequest{Notes: dto.Notes, Actor: actor})
		if err != nil {
			writeSvcErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	year, ok := urlYear(w, r)
	if !ok {
		return
	}
	var dto saveDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	lines := make([]LineInput, 0, len(dto.Lines))
	for i, l := range dto.Lines {
		amount, err := domain.NewMoney(l.Amount)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "nominal baris ke-"+strconv.Itoa(i+1)+" tidak valid: "+err.Error())
			return
		}
		lines = append(lines, LineInput{AccountID: l.AccountID, Amount: amount, SortOrder: l.SortOrder})
	}
	out, err := h.svc.Save(r.Context(), tenantID, year, SaveRequest{Notes: dto.Notes, Lines: lines, Actor: actor})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) finalize(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	year, ok := urlYear(w, r)
	if !ok {
		return
	}
	out, err := h.svc.Finalize(r.Context(), tenantID, year, actor)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) reopen(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	year, ok := urlYear(w, r)
	if !ok {
		return
	}
	var dto reopenDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	out, err := h.svc.Reopen(r.Context(), tenantID, year, dto.Reason, actor)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	year, ok := urlYear(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), tenantID, year); err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── Error mapping ────────────────────────────────────────────────────────────

func writeSvcErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSnapshotNotFound), errors.Is(err, ErrAccountNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrSnapshotExists), errors.Is(err, ErrSnapshotFinal),
		errors.Is(err, ErrSnapshotNotFinal), errors.Is(err, ErrYearHasJournals):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrYearInvalid), errors.Is(err, ErrYearFuture),
		errors.Is(err, ErrSnapshotEmpty), errors.Is(err, ErrNotBalanced),
		errors.Is(err, ErrReasonRequired), errors.Is(err, ErrDuplicateAccount),
		errors.Is(err, ErrAccountTypeUnkn), errors.Is(err, ErrComputedAccount):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
