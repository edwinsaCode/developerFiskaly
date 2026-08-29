package notary

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

// Handler — HTTP untuk Titipan Notaris (UAT Batch 2 §3).
type Handler struct {
	svc *Service
}

func NewHandler(db *gorm.DB) *Handler { return &Handler{svc: NewService(db)} }

func (h *Handler) Mount(r chi.Router) {
	r.Route("/notary-deposits", func(r chi.Router) {
		r.Get("/", h.list)
		// W-1: intake ditutup permanen (410 Gone, bukan 404) — client lama
		// harus tahu bedanya "endpoint hilang" dan "jalur ini sudah pindah".
		r.Post("/", h.receiveGone)
		// Drain tetap terbuka: titipan lama wajib bisa dibayarkan ke notaris.
		r.With(auth.RequireWrite()).Post("/{depositID}/payout", h.payout)
	})
}

type payoutDTO struct {
	BankAccountCode string `json:"bank_account_code"`
	PaidOutAt       string `json:"paid_out_at,omitempty"` // YYYY-MM-DD
	Notes           string `json:"notes,omitempty"`
}

// receiveGone menjawab 410 Gone + arah pindahnya. Tidak menyentuh DB sama
// sekali: tidak ada lagi cara membuat titipan notaris di luar Charge Group.
func (h *Handler) receiveGone(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusGone, map[string]string{
		"error":    ErrNotaryIntakeClosed.Error(),
		"moved_to": "POST /charges (kind=realization, charge_type_code=notaris)",
	})
}

func (h *Handler) payout(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "depositID"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "depositID tidak valid")
		return
	}
	var dto payoutDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	paidAt, err := parseDate(dto.PaidOutAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "paid_out_at harus YYYY-MM-DD")
		return
	}
	var actor *uint64
	if uid, uok := auth.UserIDFrom(r.Context()); uok {
		actor = &uid
	}
	d, err := h.svc.Payout(r.Context(), tenantID, id, PayoutRequest{
		BankAccountCode: dto.BankAccountCode,
		PaidOutAt:       paidAt,
		Notes:           dto.Notes,
		ActorID:         actor,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	out, err := h.svc.List(r.Context(), tenantID, DepositStatus(r.URL.Query().Get("status")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeSvcErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDepositNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrDepositNotHeld):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrDepositAmountInvalid), errors.Is(err, ErrCustomerRequired):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotaryAccountMissing):
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", s)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
