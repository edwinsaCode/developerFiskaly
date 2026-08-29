package legacyar

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// maxUploadBytes membatasi ukuran berkas impor. Sepuluh megabyte cukup untuk
// puluhan ribu baris piutang; di atas itu lebih mungkin salah unggah daripada
// data sungguhan.
const maxUploadBytes = 10 << 20

// Handler — HTTP untuk Piutang Proyek Lama (W-7).
type Handler struct {
	svc *Service
}

// NewHandler membuat handler beserta service-nya.
func NewHandler(db *gorm.DB) *Handler {
	return &Handler{svc: NewService(NewRepository(db), db)}
}

// Svc mengekspos service untuk wiring (adapter pembaca laporan di main).
func (h *Handler) Svc() *Service { return h.svc }

func (h *Handler) Mount(r chi.Router) {
	r.Route("/legacy-ar", func(r chi.Router) {
		r.Get("/template", h.downloadTemplate)
		r.Get("/", h.list)
		r.Get("/reconciliation", h.reconciliation)
		r.Get("/batches", h.listBatches)
		r.Get("/batches/{batchID}", h.previewBatch)
		r.Get("/{id}", h.detail)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireWrite())
			r.Post("/batches", h.upload)
			r.Post("/batches/{batchID}/commit", h.commit)
			r.Post("/batches/{batchID}/discard", h.discard)
			r.Post("/payments", h.receivePayment)
			r.Post("/payments/{paymentID}/void", h.voidPayment)
		})
	})
}

// ── Template ─────────────────────────────────────────────────────────────────

func (h *Handler) downloadTemplate(w http.ResponseWriter, r *http.Request) {
	buf, err := BuildTemplate()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="template-piutang-proyek-lama.xlsx"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	_, _ = w.Write(buf)
}

// ── Impor ────────────────────────────────────────────────────────────────────

// upload menerima multipart berisi berkas Excel + tanggal posisi.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "berkas tidak terbaca: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "field `file` wajib diisi")
		return
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "berkas tidak terbaca: "+err.Error())
		return
	}
	if len(content) > maxUploadBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "berkas melebihi 10 MB")
		return
	}

	asOf, ok := parseDateReq(w, r.FormValue("as_of_date"))
	if !ok {
		return
	}

	batch, err := h.svc.Upload(r.Context(), tenantID, UploadRequest{
		FileName:       header.Filename,
		Content:        content,
		AsOfDate:       asOf,
		Notes:          r.FormValue("notes"),
		ControlAccount: r.FormValue("control_account_code"),
		CreatedBy:      actor,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}

	// Pratinjau langsung disertakan: tujuan unggah memang untuk DILIHAT dulu,
	// dan memaksa layar memanggil dua kali hanya menambah keadaan antara yang
	// bisa gagal separuh jalan.
	b, rows, rec, err := h.svc.PreviewBatch(r.Context(), tenantID, batch.ID)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"batch": b, "rows": rows, "reconciliation": rec,
	})
}

func (h *Handler) listBatches(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	out, err := h.svc.repo.ListBatches(r.Context(), tenantID, limit)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": out})
}

func (h *Handler) previewBatch(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	id, ok := urlID(w, r, "batchID")
	if !ok {
		return
	}
	b, rows, rec, err := h.svc.PreviewBatch(r.Context(), tenantID, id)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": b, "rows": rows, "reconciliation": rec})
}

type commitDTO struct {
	SkipReason                string `json:"skip_reason,omitempty"`
	OpeningCounterAccountCode string `json:"opening_counter_account_code,omitempty"`
	OpeningDescription        string `json:"opening_description,omitempty"`
}

func (h *Handler) commit(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	id, ok := urlID(w, r, "batchID")
	if !ok {
		return
	}
	var dto commitDTO
	// Body boleh kosong — commit tanpa jurnal saldo awal dan tanpa catatan.
	_ = json.NewDecoder(r.Body).Decode(&dto)

	out, err := h.svc.Commit(r.Context(), tenantID, id, CommitRequest{
		SkipReason:                dto.SkipReason,
		OpeningCounterAccountCode: dto.OpeningCounterAccountCode,
		OpeningDescription:        dto.OpeningDescription,
		ActorID:                   actor,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) discard(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	id, ok := urlID(w, r, "batchID")
	if !ok {
		return
	}
	if err := h.svc.DiscardBatch(r.Context(), tenantID, id); err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "discarded"})
}

// ── Daftar & detail ──────────────────────────────────────────────────────────

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := ListFilter{
		Status:      Status(q.Get("status")),
		Search:      normalizeSearch(q.Get("q")),
		SourceLabel: q.Get("source_label"),
		OnlyOpen:    q.Get("only_open") == "true",
	}
	if v := q.Get("batch_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			f.BatchID = n
		}
	}
	out, err := h.svc.ListReceivables(r.Context(), tenantID, f)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	id, ok := urlID(w, r, "id")
	if !ok {
		return
	}
	out, err := h.svc.GetReceivable(r.Context(), tenantID, id)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) reconciliation(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	asOf := time.Now()
	if v := r.URL.Query().Get("as_of"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "as_of harus YYYY-MM-DD")
			return
		}
		asOf = t
	}
	code := r.URL.Query().Get("control_account_code")
	out, err := h.svc.Reconcile(r.Context(), tenantID, code, asOf, domain.Zero)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ── Pelunasan ────────────────────────────────────────────────────────────────

type allocationDTO struct {
	ReceivableID uint64 `json:"receivable_id"`
	Amount       string `json:"amount"`
}

type paymentDTO struct {
	Allocations     []allocationDTO `json:"allocations"`
	CashAccountCode string          `json:"cash_account_code"`
	PaymentDate     string          `json:"payment_date,omitempty"`
	Notes           string          `json:"notes,omitempty"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
}

func (h *Handler) receivePayment(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	var dto paymentDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	if len(dto.Allocations) == 0 {
		writeErr(w, http.StatusBadRequest, "alokasi wajib diisi — pembayaran harus menunjuk piutang mana yang dilunasi")
		return
	}
	allocs := make([]PaymentAllocation, 0, len(dto.Allocations))
	for _, a := range dto.Allocations {
		m, err := domain.NewMoney(a.Amount)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "nominal tidak valid: "+err.Error())
			return
		}
		allocs = append(allocs, PaymentAllocation{ReceivableID: a.ReceivableID, Amount: m})
	}
	date, ok := parseDateOpt(w, dto.PaymentDate)
	if !ok {
		return
	}
	out, err := h.svc.ReceivePayment(r.Context(), tenantID, PaymentRequest{
		Allocations:     allocs,
		CashAccountCode: dto.CashAccountCode,
		PaymentDate:     date,
		Notes:           dto.Notes,
		IdempotencyKey:  dto.IdempotencyKey,
		ActorID:         actor,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

type voidDTO struct {
	Reason string `json:"reason"`
}

func (h *Handler) voidPayment(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	id, ok := urlID(w, r, "paymentID")
	if !ok {
		return
	}
	var dto voidDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	out, err := h.svc.VoidPayment(r.Context(), tenantID, id, dto.Reason, actor)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
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

func urlID(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, name), 10, 64)
	if err != nil || id == 0 {
		writeErr(w, http.StatusBadRequest, name+" tidak valid")
		return 0, false
	}
	return id, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func parseDateReq(w http.ResponseWriter, s string) (time.Time, bool) {
	if s == "" {
		writeErr(w, http.StatusBadRequest, "as_of_date wajib diisi (YYYY-MM-DD)")
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "as_of_date harus YYYY-MM-DD")
		return time.Time{}, false
	}
	return t, true
}

func parseDateOpt(w http.ResponseWriter, s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, true
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "tanggal harus YYYY-MM-DD")
		return time.Time{}, false
	}
	return t, true
}

// writeSvcErr memetakan sentinel paket ini ke status HTTP.
//
// Dipetakan satu per satu, bukan lewat "semua error validasi = 400": pembeda
// antara 409 (keadaan sudah begitu — batch ganda, sudah lunas) dan 400 (yang
// dikirim salah) menentukan apakah layar boleh menyuruh user mengulang.
func writeSvcErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBatchNotFound), errors.Is(err, ErrReceivableNotFound),
		errors.Is(err, ErrPaymentNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrBatchDuplicate), errors.Is(err, ErrExternalRefTaken),
		errors.Is(err, ErrPaymentVoided), errors.Is(err, ErrAlreadySettled),
		errors.Is(err, ErrBatchNotDraft):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrOverpayment), errors.Is(err, ErrAmountNotPositive),
		errors.Is(err, ErrHasErrorRows), errors.Is(err, ErrNoValidRows),
		errors.Is(err, ErrControlAccount), errors.Is(err, ErrCashAccount),
		errors.Is(err, ErrCounterAccount), errors.Is(err, ErrWrittenOff),
		errors.Is(err, ErrOpeningNotAllowed):
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
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

