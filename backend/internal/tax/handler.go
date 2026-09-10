package tax

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/auth"
)

// Handler menyediakan HTTP handler untuk Phase 7 (PPh Final Pengalihan).
type Handler struct {
	svc   *Service
	rules *GORMRepository // Increment 4: listing Tax Rules
}

// NewHandler membuat Handler lengkap dengan GORMRepository yang sudah diwire.
// Phase 8: repo juga mengimplementasi BASTReader dan LedgerAccountReader.
func NewHandler(db *gorm.DB) *Handler {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	repo := NewGORMRepository(db, posting)
	svc := NewService(repo, repo, repo, repo,
		WithBASTReader(repo),
		WithLedgerReader(repo),
		WithRuleResolution(repo, repo), // Increment 4: tarif per kategori proyek
		WithUnitProductPolicy(repo),    // rule klien UAT #3: tarif per PRODUK unit
		WithTxRunner(repo),             // W-3.0: PayTax atomik
	)
	return &Handler{svc: svc, rules: repo}
}

// Svc mengekspos tax.Service untuk wiring lintas-modul (S7: reporting membaca
// laporan pajak lewat pembaca kanonik tax.GetTaxReport, bukan SQL sendiri).
func (h *Handler) Svc() *Service { return h.svc }

// Mount mendaftarkan semua route Phase 7 ke router chi.
// Semua route membutuhkan JWT middleware yang sudah dipasang di parent router.
func (h *Handler) Mount(r chi.Router) {
	// Konfigurasi tarif (admin).
	//
	// RequireWrite di tiga route tulis di bawah: sebelum ini modul pajak adalah
	// satu-satunya modul yang membiarkan route tulisnya tanpa penjaga peran.
	// Marketing tetap tertolak karena allow-list router (fail-closed), tetapi
	// Viewer — peran baca — lolos sampai ke business logic dan bisa memasang
	// tarif serta memposting jurnal akrual/pelunasan pajak. Penjaga ini
	// menyamakan pajak dengan modul lain; tidak ada aturan pajak yang berubah.
	r.With(auth.RequireWrite()).Post("/tax/tax-rates", h.setTaxRate)
	r.Get("/tax/tax-rates", h.listTaxRates)

	// Akrual dan pelunasan per unit
	r.With(auth.RequireWrite()).Post("/units/{unitID}/tax/accrue", h.accrueTax)
	r.With(auth.RequireWrite()).Post("/tax/obligations/{id}/pay", h.payTax)
	r.Get("/tax/obligations/{id}", h.getObligation)

	// Laporan per periode
	r.Get("/tax/report", h.getTaxReport)

	// Phase 8: laporan PPN & penjaga audit
	r.Get("/tax/bast-without-pph", h.listBASTWithoutPPhFinal)
	r.Get("/tax/vat-report", h.getVATReport)
	r.Get("/tax/combined-report", h.getCombinedTaxReport)
}

// ── POST /tax/tax-rates ───────────────────────────────────────────────────────

type setTaxRateRequest struct {
	RateCode      string `json:"rate_code"`
	Name          string `json:"name"`
	Rate          string `json:"rate"`           // string agar tidak ada float64
	AppliesTo     string `json:"applies_to"`     // all|subsidi|komersial (kosong = all)
	TriggerEvent  string `json:"trigger_event"`  // kosong = bast
	CalcBase      string `json:"calc_base"`      // kosong = transfer_value
	Formula       string `json:"formula"`        // kosong = proportional
	DebitAccount  string `json:"debit_account"`  // kosong = default (5-2000)
	CreditAccount string `json:"credit_account"` // kosong = default (2-4000)
	EffectiveFrom string `json:"effective_from"` // RFC3339 / 2006-01-02
	EffectiveTo   string `json:"effective_to"`   // opsional
	Description   string `json:"description"`
}

func (h *Handler) setTaxRate(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)

	var req setTaxRateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body tidak valid: "+err.Error())
		return
	}

	rate, err := decimal.NewFromString(req.Rate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "rate tidak valid: "+err.Error())
		return
	}
	effDate, err := parseFlexDate(req.EffectiveFrom)
	if err != nil {
		writeError(w, http.StatusBadRequest, "effective_from tidak valid: "+err.Error())
		return
	}

	svcReq := SetTaxRateRequest{
		RateCode:      req.RateCode,
		Name:          req.Name,
		Rate:          rate,
		AppliesTo:     RuleAppliesTo(req.AppliesTo),
		TriggerEvent:  TriggerEvent(req.TriggerEvent),
		CalcBase:      req.CalcBase,
		Formula:       FormulaType(req.Formula),
		DebitAccount:  req.DebitAccount,
		CreditAccount: req.CreditAccount,
		EffectiveFrom: effDate,
		Description:   req.Description,
	}
	if req.EffectiveTo != "" {
		effTo, terr := parseFlexDate(req.EffectiveTo)
		if terr != nil {
			writeError(w, http.StatusBadRequest, "effective_to tidak valid: "+terr.Error())
			return
		}
		svcReq.EffectiveTo = &effTo
	}
	if err := h.svc.SetTaxRate(r.Context(), tenantID, svcReq); err != nil {
		writeValidationOrInternal(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// ── GET /tax/tax-rates ────────────────────────────────────────────────────────

func (h *Handler) listTaxRates(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)
	rules, err := h.rules.ListRules(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rules})
}

// ── POST /units/{unitID}/tax/accrue ──────────────────────────────────────────

type accrueTaxRequest struct {
	TransferValue string  `json:"transfer_value"` // rupiah bulat, string
	AccrualDate   string  `json:"accrual_date"`   // RFC3339 / 2006-01-02; opsional
	RateCode      string  `json:"rate_code"`      // opsional; default pph_final_pengalihan
	ProjectID     *uint64 `json:"project_id,omitempty"`
}

func (h *Handler) accrueTax(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)
	unitID, err := parseUintParam(r, "unitID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "unit_id tidak valid")
		return
	}

	var req accrueTaxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body tidak valid: "+err.Error())
		return
	}

	transferValue, err := domain.NewMoney(req.TransferValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "transfer_value tidak valid: "+err.Error())
		return
	}

	var accrualDate time.Time
	if req.AccrualDate != "" {
		accrualDate, err = parseFlexDate(req.AccrualDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "accrual_date tidak valid: "+err.Error())
			return
		}
	}

	svcReq := AccrueTaxRequest{
		RateCode:      req.RateCode,
		TransferValue: transferValue,
		AccrualDate:   accrualDate,
		UnitID:        &unitID,
		ProjectID:     req.ProjectID,
	}

	obl, err := h.svc.AccrueTax(r.Context(), tenantID, svcReq)
	if err != nil {
		writeValidationOrInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, obl)
}

// ── POST /tax/obligations/{id}/pay ────────────────────────────────────────────

type payTaxRequest struct {
	BankAccountCode string `json:"bank_account_code"`
	PaymentDate     string `json:"payment_date"` // opsional
}

func (h *Handler) payTax(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)
	oblID, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id obligation tidak valid")
		return
	}

	var req payTaxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body tidak valid: "+err.Error())
		return
	}

	var payDate time.Time
	if req.PaymentDate != "" {
		payDate, err = parseFlexDate(req.PaymentDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "payment_date tidak valid: "+err.Error())
			return
		}
	}

	svcReq := PayTaxRequest{
		ObligationID:    oblID,
		BankAccountCode: req.BankAccountCode,
		PaymentDate:     payDate,
	}

	payment, err := h.svc.PayTax(r.Context(), tenantID, svcReq)
	if err != nil {
		writeValidationOrInternal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, payment)
}

// ── GET /tax/obligations/{id} ─────────────────────────────────────────────────

func (h *Handler) getObligation(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)
	oblID, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id tidak valid")
		return
	}

	obl, err := h.svc.store.FindObligationByID(r.Context(), tenantID, oblID)
	if err != nil {
		writeValidationOrInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, obl)
}

// ── GET /tax/report ───────────────────────────────────────────────────────────

func (h *Handler) getTaxReport(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)
	q := r.URL.Query()

	from, err := parseFlexDate(q.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "from tidak valid (gunakan YYYY-MM-DD)")
		return
	}
	to, err := parseFlexDate(q.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "to tidak valid (gunakan YYYY-MM-DD)")
		return
	}

	report, err := h.svc.GetTaxReport(r.Context(), tenantID, from, to)
	if err != nil {
		writeValidationOrInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ── GET /tax/bast-without-pph ─────────────────────────────────────────────────

// listBASTWithoutPPhFinal mengembalikan unit yang sudah BAST dalam periode
// tetapi belum ada akrual PPh Final. Penjaga audit untuk operator.
// Query params: from, to (YYYY-MM-DD atau RFC3339).
func (h *Handler) listBASTWithoutPPhFinal(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)
	q := r.URL.Query()

	from, err := parseFlexDate(q.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "from tidak valid (gunakan YYYY-MM-DD)")
		return
	}
	to, err := parseFlexDate(q.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "to tidak valid (gunakan YYYY-MM-DD)")
		return
	}

	items, err := h.svc.ListBASTWithoutPPhFinal(r.Context(), tenantID, from, to)
	if err != nil {
		writeValidationOrInternal(w, err)
		return
	}
	if items == nil {
		items = []BASTWithoutPPhFinalItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

// ── GET /tax/vat-report ───────────────────────────────────────────────────────

// getVATReport mengembalikan laporan PPN per periode.
// PPNKeluaran direkonsiliasi ke Σ kredit 2-3000 di ledger.
// Query params: from, to (YYYY-MM-DD atau RFC3339).
func (h *Handler) getVATReport(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)
	q := r.URL.Query()

	from, err := parseFlexDate(q.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "from tidak valid (gunakan YYYY-MM-DD)")
		return
	}
	to, err := parseFlexDate(q.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "to tidak valid (gunakan YYYY-MM-DD)")
		return
	}

	report, err := h.svc.GetVATReport(r.Context(), tenantID, from, to)
	if err != nil {
		writeValidationOrInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ── GET /tax/combined-report ──────────────────────────────────────────────────

// getCombinedTaxReport mengembalikan laporan kewajiban pajak gabungan
// (PPh Final + PPN) dalam satu respons.
// Query params: from, to (YYYY-MM-DD atau RFC3339).
func (h *Handler) getCombinedTaxReport(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIDFromCtx(r)
	q := r.URL.Query()

	from, err := parseFlexDate(q.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "from tidak valid (gunakan YYYY-MM-DD)")
		return
	}
	to, err := parseFlexDate(q.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "to tidak valid (gunakan YYYY-MM-DD)")
		return
	}

	report, err := h.svc.GetCombinedTaxReport(r.Context(), tenantID, from, to)
	if err != nil {
		writeValidationOrInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func tenantIDFromCtx(r *http.Request) uint64 {
	id, _ := auth.TenantIDFrom(r.Context())
	return id
}

func parseUintParam(r *http.Request, key string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, key), 10, 64)
}

// parseFlexDate menerima RFC3339 atau "2006-01-02".
func parseFlexDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeValidationOrInternal mengklasifikasi error domain sebagai 422 atau 500.
func writeValidationOrInternal(w http.ResponseWriter, err error) {
	domainErrs := []error{
		ErrTransferValueFractional, ErrTransferValueZeroOrNeg,
		ErrObligationAlreadyPaid, ErrPaymentAmountMismatch,
		ErrInvalidBankAccount, ErrRateNotConfigured,
		ErrRateCodeRequired, ErrRateValueInvalid,
		ErrEffectiveDateRequired, ErrObligationAmountZero,
		ErrObligationNotFound,
	}
	for _, de := range domainErrs {
		if errors.Is(err, de) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
	}
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
