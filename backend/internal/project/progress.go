package project

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/platform/auth"
)

// ── R3: Progress FISIK proyek ─────────────────────────────────────────────────
//
// Entitas OPERASIONAL — bukan data akuntansi, tidak menyentuh ledger, tidak
// menduplikasi source of truth keuangan (tidak ada angka uang di dalamnya).
// APPEND-ONLY: koreksi = entri baru dengan nilai yang benar; histori adalah
// kurva-S proyek + audit siapa/kapan input.

type ProjectProgressEntry struct {
	ID          uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    uint64          `gorm:"not null;index"           json:"-"`
	ProjectID   uint64          `gorm:"not null;index"           json:"project_id"`
	PhaseID     *uint64         `json:"phase_id,omitempty"`
	ProgressPct decimal.Decimal `gorm:"type:DECIMAL(5,2);not null" json:"progress_pct"`
	AsOfDate    time.Time       `gorm:"not null;type:date"       json:"as_of_date"`
	Notes       string          `gorm:"size:500;not null;default:''" json:"notes,omitempty"`
	CreatedBy   *uint64         `json:"created_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func (ProjectProgressEntry) TableName() string { return "project_progress_entries" }

var (
	ErrProgressPctInvalid = errors.New("progress_pct harus 0–100")
	// Guard monotonic longgar: penurunan diizinkan (koreksi), tapi wajib ada
	// catatan agar audit trail menjelaskan kenapa progress "mundur".
	ErrProgressDropNeedsNote = errors.New("progress lebih rendah dari entri terakhir — wajib isi catatan alasan (koreksi/rework)")
)

// AddProgressRequest adalah input satu titik progress fisik.
type AddProgressRequest struct {
	PhaseID     *uint64
	ProgressPct decimal.Decimal
	AsOfDate    time.Time
	Notes       string
	CreatedBy   *uint64
}

// AddProgress menambah entri progress fisik (append-only).
func (s *Service) AddProgress(ctx context.Context, tenantID, projectID uint64, req AddProgressRequest, db *gorm.DB) (*ProjectProgressEntry, error) {
	if req.ProgressPct.IsNegative() || req.ProgressPct.GreaterThan(decimal.NewFromInt(100)) {
		return nil, ErrProgressPctInvalid
	}
	if _, err := s.projects.FindProjectByID(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	// Entri terakhir (scope sama: proyek + fase) untuk guard penurunan.
	var last ProjectProgressEntry
	q := db.WithContext(ctx).Where("tenant_id = ? AND project_id = ?", tenantID, projectID)
	if req.PhaseID != nil {
		q = q.Where("phase_id = ?", *req.PhaseID)
	} else {
		q = q.Where("phase_id IS NULL")
	}
	if err := q.Order("as_of_date DESC, id DESC").First(&last).Error; err == nil {
		if req.ProgressPct.LessThan(last.ProgressPct) && req.Notes == "" {
			return nil, ErrProgressDropNeedsNote
		}
	}

	e := &ProjectProgressEntry{
		TenantID:    tenantID,
		ProjectID:   projectID,
		PhaseID:     req.PhaseID,
		ProgressPct: req.ProgressPct,
		AsOfDate:    req.AsOfDate,
		Notes:       req.Notes,
		CreatedBy:   req.CreatedBy,
	}
	if err := db.WithContext(ctx).Create(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ListProgress mengembalikan histori progress (kurva-S) terbaru dulu.
func ListProgress(ctx context.Context, db *gorm.DB, tenantID, projectID uint64) ([]*ProjectProgressEntry, error) {
	var out []*ProjectProgressEntry
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Order("as_of_date DESC, id DESC").
		Find(&out).Error
	return out, err
}

// ── HTTP ──────────────────────────────────────────────────────────────────────

type addProgressDTO struct {
	PhaseID     *uint64 `json:"phase_id,omitempty"`
	ProgressPct string  `json:"progress_pct"`
	AsOfDate    string  `json:"as_of_date"` // YYYY-MM-DD; kosong = hari ini
	Notes       string  `json:"notes,omitempty"`
}

// MountProgress mendaftarkan route progress fisik. db diberikan langsung dari
// wiring (read model sederhana — tidak lewat repo penuh).
func (h *Handler) MountProgress(r chi.Router, db *gorm.DB) {
	r.Route("/projects/{projectID}/progress", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) { h.listProgress(w, req, db) })
		r.With(auth.RequireWrite()).Post("/", func(w http.ResponseWriter, req *http.Request) { h.addProgress(w, req, db) })
	})
}

func (h *Handler) addProgress(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseUintParam(r, "projectID")
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	var dto addProgressDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeProjectError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	pct, err := decimal.NewFromString(dto.ProgressPct)
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "progress_pct tidak valid")
		return
	}
	asOf := time.Now()
	if dto.AsOfDate != "" {
		asOf, err = time.Parse("2006-01-02", dto.AsOfDate)
		if err != nil {
			writeProjectError(w, http.StatusBadRequest, "as_of_date format YYYY-MM-DD")
			return
		}
	}
	userID, _ := auth.UserIDFrom(r.Context())
	var createdBy *uint64
	if userID != 0 {
		createdBy = &userID
	}
	e, err := h.svc.AddProgress(r.Context(), tenantID, projectID, AddProgressRequest{
		PhaseID: dto.PhaseID, ProgressPct: pct, AsOfDate: asOf, Notes: dto.Notes, CreatedBy: createdBy,
	}, db)
	if err != nil {
		switch {
		case errors.Is(err, ErrProgressPctInvalid), errors.Is(err, ErrProgressDropNeedsNote):
			writeProjectError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrProjectNotFound):
			writeProjectError(w, http.StatusNotFound, "proyek tidak ditemukan")
		default:
			writeProjectError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeProjectJSON(w, http.StatusCreated, e)
}

func (h *Handler) listProgress(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	tenantID, err := projectTenantID(r)
	if err != nil {
		writeProjectError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseUintParam(r, "projectID")
	if err != nil {
		writeProjectError(w, http.StatusBadRequest, "projectID tidak valid")
		return
	}
	entries, err := ListProgress(r.Context(), db, tenantID, projectID)
	if err != nil {
		writeProjectError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProjectJSON(w, http.StatusOK, entries)
}
