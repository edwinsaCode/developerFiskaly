package ap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/approval"
	"esaproperti/internal/budget"
	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/project"
)

// Handler adalah lapisan HTTP hutang usaha. Nol business logic di sini: setiap
// method menerjemahkan JSON ke request domain, memanggil service, lalu
// menerjemahkan error domain menjadi status code.
type Handler struct{ svc *Service }

// NewHandler membangun handler produksi.
//
// Perhatikan planner biaya yang dipasang: ia `*cost.Service` yang dirakit
// dengan resolver yang SAMA seperti di cost.NewHandler. Merakitnya dengan
// resolver yang lebih sedikit akan membuat tagihan vendor lolos pemeriksaan
// yang ditolak jalur biaya kas — persis dua definisi aturan yang A-1 hapus.
func NewHandler(db *gorm.DB) *Handler {
	planner := newCostPlanner(db)
	svc := NewService(db, planner).
		WithApprovalGate(&gateAdapter{svc: approval.NewService(approval.NewGORMRepository(db))})
	return &Handler{svc: svc}
}

// newCostPlanner merakit cost.Service khusus sebagai PLANNER — ia tidak pernah
// dipakai untuk menulis biaya (tidak ada TxRunner yang dipasang), hanya untuk
// menjawab "baris ini sah, dan akun debitnya apa".
func newCostPlanner(db *gorm.DB) *cost.Service {
	repo := cost.NewGORMRepository(db)
	ledgerRepo := ledger.NewGORMRepository(db)
	postingSvc := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	adapter := cost.NewLedgerJournalAdapter(postingSvc)
	svc := cost.NewService(repo, adapter, repo, repo, newBudgetItemLookup(db))
	svc.SetUnitPolicyResolver(project.NewGORMRepository(db))
	svc.SetExpenseTypeResolver(repo)
	svc.SetPaymentAccountValidator(repo)
	svc.SetUnitProjectResolver(repo)
	return svc
}

// gateAdapter menyempitkan approval.Service menjadi satu pertanyaan.
//
// TargetCost dipakai (bukan jenis target baru) karena daftar jenis target hidup
// di schema approval; menambah nilai baru berarti migrasi di modul yang tidak
// termasuk cakupan W-11.
type gateAdapter struct{ svc *approval.Service }

func (a *gateAdapter) RequireApproved(ctx context.Context, tenantID, invoiceID uint64) error {
	err := a.svc.RequireApproved(ctx, tenantID, approval.TargetCost, invoiceID)
	if errors.Is(err, approval.ErrApprovalRequired) {
		return ErrApprovalRequired
	}
	return err
}

// Svc membuka service (dipakai wiring & test).
func (h *Handler) Svc() *Service { return h.svc }

func (h *Handler) Mount(r chi.Router) {
	r.Route("/ap", func(r chi.Router) {
		r.Route("/vendors", func(r chi.Router) {
			r.Get("/", h.listVendors)
			r.With(auth.RequireWrite()).Post("/", h.createVendor)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.getVendor)
				r.With(auth.RequireWrite()).Patch("/", h.updateVendor)
			})
		})
		r.Route("/invoices", func(r chi.Router) {
			r.Get("/", h.listInvoices)
			r.Get("/print", h.listInvoicesPrint)
			r.Post("/preview", h.previewInvoice)
			r.With(auth.RequireWrite()).Post("/", h.createInvoice)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.getInvoice)
				r.Get("/payments", h.listInvoicePayments)
				r.With(auth.RequireWrite()).Post("/post", h.postInvoice)
				r.With(auth.RequireWrite()).Post("/reverse", h.reverseInvoice)
			})
		})
		r.Route("/payments", func(r chi.Router) {
			r.Get("/", h.listPayments)
			// Pratinjau TIDAK di balik RequireWrite: ia tidak menulis apa pun
			// (transaksinya di-rollback), dan mengharuskan hak tulis untuk
			// melihat angka akan mendorong orang menebak alih-alih memeriksa.
			r.Post("/preview", h.previewPayment)
			r.With(auth.RequireWrite()).Post("/", h.createPayment)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.getPayment)
				r.Get("/print", h.getPaymentPrint)
				r.With(auth.RequireWrite()).Post("/reverse", h.reversePayment)
			})
		})
	})
}

// ── DTO ──────────────────────────────────────────────────────────────────────

type vendorBody struct {
	Name     *string `json:"name"`
	NPWP     *string `json:"npwp"`
	IsPKP    *bool   `json:"is_pkp"`
	Address  *string `json:"address"`
	Phone    *string `json:"phone"`
	Email    *string `json:"email"`
	BankName *string `json:"bank_name"`
	BankAcc  *string `json:"bank_account"`
	Note     *string `json:"note"`
	IsActive *bool   `json:"is_active"`
}

type invoiceLineBody struct {
	UnitID       *uint64 `json:"unit_id"`
	PhaseID      *uint64 `json:"phase_id"`
	Category     string  `json:"category"`
	CostTier     string  `json:"cost_tier"`
	Amount       string  `json:"amount"`
	BudgetItemID *uint64 `json:"budget_item_id"`
	Description  string  `json:"description"`
}

type invoiceBody struct {
	VendorID  uint64 `json:"vendor_id"`
	ProjectID uint64 `json:"project_id"`

	InvoiceNumber string `json:"invoice_number"`
	InvoiceDate   string `json:"invoice_date"`
	DueDate       string `json:"due_date"`

	DPPAmount         string `json:"dpp_amount"`
	PPNAmount         string `json:"ppn_amount"`
	FakturPajakNumber string `json:"faktur_pajak_number"`
	RetentionAmount   string `json:"retention_amount"`
	RetentionDueDate  string `json:"retention_due_date"`
	AdvanceApplied    string `json:"advance_applied"`

	Description string            `json:"description"`
	Lines       []invoiceLineBody `json:"lines"`
}

type reverseBody struct {
	ReverseDate string `json:"reverse_date"`
}

type paymentAllocBody struct {
	InvoiceID uint64 `json:"invoice_id"`
	Amount    string `json:"amount"`
}

type paymentBody struct {
	VendorID        uint64             `json:"vendor_id"`
	PaymentKind     string             `json:"payment_kind"`
	PaymentDate     string             `json:"payment_date"`
	Amount          string             `json:"amount"`
	CashAccountCode string             `json:"cash_account_code"`
	Allocations     []paymentAllocBody `json:"allocations"`
	Description     string             `json:"description"`
	// IdempotencyKey boleh datang dari body ATAU dari header Idempotency-Key.
	// Header didahulukan: ia yang dikirim otomatis oleh klien HTTP, sementara
	// body diisi manual dan lebih mudah tertinggal saat permintaan diulang.
	IdempotencyKey string `json:"idempotency_key"`
}

// ── Vendor ───────────────────────────────────────────────────────────────────

func (h *Handler) listVendors(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	f := VendorFilter{Search: r.URL.Query().Get("q")}
	switch r.URL.Query().Get("active") {
	case "true":
		t := true
		f.ActiveOnly = &t
	case "false":
		fa := false
		f.ActiveOnly = &fa
	}
	list, err := h.svc.ListVendors(r.Context(), tid, f)
	if err != nil {
		apErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	apJSON(w, http.StatusOK, list)
}

func (h *Handler) createVendor(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	var b vendorBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		apErr(w, http.StatusBadRequest, "body JSON tidak valid")
		return
	}
	v, err := h.svc.CreateVendor(r.Context(), tid, CreateVendorRequest{
		Name:     deref(b.Name),
		NPWP:     deref(b.NPWP),
		IsPKP:    b.IsPKP != nil && *b.IsPKP,
		Address:  deref(b.Address),
		Phone:    deref(b.Phone),
		Email:    deref(b.Email),
		BankName: deref(b.BankName),
		BankAcc:  deref(b.BankAcc),
		Note:     deref(b.Note),
	})
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusCreated, v)
}

func (h *Handler) getVendor(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := h.svc.GetVendor(r.Context(), tid, id)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, v)
}

func (h *Handler) updateVendor(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var b vendorBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		apErr(w, http.StatusBadRequest, "body JSON tidak valid")
		return
	}
	v, err := h.svc.UpdateVendor(r.Context(), tid, id, UpdateVendorRequest{
		Name: b.Name, NPWP: b.NPWP, IsPKP: b.IsPKP, Address: b.Address,
		Phone: b.Phone, Email: b.Email, BankName: b.BankName, BankAcc: b.BankAcc,
		Note: b.Note, IsActive: b.IsActive,
	})
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, v)
}

// ── Tagihan ──────────────────────────────────────────────────────────────────

func (h *Handler) listInvoices(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	q := r.URL.Query()
	f := InvoiceFilter{
		VendorID:  parseUint(q.Get("vendor_id")),
		ProjectID: parseUint(q.Get("project_id")),
		Status:    InvoiceStatus(q.Get("status")),
	}
	if s := q.Get("due_before"); s != "" {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			apErr(w, http.StatusBadRequest, "due_before harus YYYY-MM-DD")
			return
		}
		f.DueBefore = &d
	}
	list, err := h.svc.ListInvoices(r.Context(), tid, f)
	if err != nil {
		apErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	apJSON(w, http.StatusOK, list)
}

// listInvoicesPrint merender Laporan Hutang Usaha ke HTML A4 siap-cetak —
// sumber data SAMA dengan listInvoices (ListInvoices), tidak menghitung ulang.
// Pola sama dengan internal/reporting/handler.go (write*Print handlers).
func (h *Handler) listInvoicesPrint(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	q := r.URL.Query()
	f := InvoiceFilter{
		VendorID:  parseUint(q.Get("vendor_id")),
		ProjectID: parseUint(q.Get("project_id")),
		Status:    InvoiceStatus(q.Get("status")),
	}
	list, err := h.svc.ListInvoices(r.Context(), tid, f)
	if err != nil {
		apErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	company, _ := h.svc.TenantName(r.Context(), tid)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = RenderAPInvoiceListPrint(w, company, list)
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := h.svc.GetInvoice(r.Context(), tid, id)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, v)
}

func (h *Handler) previewInvoice(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	req, err := decodeInvoice(r, actor)
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.svc.PreviewInvoice(r.Context(), tid, req)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, res)
}

func (h *Handler) createInvoice(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	req, err := decodeInvoice(r, actor)
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	inv, err := h.svc.RecordInvoice(r.Context(), tid, req)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusCreated, inv)
}

func (h *Handler) postInvoice(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	inv, err := h.svc.PostInvoice(r.Context(), tid, id)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, inv)
}

func (h *Handler) reverseInvoice(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var b reverseBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	var when time.Time
	if b.ReverseDate != "" {
		when, err = time.Parse("2006-01-02", b.ReverseDate)
		if err != nil {
			apErr(w, http.StatusBadRequest, "reverse_date harus YYYY-MM-DD")
			return
		}
	}
	inv, err := h.svc.ReverseInvoice(r.Context(), tid, id, when)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, inv)
}

func (h *Handler) listInvoicePayments(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.svc.ListInvoicePayments(r.Context(), tid, id)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, rows)
}

// ── Pembayaran ───────────────────────────────────────────────────────────────

func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	q := r.URL.Query()
	list, err := h.svc.ListPayments(r.Context(), tid, PaymentFilter{
		VendorID:        parseUint(q.Get("vendor_id")),
		Kind:            PaymentKind(q.Get("payment_kind")),
		IncludeReversed: q.Get("include_reversed") == "true",
	})
	if err != nil {
		apErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	apJSON(w, http.StatusOK, list)
}

func (h *Handler) getPayment(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := h.svc.GetPayment(r.Context(), tid, id)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, v)
}

// getPaymentPrint merender bukti kas keluar (BKK) satu pembayaran vendor ke
// HTML A4 siap-cetak — sumber data SAMA dengan getPayment (GetPayment), tidak
// membuat transaksi baru. Nomor dokumennya adalah Payment.DocumentNumber yang
// sudah ditautkan saat RecordPayment memposting jurnal (INV-DOC-1) — kwitansi
// ini murni representasi baca dari catatan yang sudah ada.
func (h *Handler) getPaymentPrint(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := h.svc.GetPayment(r.Context(), tid, id)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	vendor, _ := h.svc.repo.FindVendor(r.Context(), tid, v.VendorID)
	company, _ := h.svc.TenantName(r.Context(), tid)
	data := &PaymentReceiptData{
		DocumentNumber:  v.DocumentNumber,
		PaymentDate:     v.PaymentDate,
		Amount:          v.Amount,
		CashAccountCode: v.CashAccountCode,
		Kind:            v.Kind,
		Description:     v.Description,
		CompanyName:     company,
		VendorName:      v.VendorName,
		Allocations:     v.Allocations,
		IsReversed:      v.IsReversed,
	}
	if vendor != nil {
		data.VendorAddress = vendor.Address
		data.VendorNPWP = vendor.NPWP
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = RenderPaymentReceiptPrint(w, data)
}

func (h *Handler) previewPayment(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	req, err := decodePayment(r, actor)
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.svc.PreviewPayment(r.Context(), tid, req)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, res)
}

func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	req, err := decodePayment(r, actor)
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.svc.RecordPayment(r.Context(), tid, req)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	// Permintaan yang diulang mengembalikan 200, bukan 201: tidak ada yang baru
	// dibuat. Klien yang membedakan keduanya bisa tahu tanpa membaca body.
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	apJSON(w, status, res)
}

func (h *Handler) reversePayment(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := apAuth(r)
	if !ok {
		apErr(w, http.StatusUnauthorized, "konteks autentikasi tidak ada")
		return
	}
	id, err := apParamID(r, "id")
	if err != nil {
		apErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var b reverseBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	var when time.Time
	if b.ReverseDate != "" {
		when, err = time.Parse("2006-01-02", b.ReverseDate)
		if err != nil {
			apErr(w, http.StatusBadRequest, "reverse_date harus YYYY-MM-DD")
			return
		}
	}
	pay, err := h.svc.ReversePayment(r.Context(), tid, id, when)
	if err != nil {
		apWriteErr(w, err)
		return
	}
	apJSON(w, http.StatusOK, pay)
}

func decodePayment(r *http.Request, actor *uint64) (CreatePaymentRequest, error) {
	var b paymentBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		return CreatePaymentRequest{}, errors.New("body JSON tidak valid")
	}
	amount, err := parseMoney(b.Amount, "amount")
	if err != nil {
		return CreatePaymentRequest{}, err
	}
	req := CreatePaymentRequest{
		VendorID:        b.VendorID,
		Kind:            PaymentKind(strings.TrimSpace(b.PaymentKind)),
		Amount:          amount,
		CashAccountCode: strings.TrimSpace(b.CashAccountCode),
		Description:     b.Description,
		IdempotencyKey:  strings.TrimSpace(b.IdempotencyKey),
		ActorID:         actor,
	}
	if k := strings.TrimSpace(r.Header.Get("Idempotency-Key")); k != "" {
		req.IdempotencyKey = k
	}
	// payment_date opsional: kosong = hari ini. Tanggal pembayaran yang wajib
	// diisi hanya akan diisi asal-asalan saat operator sedang terburu-buru.
	if strings.TrimSpace(b.PaymentDate) != "" {
		d, err := parseDate(b.PaymentDate, "payment_date")
		if err != nil {
			return CreatePaymentRequest{}, err
		}
		req.PaymentDate = d
	}
	for i, a := range b.Allocations {
		amt, err := parseMoney(a.Amount, "allocations["+strconv.Itoa(i)+"].amount")
		if err != nil {
			return CreatePaymentRequest{}, err
		}
		req.Allocations = append(req.Allocations, AllocationInput{InvoiceID: a.InvoiceID, Amount: amt})
	}
	return req, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func decodeInvoice(r *http.Request, actor *uint64) (CreateInvoiceRequest, error) {
	var b invoiceBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		return CreateInvoiceRequest{}, errors.New("body JSON tidak valid")
	}
	invDate, err := parseDate(b.InvoiceDate, "invoice_date")
	if err != nil {
		return CreateInvoiceRequest{}, err
	}
	dueDate, err := parseDate(b.DueDate, "due_date")
	if err != nil {
		return CreateInvoiceRequest{}, err
	}
	dpp, err := parseMoney(b.DPPAmount, "dpp_amount")
	if err != nil {
		return CreateInvoiceRequest{}, err
	}
	ppn, err := parseMoney(b.PPNAmount, "ppn_amount")
	if err != nil {
		return CreateInvoiceRequest{}, err
	}
	ret, err := parseMoney(b.RetentionAmount, "retention_amount")
	if err != nil {
		return CreateInvoiceRequest{}, err
	}
	adv, err := parseMoney(b.AdvanceApplied, "advance_applied")
	if err != nil {
		return CreateInvoiceRequest{}, err
	}
	req := CreateInvoiceRequest{
		VendorID:          b.VendorID,
		ProjectID:         b.ProjectID,
		InvoiceNumber:     b.InvoiceNumber,
		InvoiceDate:       invDate,
		DueDate:           dueDate,
		DPPAmount:         dpp,
		PPNAmount:         ppn,
		FakturPajakNumber: b.FakturPajakNumber,
		RetentionAmount:   ret,
		AdvanceApplied:    adv,
		Description:       b.Description,
		ActorID:           actor,
	}
	if b.RetentionDueDate != "" {
		d, err := parseDate(b.RetentionDueDate, "retention_due_date")
		if err != nil {
			return CreateInvoiceRequest{}, err
		}
		req.RetentionDueDate = &d
	}
	for i, ln := range b.Lines {
		amt, err := parseMoney(ln.Amount, "lines["+strconv.Itoa(i)+"].amount")
		if err != nil {
			return CreateInvoiceRequest{}, err
		}
		req.Lines = append(req.Lines, InvoiceLineInput{
			UnitID:       ln.UnitID,
			PhaseID:      ln.PhaseID,
			Category:     domain.CostCategory(ln.Category),
			CostTier:     domain.CostTier(ln.CostTier),
			Amount:       amt,
			BudgetItemID: ln.BudgetItemID,
			Description:  ln.Description,
		})
	}
	return req, nil
}

func parseDate(s, field string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, errors.New(field + " wajib diisi (YYYY-MM-DD)")
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, errors.New(field + " harus YYYY-MM-DD")
	}
	return d, nil
}

func parseMoney(s, field string) (domain.Money, error) {
	if strings.TrimSpace(s) == "" {
		return domain.Zero, nil
	}
	m, err := domain.NewMoney(s)
	if err != nil {
		return domain.Zero, errors.New(field + " bukan nominal yang sah")
	}
	return m, nil
}

func parseUint(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func apAuth(r *http.Request) (uint64, *uint64, bool) {
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

func apParamID(r *http.Request, name string) (uint64, error) {
	v, err := strconv.ParseUint(chi.URLParam(r, name), 10, 64)
	if err != nil || v == 0 {
		return 0, errors.New(name + " tidak valid")
	}
	return v, nil
}

func apJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apErr(w http.ResponseWriter, status int, msg string) {
	apJSON(w, status, map[string]string{"error": msg})
}

// apWriteErr memetakan error domain ke status HTTP.
//
// Pemetaannya sengaja eksplisit: kesalahan input pengguna (422) harus terpisah
// dari kesalahan sistem (500), karena yang pertama layak ditampilkan apa adanya
// di layar dan yang kedua tidak.
func apWriteErr(w http.ResponseWriter, err error) {
	// D-18: kelebihan bayar dijawab dengan ANGKANYA, bukan hanya kalimat. Layar
	// pembayaran butuh ketiganya untuk menawarkan koreksi yang benar tanpa
	// menyuruh operator membuka tagihan satu per satu.
	var over *OverpaymentError
	if errors.As(err, &over) {
		body := map[string]any{
			"error":       "overpayment",
			"message":     over.Error(),
			"outstanding": over.Outstanding.String(),
			"requested":   over.Requested.String(),
			"excess":      over.Excess().String(),
		}
		if over.InvoiceID != 0 {
			body["invoice_id"] = over.InvoiceID
			body["invoice_number"] = over.InvoiceNumber
		}
		apJSON(w, http.StatusUnprocessableEntity, body)
		return
	}

	switch {
	case errors.Is(err, ErrVendorNotFound), errors.Is(err, ErrInvoiceNotFound),
		errors.Is(err, ErrPaymentNotFound):
		apErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrApprovalRequired):
		apErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrIdempotencyConflict):
		apErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvoiceNotDraft), errors.Is(err, ErrInvoiceNotPosted),
		errors.Is(err, ErrInvoiceReversed), errors.Is(err, ErrHasAllocations),
		errors.Is(err, ErrPaymentReversed):
		apErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrPeriodClosed), errors.Is(err, ErrLineSumMismatch),
		errors.Is(err, ErrPPNRequiresPKP), errors.Is(err, ErrFakturRequired),
		errors.Is(err, ErrPPNProjectTagged), errors.Is(err, ErrRetentionExceedsDPP),
		errors.Is(err, ErrRetentionNotEnabled), errors.Is(err, ErrAdvanceNotEnabled),
		errors.Is(err, ErrLineAccountNotTaxonomy), errors.Is(err, ErrLineCreditMismatch),
		errors.Is(err, ErrNoLines), errors.Is(err, ErrDPPZeroOrNeg),
		errors.Is(err, ErrNegAmount), errors.Is(err, ErrAmountFrac),
		errors.Is(err, ErrVendorInactive), errors.Is(err, ErrVendorNameReq),
		errors.Is(err, ErrInvoiceNumberReq), errors.Is(err, ErrDueBeforeInvoice),
		errors.Is(err, ErrAccountNotFound),
		errors.Is(err, ErrCashAccount), errors.Is(err, ErrPaymentKind),
		errors.Is(err, ErrNoAllocations), errors.Is(err, ErrAllocationZero),
		errors.Is(err, ErrAllocationSumMismatch), errors.Is(err, ErrPaymentTouchesCost),
		errors.Is(err, ErrInvoiceWrongVendor), errors.Is(err, ErrDuplicateAllocTgt),
		errors.Is(err, ErrNothingToPay), errors.Is(err, ErrExcessNeedsFlag):
		apErr(w, http.StatusUnprocessableEntity, err.Error())
	default:
		// Aturan baris biaya milik package cost: errornya sudah menjelaskan
		// sendiri apa yang salah, jadi diteruskan sebagai kesalahan input.
		if isCostRuleError(err) {
			apErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		apErr(w, http.StatusInternalServerError, err.Error())
	}
}

// isCostRuleError menjawab apakah error berasal dari aturan baris biaya (A-1).
func isCostRuleError(err error) bool {
	for _, target := range []error{
		cost.ErrInvalidCostTier, cost.ErrInvalidCategory, cost.ErrTierCategoryMismatch,
		cost.ErrUnitRequiredForDirect, cost.ErrUnitNotAllowedForTier, cost.ErrProjectRequired,
		cost.ErrPhaseRequiresProject, cost.ErrBudgetLinkRequiresProject,
		cost.ErrCostAmountZeroOrNeg, cost.ErrCostAmountFractional,
		cost.ErrBudgetItemNotFound, cost.ErrBudgetItemProjectMismatch,
		cost.ErrBudgetItemCategoryMismatch, cost.ErrUnitNotInProject,
		cost.ErrUnitNotHPPEligible, cost.ErrUnitProductPolicyUnresolved,
		cost.ErrAccountNotFound,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// newBudgetItemLookup menjembatani budget.GORMRepository ke cost.BudgetItemLookup.
//
// Salinan sempit dari adapter di cost/handler.go — yang di sana tidak diekspor.
// Menjadikannya exported berarti mengubah permukaan package cost lebih jauh dari
// yang disetujui A-1 (satu tipe + satu method).
func newBudgetItemLookup(db *gorm.DB) cost.BudgetItemLookup {
	return &budgetItemLookup{repo: budget.NewGORMRepository(db)}
}

type budgetItemLookup struct{ repo *budget.GORMRepository }

func (a *budgetItemLookup) FindItemForCostValidation(
	ctx context.Context, tenantID, projectID, itemID uint64,
) (domain.CostCategory, error) {
	foundProjectID, cc, isMappable, err := a.repo.FindItemForCostLookup(ctx, tenantID, itemID)
	if errors.Is(err, budget.ErrItemNotFound) {
		return "", cost.ErrBudgetItemNotFound
	}
	if err != nil {
		return "", err
	}
	if foundProjectID != projectID {
		return "", cost.ErrBudgetItemProjectMismatch
	}
	if !isMappable {
		return "", cost.ErrBudgetItemCategoryMismatch
	}
	return cc, nil
}
