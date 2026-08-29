package document

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

// Handler — HTTP untuk W-2 Document Domain.
type Handler struct{ svc *Service }

func NewHandler(db *gorm.DB) *Handler { return &Handler{svc: NewService(db)} }

// Svc mengekspos service untuk wiring (adapter di main).
func (h *Handler) Svc() *Service { return h.svc }

func (h *Handler) Mount(r chi.Router) {
	r.Route("/documents", func(r chi.Router) {
		r.Get("/", h.listDocuments)            // ?type=&year=&limit=
		r.Get("/types", h.listTypes)           // master konfigurasi penomoran
		r.Get("/types/history", h.typeHistory) // audit perubahan konfigurasi
		r.Get("/preview", h.preview)           // nomor berikutnya per jenis (read-only)
		r.Get("/audit", h.auditCash)           // W-3.4: pelanggaran INV-DOC-1 (?limit=)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireWrite())
			r.Post("/types", h.createType)
			r.Patch("/types/{typeID}", h.updateType)
		})
	})
}

// ── DTO ─────────────────────────────────────────────────────────────────────

type typeDTO struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	Prefix       string `json:"prefix"`
	NumberFormat string `json:"number_format,omitempty"`
	ResetPolicy  string `json:"reset_policy,omitempty"`
	Padding      uint8  `json:"padding,omitempty"`
}

type typeUpdateDTO struct {
	Name         *string `json:"name,omitempty"`
	Prefix       *string `json:"prefix,omitempty"`
	NumberFormat *string `json:"number_format,omitempty"`
	ResetPolicy  *string `json:"reset_policy,omitempty"`
	Padding      *uint8  `json:"padding,omitempty"`
	IsActive     *bool   `json:"is_active,omitempty"`
}

// ── Endpoint ────────────────────────────────────────────────────────────────

func (h *Handler) listTypes(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "tenant tidak dikenal")
		return
	}
	types, err := h.svc.ListTypes(r.Context(), tenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"types": types})
}

func (h *Handler) typeHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "tenant tidak dikenal")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.svc.ListMasterChanges(r.Context(), tenantID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"history": rows})
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "tenant tidak dikenal")
		return
	}
	out, err := h.svc.PreviewAll(r.Context(), tenantID, time.Now())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"preview": out})
}

func (h *Handler) listDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "tenant tidak dikenal")
		return
	}
	f := ListFilter{TypeCode: r.URL.Query().Get("type")}
	if y, err := strconv.ParseUint(r.URL.Query().Get("year"), 10, 16); err == nil {
		f.FiscalYear = uint16(y)
	}
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		f.Limit = l
	}
	docs, err := h.svc.ListDocuments(r.Context(), tenantID, f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"documents": docs})
}

// auditCash — W-3.4. Laporan pelanggaran INV-DOC-1 untuk tenant berjalan.
// Read-only dan tanpa RequireWrite: auditor perlu membacanya tanpa hak tulis.
func (h *Handler) auditCash(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "tenant tidak dikenal")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rep, err := h.svc.Audit(r.Context(), tenantID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (h *Handler) createType(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "tenant tidak dikenal")
		return
	}
	var dto typeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak valid")
		return
	}
	actor := actorOf(r)
	dt, err := h.svc.CreateType(r.Context(), tenantID, TypeInput{
		Code:         dto.Code,
		Name:         dto.Name,
		Prefix:       dto.Prefix,
		NumberFormat: dto.NumberFormat,
		ResetPolicy:  ResetPolicy(dto.ResetPolicy),
		Padding:      dto.Padding,
	}, actor)
	if err != nil {
		writeTypeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dt)
}

func (h *Handler) updateType(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "tenant tidak dikenal")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "typeID"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id jenis dokumen tidak valid")
		return
	}
	var dto typeUpdateDTO
	if derr := json.NewDecoder(r.Body).Decode(&dto); derr != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak valid")
		return
	}
	upd := TypeUpdate{
		Name:         dto.Name,
		Prefix:       dto.Prefix,
		NumberFormat: dto.NumberFormat,
		Padding:      dto.Padding,
		IsActive:     dto.IsActive,
	}
	if dto.ResetPolicy != nil {
		p := ResetPolicy(*dto.ResetPolicy)
		upd.ResetPolicy = &p
	}
	dt, uerr := h.svc.UpdateType(r.Context(), tenantID, id, upd, actorOf(r))
	if uerr != nil {
		writeTypeErr(w, uerr)
		return
	}
	writeJSON(w, http.StatusOK, dt)
}

// ── Util ────────────────────────────────────────────────────────────────────

func actorOf(r *http.Request) *uint64 {
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		return &uid
	}
	return nil
}

func writeTypeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTypeDuplicate):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrTypeNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrTypeInvalid), errors.Is(err, ErrFormatInvalid),
		errors.Is(err, ErrTypeUnknown), errors.Is(err, ErrTypeInactive):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
