package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// ── Interfaces for testability ────────────────────────────────────────────────

// Querier is the read-only side of the ledger: reports and lookups.
// *QueryService implements this interface; tests may inject a mock.
type Querier interface {
	ListAccounts(ctx context.Context, tenantID uint64) ([]*Account, error)
	ListCashBankAccounts(ctx context.Context, tenantID uint64) ([]*Account, error)
	GetAccount(ctx context.Context, tenantID, id uint64) (*Account, error)
	ListJournals(ctx context.Context, tenantID uint64) ([]*JournalEntry, error)
	ListJournalSummaries(ctx context.Context, tenantID uint64, filter JournalFilter) ([]*JournalSummary, error)
	GetJournal(ctx context.Context, tenantID, id uint64) (*JournalEntry, error)
	// DocumentOf — bukti yang membuktikan sebuah jurnal (W-3.6). nil = non-kas.
	DocumentOf(ctx context.Context, tenantID, journalID uint64) (*JournalDocument, error)
	TrialBalance(ctx context.Context, tenantID uint64, asOf time.Time) (*TrialBalance, error)
	GeneralLedger(ctx context.Context, tenantID uint64, filter LedgerFilter) ([]LedgerEntry, error)
}

// AccountWriter handles account creation (subset of GORMRepository).
type AccountWriter interface {
	CreateAccount(ctx context.Context, a *Account) error
}

// AccountUpdater handles account mutation (subset of GORMRepository).
type AccountUpdater interface {
	UpdateAccount(ctx context.Context, tenantID, id uint64, name, description string, isActive bool) error
}

// DraftStore manages draft (unposted) journals (subset of JournalStore).
type DraftStore interface {
	DeleteDraft(ctx context.Context, tenantID, id uint64) error
}

// PeriodStore handles accounting period mutations.
type PeriodStore interface {
	ListPeriods(ctx context.Context, tenantID uint64) ([]*AccountingPeriod, error)
	UpsertPeriodClose(ctx context.Context, tenantID uint64, year, month int, closedBy uint64) (*AccountingPeriod, error)
	ReopenPeriod(ctx context.Context, tenantID uint64, year, month int) (*AccountingPeriod, error)
}

// ── Handler ───────────────────────────────────────────────────────────────────

// Handler wires the ledger HTTP routes.
type Handler struct {
	posting   *PostingService
	query     Querier
	accts     AccountWriter
	updater   AccountUpdater
	drafts    DraftStore
	periods   PeriodStore
	recurring *RecurringService // PS-5; nil pada NewHandlerWithServices (test)
	closing   *ClosingService
	// owners (W-8 · R-1): pembaca kepemilikan jurnal. Kosong berarti tidak ada
	// sub-ledger terdaftar — perilaku lama, pembalikan generik diizinkan.
	owners []JournalOwnershipReader
}

// NewHandler constructs the production ledger handler backed by GORM.
func NewHandler(db *gorm.DB) *Handler {
	repo := NewGORMRepository(db)
	qs := NewQueryService(db)
	// Closing posts year-end entries dated 31 Des. It uses a posting service
	// WITHOUT the monthly period guard so finalisasi tetap bisa diposting meski
	// periode bulanan sudah dikunci. Normal journals tetap dijaga `posting`.
	closingPosting := NewPostingService(repo, repo).WithJournalTx(repo)
	h := &Handler{
		posting: NewPostingService(repo, repo).WithPeriodChecker(repo).WithJournalTx(repo),
		query:   qs,
		accts:   repo,
		updater: repo,
		drafts:  repo,
		periods: repo,
		closing: NewClosingService(closingPosting, qs),
	}
	h.recurring = NewRecurringService(db, h.posting)
	return h
}

// NewHandlerWithServices constructs a ledger handler with injectable dependencies.
// Used in tests to inject in-memory mocks without a real database.
func NewHandlerWithServices(posting *PostingService, query Querier, accts AccountWriter, updater AccountUpdater, drafts DraftStore) *Handler {
	return &Handler{posting: posting, query: query, accts: accts, updater: updater, drafts: drafts, periods: nil}
}

// Mount registers all ledger routes on the provided router.
// The router must already have auth.Middleware applied upstream.
func (h *Handler) Mount(r chi.Router) {
	h.mountRecurring(r)
	r.Route("/accounts", func(r chi.Router) {
		r.Get("/", h.listAccounts)
		r.Get("/cash-bank", h.listCashBankAccounts) // COA-driven payment accounts
		r.With(auth.RequireWrite()).Post("/", h.createAccount)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getAccount)
			r.With(auth.RequireWrite()).Put("/", h.updateAccount)
		})
	})

	r.Route("/journals", func(r chi.Router) {
		r.Get("/", h.listJournals)
		r.With(auth.RequireWrite()).Post("/", h.createJournal)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getJournal)
			r.With(auth.RequireWrite()).Delete("/", h.deleteDraftJournal)
			r.Get("/document-choices", h.journalDocumentChoices)
			r.With(auth.RequireWrite()).Post("/post", h.postJournal)
			r.With(auth.RequireWrite()).Post("/reverse", h.reverseJournal)
		})
	})

	r.Route("/reports", func(r chi.Router) {
		r.Get("/trial-balance", h.trialBalance)
		r.Get("/ledger", h.generalLedger)
	})

	r.Route("/periods", func(r chi.Router) {
		r.Get("/", h.listPeriods)
		r.Route("/{year}/{month}", func(r chi.Router) {
			r.With(auth.RequireWrite()).Post("/close", h.closePeriod)
			r.With(auth.RequireRole("owner")).Post("/reopen", h.reopenPeriod)
		})
	})

	// Tutup buku tahunan (year-end closing entries).
	r.Route("/fiscal-years/{year}", func(r chi.Router) {
		r.Get("/closing", h.previewYearClose)
		r.With(auth.RequireRole("owner")).Post("/close", h.closeFiscalYear)
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func tenantIDFrom(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseUintParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func parseOptionalUint(r *http.Request, name string) *uint64 {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func parseOptionalDate(r *http.Request, name string) *time.Time {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil
	}
	return &t
}

// ── Account handlers ──────────────────────────────────────────────────────────

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	accounts, err := h.query.ListAccounts(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

// listCashBankAccounts mengembalikan akun tujuan pembayaran (aset aktif,
// kategori cash/bank) — sumber data dropdown Bank/Cash frontend.
func (h *Handler) listCashBankAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	accounts, err := h.query.ListCashBankAccounts(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

type createAccountRequest struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	NormalBalance   string `json:"normal_balance"`
	IsSystem        bool   `json:"is_system"`
	AccountCategory string `json:"account_category"` // cash|bank|other_asset (opsional)
	Description     string `json:"description"`
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Code == "" || req.Name == "" || req.Type == "" {
		writeError(w, http.StatusBadRequest, "code, name, type required")
		return
	}

	accType := domain.AccountType(req.Type)
	nb := domain.NormalBalance(req.NormalBalance)
	if nb == "" {
		nb = domain.NormalBalanceFor(accType)
	}

	// Kategori: validasi nilai; default berdasarkan tipe bila kosong.
	category := AccountCategory(req.AccountCategory)
	if !category.IsValid() {
		writeError(w, http.StatusBadRequest, ErrInvalidCategory.Error())
		return
	}
	if category == "" {
		category = DefaultCategoryFor(accType)
	}

	acc := &Account{
		TenantID:      tenantID,
		Code:          req.Code,
		Name:          req.Name,
		Type:          accType,
		NormalBalance: nb,
		IsSystem:      req.IsSystem,
		Category:      category,
		Description:   req.Description,
	}
	if err := h.accts.CreateAccount(r.Context(), acc); err != nil {
		if errors.Is(err, ErrAccountCodeDuplicate) {
			writeError(w, http.StatusConflict, "kode akun sudah digunakan")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, acc)
}

func (h *Handler) getAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	acc, err := h.query.GetAccount(r.Context(), tenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "akun tidak ditemukan")
		return
	}
	writeJSON(w, http.StatusOK, acc)
}

type updateAccountRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
}

func (h *Handler) updateAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if err := h.updater.UpdateAccount(r.Context(), tenantID, id, req.Name, req.Description, req.IsActive); err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			writeError(w, http.StatusNotFound, "akun tidak ditemukan")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	acc, err := h.query.GetAccount(r.Context(), tenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, acc)
}

// ── Journal handlers ──────────────────────────────────────────────────────────

func (h *Handler) listJournals(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	filter := JournalFilter{
		DateFrom: parseOptionalDate(r, "date_from"),
		DateTo:   parseOptionalDate(r, "date_to"),
		Source:   r.URL.Query().Get("source"),
	}
	summaries, err := h.query.ListJournalSummaries(r.Context(), tenantID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

type lineInputDTO struct {
	AccountID   uint64  `json:"account_id"`
	Debit       string  `json:"debit"`
	Credit      string  `json:"credit"`
	ProjectID   *uint64 `json:"project_id,omitempty"`
	PhaseID     *uint64 `json:"phase_id,omitempty"`
	UnitID      *uint64 `json:"unit_id,omitempty"`
	Description string  `json:"description"`
}

type createJournalDTO struct {
	Date        string         `json:"date"`
	Description string         `json:"description"`
	Reference   string         `json:"reference"`
	Source      string         `json:"source"` // optional; defaults to "manual"; allows "opening_balance"
	Lines       []lineInputDTO `json:"lines"`
}

func (h *Handler) createJournal(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto createJournalDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	date, err := time.Parse("2006-01-02", dto.Date)
	if err != nil {
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	lines, err := parseLinesDTO(dto.Lines)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	source := "manual"
	if dto.Source == "opening_balance" {
		source = "opening_balance"
	}
	userID, _ := auth.UserIDFrom(r.Context())
	entry, err := h.posting.Create(r.Context(), CreateJournalRequest{
		TenantID:    tenantID,
		Date:        date,
		Description: dto.Description,
		Reference:   dto.Reference,
		Lines:       lines,
		Source:      source,
		CreatedBy:   &userID,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if isDomainError(err) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (h *Handler) getJournal(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	entry, err := h.query.GetJournal(r.Context(), tenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "jurnal tidak ditemukan")
		return
	}
	// S9/R-9: total debit/kredit dihitung backend (decimal) — FE hanya display.
	totalDebit, totalCredit := domain.Zero, domain.Zero
	for _, l := range entry.Lines {
		totalDebit = totalDebit.Add(l.Debit)
		totalCredit = totalCredit.Add(l.Credit)
	}
	// W-3.6: nomor bukti ikut dikirim supaya detail jurnal bisa menampilkan
	// "jurnal ini dibuktikan oleh apa" tanpa fetch kedua dari layar.
	doc, err := h.query.DocumentOf(r.Context(), tenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var docNumber, docType, docTypeName, docIssuedAt string
	if doc != nil {
		docNumber, docType, docTypeName, docIssuedAt = doc.Number, doc.TypeCode, doc.TypeName, doc.IssuedAt
	}
	writeJSON(w, http.StatusOK, struct {
		*JournalEntry
		TotalDebit      string `json:"total_debit"`
		TotalCredit     string `json:"total_credit"`
		DocumentNumber  string `json:"document_number,omitempty"`
		DocumentType    string `json:"document_type_code,omitempty"`
		DocumentTypeNm  string `json:"document_type_name,omitempty"`
		DocumentIssueAt string `json:"document_issued_at,omitempty"`
	}{entry, totalDebit.String(), totalCredit.String(), docNumber, docType, docTypeName, docIssuedAt})
}

func (h *Handler) deleteDraftJournal(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.drafts.DeleteDraft(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, ErrJournalAlreadyPosted) {
			writeError(w, http.StatusConflict, "jurnal sudah diposting — tidak bisa dihapus, gunakan pembalik")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) postJournal(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	// W-3.2 (B-4/D-W3-4): endpoint ini dulu tidak sadar kas sama sekali —
	// jurnal manual yang mengkredit bank bisa diposting tanpa bukti apa pun.
	// Body opsional; hanya jurnal yang menyentuh kas yang diperiksa.
	var body postJournalDTO
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body) // body kosong = tidak memilih
	}
	draft, err := h.posting.Draft(r.Context(), tenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "jurnal tidak ditemukan")
		return
	}
	defaultSpec, err := h.posting.EntryCashSpec(r.Context(), tenantID, draft)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	spec, err := ResolveManualDocument(body.DocumentType, defaultSpec)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   err.Error(),
			"choices": ManualDocumentChoices(defaultSpec),
		})
		return
	}

	entry, err := h.posting.PostDraft(r.Context(), tenantID, id, spec)
	if err != nil {
		if errors.Is(err, ErrJournalAlreadyPosted) {
			writeError(w, http.StatusConflict, "jurnal sudah diposting")
			return
		}
		status := http.StatusInternalServerError
		if isDomainError(err) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// postJournalDTO membawa pilihan jenis dokumen kas saat memposting draft.
type postJournalDTO struct {
	DocumentType string `json:"document_type"`
}

// journalDocumentChoices memberi tahu frontend apakah sebuah draft memerlukan
// jenis dokumen sebelum diposting, dan apa saja pilihannya. Tanpa endpoint ini
// frontend harus menebak aturan kas — dan tebakan yang meleset baru ketahuan
// sebagai error 400 di tengah alur posting.
func (h *Handler) journalDocumentChoices(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	draft, err := h.posting.Draft(r.Context(), tenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "jurnal tidak ditemukan")
		return
	}
	spec, err := h.posting.EntryCashSpec(r.Context(), tenantID, draft)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	choices := ManualDocumentChoices(spec)
	writeJSON(w, http.StatusOK, map[string]any{
		"touches_cash": len(choices) > 0,
		"required":     len(choices) > 1,
		"default":      spec.TypeCode,
		"choices":      choices,
	})
}

type reverseRequest struct {
	Date string `json:"date"`
}

// ── R-1 (W-8) — kepemilikan jurnal ────────────────────────────────────────────

// JournalOwnerInfo menerangkan siapa pemilik sebuah jurnal dan lewat mana
// pembatalannya harus dilakukan.
type JournalOwnerInfo struct {
	// Owner adalah domain pemiliknya, mis. "penjualan".
	Owner string
	// Hint adalah jalur sah membatalkannya, ditulis untuk dibaca operator.
	Hint string
}

// JournalOwnershipReader menjawab apakah sebuah jurnal dimiliki sub-ledger.
// Mengembalikan nil bila jurnal itu tidak dimiliki siapa pun (jurnal manual).
type JournalOwnershipReader interface {
	FindJournalOwner(ctx context.Context, tenantID, journalID uint64) (*JournalOwnerInfo, error)
}

// AddJournalOwnership mendaftarkan satu pembaca kepemilikan (W-8, R-1).
//
// Alasannya bukan tata tertib melainkan rekonsiliasi: jurnal BAST, penerimaan
// termin, dan reklas piutang punya baris pendamping di sub-ledger penjualan.
// Membalikkannya lewat endpoint jurnal umum menggerakkan buku besar TANPA
// menyentuh baris pendamping itu — GL turun, sub-ledger tidak, dan INV-AR-4
// menjadi selisih yang tidak bisa ditutup lewat jalan apa pun kecuali menjurnal
// karangan. Yang ditolak hanyalah PINTU UMUM-nya; jalur domain (pembatalan
// penjualan) tetap memanggil PostingService.Reverse yang sama, karena di sana
// pembalik dan sub-ledger bergerak dalam satu transaksi.
func (h *Handler) AddJournalOwnership(r JournalOwnershipReader) {
	if r != nil {
		h.owners = append(h.owners, r)
	}
}

// journalOwner mencari pemilik jurnal pada seluruh pembaca terdaftar.
//
// Kegagalan pembaca TIDAK diperlakukan sebagai "tidak ada pemilik": kalau
// pertanyaan kepemilikan tidak bisa dijawab, penjaga ini harus menutup, bukan
// membuka. Fail-open di sini berarti gangguan database sesaat cukup untuk
// melubangi ledger.
func (h *Handler) journalOwner(ctx context.Context, tenantID, journalID uint64) (*JournalOwnerInfo, error) {
	for _, o := range h.owners {
		info, err := o.FindJournalOwner(ctx, tenantID, journalID)
		if err != nil {
			return nil, err
		}
		if info != nil {
			return info, nil
		}
	}
	return nil, nil
}

func (h *Handler) reverseJournal(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req reverseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	reverseDate, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	// R-1: jurnal yang punya sub-ledger tidak boleh dibalik dari pintu umum.
	owner, err := h.journalOwner(r.Context(), tenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cek kepemilikan jurnal: "+err.Error())
		return
	}
	if owner != nil {
		writeError(w, http.StatusConflict,
			"jurnal ini milik domain "+owner.Owner+" dan punya catatan pendamping di sub-ledger. "+
				"Membalikkannya dari sini akan membuat buku besar dan sub-ledger berbeda. "+owner.Hint)
		return
	}

	rev, err := h.posting.Reverse(r.Context(), tenantID, id, reverseDate)
	if err != nil {
		if errors.Is(err, ErrJournalNotPosted) {
			writeError(w, http.StatusBadRequest, "jurnal belum diposting")
			return
		}
		if errors.Is(err, ErrJournalAlreadyReversed) {
			writeError(w, http.StatusConflict, "jurnal sudah memiliki entri pembalik")
			return
		}
		// R-2 (W-8): pembalik bertanggal periode tertutup ditolak — bukan
		// kegagalan server, melainkan aturan periode. Jalan keluarnya ada di
		// pesan supaya operator tidak menyimpulkan koreksinya mustahil.
		if errors.Is(err, ErrPeriodClosed) {
			writeError(w, http.StatusConflict,
				"periode akuntansi pada tanggal pembalik sudah ditutup — beri tanggal pembalik pada periode berjalan")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rev)
}

// ── Report handlers ───────────────────────────────────────────────────────────

func (h *Handler) trialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := time.Now()
	if d := r.URL.Query().Get("as_of"); d != "" {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			writeError(w, http.StatusBadRequest, "as_of must be YYYY-MM-DD")
			return
		}
		asOf = t
	}
	tb, err := h.query.TrialBalance(r.Context(), tenantID, asOf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tb)
}

func (h *Handler) generalLedger(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	filter := LedgerFilter{
		AccountID: parseOptionalUint(r, "account_id"),
		ProjectID: parseOptionalUint(r, "project_id"),
		PhaseID:   parseOptionalUint(r, "phase_id"),
		UnitID:    parseOptionalUint(r, "unit_id"),
		DateFrom:  parseOptionalDate(r, "date_from"),
		DateTo:    parseOptionalDate(r, "date_to"),
	}
	entries, err := h.query.GeneralLedger(r.Context(), tenantID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// ── private parsing helpers ───────────────────────────────────────────────────

func parseLinesDTO(dtos []lineInputDTO) ([]LineInput, error) {
	lines := make([]LineInput, len(dtos))
	for i, d := range dtos {
		debit, err := parseMoneyString(d.Debit)
		if err != nil {
			return nil, fmt.Errorf("baris %d debit: %w", i, err)
		}
		credit, err := parseMoneyString(d.Credit)
		if err != nil {
			return nil, fmt.Errorf("baris %d credit: %w", i, err)
		}
		lines[i] = LineInput{
			AccountID:   d.AccountID,
			Debit:       debit,
			Credit:      credit,
			ProjectID:   d.ProjectID,
			PhaseID:     d.PhaseID,
			UnitID:      d.UnitID,
			Description: d.Description,
		}
	}
	return lines, nil
}

func parseMoneyString(s string) (domain.Money, error) {
	if s == "" || s == "0" {
		return domain.Zero, nil
	}
	return domain.NewMoney(s)
}

func isDomainError(err error) bool {
	return errors.Is(err, ErrJournalNotBalanced) ||
		errors.Is(err, ErrAmountNotWholeRupiah) ||
		errors.Is(err, ErrTooFewLines) ||
		errors.Is(err, ErrLineInvalid) ||
		errors.Is(err, ErrLineNegative) ||
		errors.Is(err, ErrAccountNotFound) ||
		errors.Is(err, ErrPeriodClosed)
}

// ── Period handlers ───────────────────────────────────────────────────────────

func (h *Handler) listPeriods(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if h.periods == nil {
		writeJSON(w, http.StatusOK, []*AccountingPeriod{})
		return
	}
	periods, err := h.periods.ListPeriods(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, periods)
}

func parseYearMonth(r *http.Request) (int, int, error) {
	year, err := strconv.Atoi(chi.URLParam(r, "year"))
	if err != nil || year < 2000 || year > 2100 {
		return 0, 0, fmt.Errorf("year tidak valid")
	}
	month, err := strconv.Atoi(chi.URLParam(r, "month"))
	if err != nil || month < 1 || month > 12 {
		return 0, 0, fmt.Errorf("month tidak valid (1-12)")
	}
	return year, month, nil
}

func parseYear(r *http.Request) (int, error) {
	year, err := strconv.Atoi(chi.URLParam(r, "year"))
	if err != nil || year < 2000 || year > 2100 {
		return 0, fmt.Errorf("year tidak valid")
	}
	return year, nil
}

// previewYearClose mengembalikan rencana tutup buku tahun (tanpa memposting).
func (h *Handler) previewYearClose(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	year, err := parseYear(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.closing == nil {
		writeError(w, http.StatusInternalServerError, "closing service not configured")
		return
	}
	preview, err := h.closing.PreviewYearClose(r.Context(), tenantID, year)
	if err != nil {
		if errors.Is(err, ErrClosingAccountsMissing) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// closeFiscalYear memposting entri penutup tahun (owner only).
func (h *Handler) closeFiscalYear(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	year, err := parseYear(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.closing == nil {
		writeError(w, http.StatusInternalServerError, "closing service not configured")
		return
	}
	userID, _ := auth.UserIDFrom(r.Context())
	result, err := h.closing.CloseFiscalYear(r.Context(), tenantID, year, userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrYearAlreadyClosed):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrNothingToClose):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrClosingAccountsMissing):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) closePeriod(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	year, month, err := parseYearMonth(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID, _ := auth.UserIDFrom(r.Context())
	if h.periods == nil {
		writeError(w, http.StatusInternalServerError, "period store not configured")
		return
	}
	period, err := h.periods.UpsertPeriodClose(r.Context(), tenantID, year, month, userID)
	if err != nil {
		if errors.Is(err, ErrPeriodAlreadyClosed) {
			writeError(w, http.StatusConflict, "periode sudah ditutup")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, period)
}

func (h *Handler) reopenPeriod(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	year, month, err := parseYearMonth(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.periods == nil {
		writeError(w, http.StatusInternalServerError, "period store not configured")
		return
	}
	period, err := h.periods.ReopenPeriod(r.Context(), tenantID, year, month)
	if err != nil {
		if errors.Is(err, ErrPeriodNotFound) {
			writeError(w, http.StatusNotFound, "periode tidak ditemukan")
			return
		}
		if errors.Is(err, ErrPeriodNotClosed) {
			writeError(w, http.StatusConflict, "periode belum ditutup")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, period)
}
