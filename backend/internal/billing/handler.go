package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// TenantInvoiceLister lists all invoices for a tenant with joined buyer/unit data.
type TenantInvoiceLister interface {
	ListAllByTenant(ctx context.Context, tenantID uint64) ([]*InvoiceSummary, error)
}

// Handler exposes invoice and receipt endpoints.
type Handler struct {
	svc     *Service
	receipt *ReceiptService
	prints  PrintLoader
	lister  TenantInvoiceLister
}

func NewHandler(db *gorm.DB) (*Handler, *Service) {
	repo := NewGORMRepository(db)
	svc := NewService(repo, repo, repo, repo)
	receipt := NewReceiptService(repo, repo, repo)
	return &Handler{svc: svc, receipt: receipt, prints: repo, lister: repo}, svc
}

// ReceiptSvc mengekspos ReceiptService untuk wiring lintas-modul (collection).
func (h *Handler) ReceiptSvc() *ReceiptService { return h.receipt }

// Mount registers billing routes.
// GET  /invoices                             — list all invoices for tenant
// POST /sale-contracts/{contractID}/invoices — generate invoice
// GET  /sale-contracts/{contractID}/invoices — list invoices by contract
// GET  /invoices/{invoiceID}                 — get single invoice
// GET  /invoices/{invoiceID}/print           — printable HTML
func (h *Handler) Mount(r chi.Router) {
	r.Get("/invoices", h.listAllInvoices)
	r.Route("/sale-contracts/{contractID}/invoices", func(r chi.Router) {
		r.With(auth.RequireWrite()).Post("/", h.generateInvoice)
		r.Get("/", h.listInvoices)
		// R1: invoice kekurangan pembayaran (pasca pencairan bank).
		r.With(auth.RequireWrite()).Post("/shortfall", h.generateShortfallInvoice)
	})
	r.Get("/invoices/{invoiceID}", h.getInvoice)
	r.Get("/invoices/{invoiceID}/print", h.printInvoice)

	// Receipt (Kwitansi) — bukti penerimaan pembayaran termin.
	r.With(auth.RequireWrite()).Post("/termins/{terminID}/receipt", h.generateReceipt)
	r.Get("/termins/{terminID}/receipt", h.getReceiptByTermin)
	r.Get("/receipts/{receiptID}", h.getReceipt)
	r.Get("/receipts/{receiptID}/print", h.printReceipt)
}

// ── POST /sale-contracts/{contractID}/invoices/shortfall (R1) ─────────────────

type shortfallInvoiceBody struct {
	DueDate string `json:"due_date,omitempty"` // YYYY-MM-DD; kosong = +14 hari
	Notes   string `json:"notes,omitempty"`
}

func (h *Handler) generateShortfallInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := billingAuth(w, r)
	if !ok {
		return
	}
	contractID, ok := billingUintParam(w, r, "contractID")
	if !ok {
		return
	}
	var body shortfallInvoiceBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	var due time.Time
	if body.DueDate != "" {
		var perr error
		if due, perr = time.Parse("2006-01-02", body.DueDate); perr != nil {
			writeBillingError(w, http.StatusBadRequest, "due_date tidak valid (YYYY-MM-DD)")
			return
		}
	}
	inv, err := h.svc.GenerateShortfallInvoice(r.Context(), tenantID, contractID, userID, due, body.Notes)
	if err != nil {
		switch {
		case errors.Is(err, ErrNoOutstanding), errors.Is(err, ErrShortfallInvoiceExists):
			writeBillingError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrContractNotFound):
			writeBillingError(w, http.StatusNotFound, "kontrak tidak ditemukan")
		default:
			writeBillingError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeBillingJSON(w, http.StatusCreated, inv)
}

// ── POST /termins/{terminID}/receipt ──────────────────────────────────────────

type generateReceiptBody struct {
	Notes string `json:"notes"`
}

func (h *Handler) generateReceipt(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := billingAuth(w, r)
	if !ok {
		return
	}
	terminID, ok := billingUintParam(w, r, "terminID")
	if !ok {
		return
	}

	var body generateReceiptBody
	// Body opsional; abaikan error decode jika kosong.
	_ = json.NewDecoder(r.Body).Decode(&body)

	rec, err := h.receipt.GenerateReceipt(r.Context(), tenantID, userID, terminID, body.Notes)
	if err != nil {
		if errors.Is(err, ErrTerminNotFound) {
			writeBillingError(w, http.StatusNotFound, err.Error())
		} else {
			writeBillingError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeBillingJSON(w, http.StatusOK, rec)
}

// ── GET /termins/{terminID}/receipt ───────────────────────────────────────────

func (h *Handler) getReceiptByTermin(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := billingAuth(w, r)
	if !ok {
		return
	}
	terminID, ok := billingUintParam(w, r, "terminID")
	if !ok {
		return
	}
	rec, err := h.receipt.GetReceiptByTermin(r.Context(), tenantID, terminID)
	if err != nil {
		if errors.Is(err, ErrReceiptNotFound) {
			writeBillingError(w, http.StatusNotFound, err.Error())
		} else {
			writeBillingError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeBillingJSON(w, http.StatusOK, rec)
}

// ── GET /receipts/{receiptID} ─────────────────────────────────────────────────

func (h *Handler) getReceipt(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := billingAuth(w, r)
	if !ok {
		return
	}
	receiptID, ok := billingUintParam(w, r, "receiptID")
	if !ok {
		return
	}
	rec, err := h.receipt.GetReceipt(r.Context(), tenantID, receiptID)
	if err != nil {
		if errors.Is(err, ErrReceiptNotFound) {
			writeBillingError(w, http.StatusNotFound, err.Error())
		} else {
			writeBillingError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeBillingJSON(w, http.StatusOK, rec)
}

// ── GET /receipts/{receiptID}/print ───────────────────────────────────────────

func (h *Handler) printReceipt(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := billingAuth(w, r)
	if !ok {
		return
	}
	receiptID, ok := billingUintParam(w, r, "receiptID")
	if !ok {
		return
	}
	data, err := h.receipt.GetReceiptPrintData(r.Context(), tenantID, receiptID)
	if err != nil {
		if errors.Is(err, ErrReceiptNotFound) {
			writeBillingError(w, http.StatusNotFound, err.Error())
		} else {
			writeBillingError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := RenderReceiptPrint(w, data); err != nil {
		return
	}
}

// ── POST /sale-contracts/{contractID}/invoices ────────────────────────────────

type generateInvoiceBody struct {
	ScheduleID uint64 `json:"schedule_id"`
	IssueDate  string `json:"issue_date"` // optional; RFC3339 date
	Notes      string `json:"notes"`
}

func (h *Handler) generateInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := billingAuth(w, r)
	if !ok {
		return
	}
	contractID, ok := billingUintParam(w, r, "contractID")
	if !ok {
		return
	}

	var body generateInvoiceBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeBillingError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	if body.ScheduleID == 0 {
		writeBillingError(w, http.StatusBadRequest, "schedule_id wajib diisi")
		return
	}

	var issueDate time.Time
	if body.IssueDate != "" {
		var err error
		issueDate, err = time.Parse("2006-01-02", body.IssueDate)
		if err != nil {
			writeBillingError(w, http.StatusBadRequest, "issue_date format tidak valid (gunakan YYYY-MM-DD)")
			return
		}
	}

	inv, err := h.svc.GenerateInvoice(r.Context(), tenantID, contractID, userID, GenerateInvoiceRequest{
		ScheduleID: body.ScheduleID,
		IssueDate:  issueDate,
		Notes:      body.Notes,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrContractNotFound):
			writeBillingError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrScheduleNotFound):
			writeBillingError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrScheduleContractMismatch):
			writeBillingError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvoiceAlreadyExists):
			writeBillingError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrScheduleAlreadyReceived):
			writeBillingError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeBillingError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeBillingJSON(w, http.StatusCreated, inv)
}

// ── GET /sale-contracts/{contractID}/invoices ─────────────────────────────────

func (h *Handler) listInvoices(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := billingAuth(w, r)
	if !ok {
		return
	}
	contractID, ok := billingUintParam(w, r, "contractID")
	if !ok {
		return
	}

	invs, err := h.svc.ListByContract(r.Context(), tenantID, contractID)
	if err != nil {
		writeBillingError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeBillingJSON(w, http.StatusOK, invs)
}

// ── GET /invoices ─────────────────────────────────────────────────────────────

func (h *Handler) listAllInvoices(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := billingAuth(w, r)
	if !ok {
		return
	}
	invs, err := h.lister.ListAllByTenant(r.Context(), tenantID)
	if err != nil {
		writeBillingError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// S9/R-9: total belum-terbayar dihitung backend (decimal) — FE display saja.
	totalUnpaid := domain.Zero
	for _, inv := range invs {
		if inv.Status == StatusIssued || inv.Status == StatusOverdue {
			totalUnpaid = totalUnpaid.Add(inv.Amount)
		}
	}
	writeBillingJSON(w, http.StatusOK, struct {
		Invoices    []*InvoiceSummary `json:"invoices"`
		TotalUnpaid string            `json:"total_unpaid"`
	}{invs, totalUnpaid.String()})
}

// ── GET /invoices/{invoiceID} ─────────────────────────────────────────────────

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := billingAuth(w, r)
	if !ok {
		return
	}
	invoiceID, ok := billingUintParam(w, r, "invoiceID")
	if !ok {
		return
	}

	inv, err := h.svc.GetInvoice(r.Context(), tenantID, invoiceID)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			writeBillingError(w, http.StatusNotFound, err.Error())
		} else {
			writeBillingError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeBillingJSON(w, http.StatusOK, inv)
}

// ── GET /invoices/{invoiceID}/print ──────────────────────────────────────────

func (h *Handler) printInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := billingAuth(w, r)
	if !ok {
		return
	}
	invoiceID, ok := billingUintParam(w, r, "invoiceID")
	if !ok {
		return
	}
	data, err := h.svc.GetPrintData(r.Context(), tenantID, invoiceID)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			writeBillingError(w, http.StatusNotFound, err.Error())
		} else {
			writeBillingError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := RenderInvoicePrint(w, data); err != nil {
		// Header already sent as 200 — best effort log only.
		return
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func billingAuth(w http.ResponseWriter, r *http.Request) (tenantID, userID uint64, ok bool) {
	tid, tOK := auth.TenantIDFrom(r.Context())
	uid, uOK := auth.UserIDFrom(r.Context())
	if !tOK || !uOK {
		writeBillingError(w, http.StatusUnauthorized, "autentikasi diperlukan")
		return 0, 0, false
	}
	return tid, uid, true
}

func billingUintParam(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	val := chi.URLParam(r, name)
	n, err := strconv.ParseUint(val, 10, 64)
	if err != nil || n == 0 {
		writeBillingError(w, http.StatusBadRequest, name+" tidak valid")
		return 0, false
	}
	return n, true
}

func writeBillingJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeBillingError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
