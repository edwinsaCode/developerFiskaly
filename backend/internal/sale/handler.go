package sale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/budget"
	"esaproperti/internal/customer"
	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/project"
	"esaproperti/internal/salesorg"
	"esaproperti/internal/scheme"
	"esaproperti/internal/tax"
)

// InvoiceStatusUpdater is called after a schedule is marked received.
// Implemented by billing.Service; defined here to avoid circular import
// (same pattern as PPhFinalAccruer in this package).
type InvoiceStatusUpdater interface {
	MarkPaidByScheduleID(ctx context.Context, tenantID, scheduleID uint64) error
}

// ReceiptGenerator membuat kwitansi untuk sebuah termin (idempoten).
// Diimplementasikan oleh adapter ke billing.ReceiptService di wiring layer —
// sale tidak import billing (hindari circular import).
type ReceiptGenerator interface {
	GenerateReceipt(ctx context.Context, tenantID, createdBy, terminID uint64, notes string) (receiptNumber string, receiptID uint64, err error)
}

// Handler menyediakan endpoint HTTP untuk penjualan unit (Event 2, 3, 4).
type Handler struct {
	svc      *Service
	repo     *GORMRepository // untuk wiring tx-aware collaborator (committer atomik)
	vatRates VATRateProvider // opsional; auto-lookup PPN saat BAST PKP
}

// Svc exposes the underlying service for cross-package wiring (mis. SetUnitTransitioner).
func (h *Handler) Svc() *Service { return h.svc }

// JournalOwnership mengekspos pembaca kepemilikan jurnal domain penjualan
// (W-8 · R-1) untuk didaftarkan ke handler ledger.
func (h *Handler) JournalOwnership() *GORMRepository { return h.repo }

// SetInvoiceUpdater meneruskan invoice updater ke Service (ReceivePayment yang memakai).
// Juga memasang varian tx-aware ke committer atomik bila adapter mendukung.
func (h *Handler) SetInvoiceUpdater(u InvoiceStatusUpdater) {
	h.svc.SetInvoiceUpdater(u)
	if tx, ok := u.(InvoiceTxUpdater); ok && h.repo != nil {
		h.repo.SetInvoiceTxUpdater(tx)
	}
}

// SetRealizationRecognizer memasang pengakuan piutang biaya realisasi (W-5) ke
// committer atomik: invoice + jurnalnya terbit di dalam transaksi BAST yang sama.
func (h *Handler) SetRealizationRecognizer(r RealizationRecognizer) {
	if h.repo != nil {
		h.repo.SetRealizationRecognizer(r)
	}
}

// SetReceiptGenerator meneruskan receipt generator ke Service.
func (h *Handler) SetReceiptGenerator(g ReceiptGenerator) {
	h.svc.SetReceiptGenerator(g)
	if tx, ok := g.(ReceiptTxGenerator); ok && h.repo != nil {
		h.repo.SetReceiptTxGenerator(tx)
	}
}

// taxVATAdapter mengadaptasi tax.GORMRepository ke interface VATRateProvider
// yang dibutuhkan sale package, tanpa circular import (sale tidak import tax).
type taxVATAdapter struct {
	repo *tax.GORMRepository
}

func (a *taxVATAdapter) GetVATRate(ctx context.Context, tenantID uint64, rateCode string, referenceDate time.Time) (decimal.Decimal, error) {
	rate, err := a.repo.GetCurrentRate(ctx, tenantID, rateCode, referenceDate)
	if err != nil {
		return decimal.Zero, fmt.Errorf("GetVATRate: %w", err)
	}
	return rate.Rate, nil
}

// NewHandler membangun production handler yang terhubung ke GORM.
func NewHandler(db *gorm.DB) *Handler {
	ledgerRepo := ledger.NewGORMRepository(db)
	postingSvc := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)

	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo,
		allocation.WithLandPoolSource(allocRepo),
		allocation.WithHardPoolSource(allocRepo, allocRepo),
	)

	taxRepo := tax.NewGORMRepository(db, postingSvc)

	repo := NewGORMRepository(db, postingSvc, allocSvc)
	repo.SetTaxAccruer(taxRepo) // wire PPh Final auto-accrual at BAST
	// Committer atomik: seluruh penerimaan (jurnal+termin+alokasi+cache+kwitansi)
	// dalam satu transaksi (guard #2/#4). Kwitansi & invoice tx-aware dipasang
	// belakangan via SetReceiptGenerator/SetInvoiceUpdater (wiring main.go).
	//
	// Increment 3: scheme flow AKTIF di produksi — kontrak baru wajib
	// payment_scheme_id + customer_id + sales_person_id (approval note #4).
	parties := &partyLookupAdapter{
		customers: customer.NewGORMRepository(db),
		persons:   salesorg.NewGORMRepository(db),
	}
	// P0-2/P0-3 Budgeted Cost Allocation — resolver WAJIB terpasang di produksi
	// agar BAST proyek ber-RAB memakai HPP budgeted + snapshot (fix P0-4: wiring
	// ini sebelumnya hanya ada di test). Versioning basis D1 ikut di-pin.
	allocSvcVersioned := allocation.NewService(allocRepo, allocRepo, allocRepo, allocation.WithVersionStore(allocRepo), allocation.WithLandPoolSource(allocRepo))
	budgetSvc := budget.NewService(budget.NewGORMRepository(db), budget.NewGORMRealisasiProvider(db))
	resolver := NewBudgetedHPPResolver(budgetSvc, allocSvcVersioned, repo)
	resolver.SetConfigVersionSource(allocSvcVersioned)

	// kelebihan-tanah-booking-integration-2026-08: land.Service dikonstruksi
	// independen (bukan reuse land.Handler) — sale hanya butuh PrepareBundledAkad
	// (seam LandAkadPreparer); tidak import land.Handler (menghindari coupling ke
	// HTTP routes-nya). HPP resolver = PurchasePriceLandHPPResolver (koreksi
	// klien 2026-08-31, identik wiring land.NewHandler) — lihat
	// land/hpp_resolver.go.
	landRepo := land.NewGORMRepository(db)
	landResolver := land.NewPurchasePriceLandHPPResolver(landRepo)
	// PPh Final Pengalihan Kelebihan Tanah — otomatis saat Akad, tax engine
	// yang SAMA dipakai unit/BAST (taxRepo di atas), bukan engine kedua. Land
	// hanya butuh ResolveAccrualPlan (baca tarif/rule, tidak menulis) sebelum
	// transaksi Akad dibuka, jadi tax.Service non-tx (bukan tx-scoped seperti
	// AccruePPhFinalInTx) sudah cukup — posting jurnalnya sendiri terjadi
	// DALAM transaksi Akad milik land (RecordAkadTx), bukan di sini.
	landTaxSvc := tax.NewService(taxRepo, taxRepo, taxRepo, taxRepo,
		tax.WithRuleResolution(taxRepo, taxRepo),
		tax.WithUnitProductPolicy(taxRepo))
	landSvc := land.NewService(landRepo, land.WithHPPResolver(landResolver), land.WithPPhResolver(landTaxSvc))

	svc := NewService(repo, repo, repo, repo, repo, repo,
		WithContractStore(repo), WithPaymentCommitter(repo),
		WithSchemeFlow(repo, scheme.DefaultRegistry(), parties),
		WithBookingStore(repo),
		WithHouseARStore(repo),     // W-8: piutang harga rumah dibaca dari BAST + termin
		WithBillingPlanStore(repo), // W-8/P6: jadwal pra-BAST (komplemen house AR)
		// W-8: sisi buku besar dari tie-out. Pembaca saldo yang SAMA dengan yang
		// dipakai neraca dan rekonsiliasi piutang lama — bukan SUM() sendiri.
		WithHouseARLedger(ledger.NewLedgerBalanceService(ledger.NewQueryService(db))),
		WithHPPResolver(resolver),
		WithHandoverWriter(repo), // Temuan #7: serah terima fisik, terpisah dari Akad
		WithLandAkadPreparer(landSvc))
	// P1 (2026-09-04): reuse instance landSvc yang sama untuk membaca rincian
	// land_sale (m²/harga satuan) pada Customer Statement — bukan seam/service kedua.
	svc.SetLandSaleReader(landSvc)
	return &Handler{
		svc:      svc,
		repo:     repo,
		vatRates: &taxVATAdapter{repo: taxRepo},
	}
}

// Mount mendaftarkan semua route penjualan.
func (h *Handler) Mount(r chi.Router) {
	// Event 2 — penerimaan termin/uang muka
	r.Route("/units/{unitID}/termins", func(r chi.Router) {
		r.Get("/", h.listTermins)
		r.With(auth.RequireWrite()).Post("/", h.recordTermin)
	})

	// Event 3+4 — Akad (pengakuan pendapatan + HPP, atomik) — Temuan #7: dulu
	// dipicu BAST ("/bast"); endpoint di-rename karena tidak ada konsumen
	// produksi lama (frontend di-update sepaket, Increment E).
	r.With(auth.RequireWrite()).Post("/units/{unitID}/akad", h.recordAkad)

	// Serah terima fisik (Temuan #7) — TANPA jurnal, TERPISAH dari Akad.
	r.With(auth.RequireWrite()).Post("/units/{unitID}/physical-handover", h.recordPhysicalHandover)

	// Increment 7 — Booking (fee = titipan, konsumen Unit Lifecycle)
	// W-12: booking & pembatalannya adalah pekerjaan marketing.
	r.With(auth.RequireSalesWrite()).Post("/units/{unitID}/bookings", h.createBooking)
	r.Get("/units/{unitID}/booking", h.getActiveBooking)
	r.Route("/bookings", func(r chi.Router) {
		r.Get("/", h.listBookings)
		r.With(auth.RequireWrite()).Post("/mark-expired", h.markExpiredBookings)
		r.Route("/{bookingID}", func(r chi.Router) {
			r.Get("/", h.getBooking)
			r.With(auth.RequireSalesWrite()).Post("/cancel", h.cancelBooking)
			// Item 3: transfer booking active ke unit lain (tanpa jurnal baru).
			r.With(auth.RequireSalesWrite()).Post("/transfer", h.transferBooking)
			// R4 Opsi A: disposisi fee outside-price pasca-konversi.
			r.With(auth.RequireWrite()).Post("/fee-disposition", h.disposeBookingFee)
		})
	})

	// Read sale record
	r.Get("/units/{unitID}/sale-record", h.getSaleRecord)

	// Unit's active sale contract (used by billing frontend)
	r.Get("/units/{unitID}/contract", h.getUnitContract)

	// PaymentSchedule (Phase 7)
	// W-12: menutup penjualan (kontrak + jadwalnya) adalah pekerjaan marketing.
	// Yang TIDAK ikut: scheme-events, scheme-convert, BAST, dan seluruh jalur
	// penerimaan uang — semuanya memposting jurnal kas.
	r.With(auth.RequireSalesWrite()).Post("/sale-contracts", h.createContract)
	// P1 — Sales ≠ Admin Marketing: penugasan/perubahan penanggung jawab
	// administrasi kontrak, independen dari sales_person_id (komisi tetap sales).
	r.With(auth.RequireSalesWrite()).Put("/sale-contracts/{contractID}/admin-marketing", h.setAdminMarketing)
	r.With(auth.RequireSalesWrite()).Post("/sale-contracts/{contractID}/schedule", h.createSchedule)
	r.Get("/sale-contracts/{contractID}/schedule", h.listSchedules)

	// Payment Scheme lifecycle (Increment 3)
	r.Get("/sale-contracts/{contractID}/schedule-plan", h.getSchedulePlan)
	r.With(auth.RequireWrite()).Post("/sale-contracts/{contractID}/scheme-events", h.applySchemeEvent)
	r.With(auth.RequireWrite()).Post("/sale-contracts/{contractID}/scheme-convert", h.convertScheme)
	r.With(auth.RequireSalesWrite()).Post("/sale-contracts/{contractID}/schedule-regenerate", h.regenerateSchedule)
	r.Get("/sale-contracts/{contractID}/payment-events", h.listPaymentEvents)
	r.Get("/sale-contracts/{contractID}/balance", h.buyerBalance)
	r.Get("/sale-contracts/{contractID}/financial-summary", h.contractFinancialSummary)
	r.Get("/sale-contracts/{contractID}/statement", h.customerStatement)
	// FE-2 · P4 — baca sub-ledger alokasi (read-only).
	r.Get("/sale-contracts/{contractID}/allocations", h.contractAllocations)
	r.Get("/termins/{terminID}/allocations", h.terminAllocations)
	// FE-3 — Buyer Credit Lifecycle.
	r.Get("/sale-contracts/{contractID}/buyer-credit", h.buyerCredit)
	r.With(auth.RequireWrite()).Post("/sale-contracts/{contractID}/apply-credit", h.applyCredit)
	r.Get("/schedules/due", h.listDue)
	r.With(auth.RequireWrite()).Post("/schedules/{scheduleID}/received", h.recordInstallmentPaid)
	r.With(auth.RequireWrite()).Post("/schedules/mark-overdue", h.markOverdue)

	// W-8 — tie-out piutang harga rumah terhadap buku besar (INV-AR-4).
	// Read-only, dan sengaja berada di paket yang MEMILIKI sub-ledger-nya:
	// pemilik angkalah yang harus membuktikan angkanya cocok, sama seperti
	// rekonsiliasi piutang lama tinggal di paket legacyar.
	r.Get("/receivables/house/reconciliation", h.houseARReconciliation)

	// W-8/P6 — Jadwal Penagihan: kewajiban terjadwal yang BELUM menjadi piutang
	// (pra-BAST). Komplemen house AR, dihitung di paket yang sama supaya kedua
	// himpunan tidak bisa berbeda definisi — dan supaya BD-1 tidak menghapus
	// daftar kerja tim penagihan tanpa menggantinya.
	r.Get("/receivables/house/billing-plan", h.houseBillingPlan)

	// Collection Transaction Flow — catat pembayaran dari halaman collection.
	r.Get("/collections/payment/preview", h.previewCollectionPayment)
	r.With(auth.RequireWrite()).Post("/collections/payment", h.recordCollectionPayment)
}

// ── GET /collections/payment/preview ──────────────────────────────────────────

func (h *Handler) previewCollectionPayment(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := strconv.ParseUint(r.URL.Query().Get("contract_id"), 10, 64)
	if err != nil || contractID == 0 {
		writeSaleError(w, http.StatusBadRequest, "contract_id tidak valid")
		return
	}
	amount, err := domain.NewMoney(r.URL.Query().Get("amount"))
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "amount tidak valid")
		return
	}
	bankCode := r.URL.Query().Get("bank_account_code")
	if bankCode == "" {
		bankCode = "1-1300"
	}
	bankFee := domain.Zero
	if raw := r.URL.Query().Get("bank_fee"); raw != "" {
		bankFee, err = domain.NewMoney(raw)
		if err != nil {
			writeSaleError(w, http.StatusBadRequest, "bank_fee tidak valid: "+err.Error())
			return
		}
	}
	var scheduleID uint64
	if raw := r.URL.Query().Get("schedule_id"); raw != "" {
		scheduleID, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || scheduleID == 0 {
			writeSaleError(w, http.StatusBadRequest, "schedule_id tidak valid")
			return
		}
	}
	// source: sejajar dengan recordCollectionPayment — kehadiran
	// financing_source_id menandakan pencairan KPR (7C), supaya pratinjau
	// memakai resolusi akun kredit yang SAMA dengan yang akan diposting.
	source := PaymentSourceCollection
	if raw := r.URL.Query().Get("financing_source_id"); raw != "" {
		if _, ferr := strconv.ParseUint(raw, 10, 64); ferr == nil {
			source = PaymentSourceKPRDisbursement
		}
	}

	preview, err := h.svc.PreviewCollectionPayment(r.Context(), tenantID, contractID, amount, bankCode, bankFee, scheduleID, source)
	if err != nil {
		switch {
		case errors.Is(err, ErrContractNotFound):
			writeSaleError(w, http.StatusNotFound, "kontrak tidak ditemukan")
		case errors.Is(err, ErrScheduleNotFound):
			writeSaleError(w, http.StatusNotFound, "cicilan tidak ditemukan")
		case errors.Is(err, ErrScheduleContractMismatch):
			writeSaleError(w, http.StatusBadRequest, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeSaleJSON(w, http.StatusOK, preview)
}

// ── GET /receivables/house/reconciliation ─────────────────────────────────────

// houseARReconciliation menyajikan tie-out GL ↔ sub-ledger piutang harga rumah.
//
// `as_of` opsional (YYYY-MM-DD). Tanpa `as_of` yang dibandingkan adalah posisi
// BERJALAN — seluruh jurnal posted. Dengan `as_of`, buku besar dipotong per
// tanggal itu sementara sub-ledger tetap posisi berjalan; karena itu parameter
// ini hanya diterima apa adanya dan hasilnya diberi catatan, bukan diam-diam
// dianggap perbandingan historis yang sah.
func (h *Handler) houseARReconciliation(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var asOf *time.Time
	if raw := r.URL.Query().Get("as_of"); raw != "" {
		t, perr := time.Parse("2006-01-02", raw)
		if perr != nil {
			writeSaleError(w, http.StatusBadRequest, "as_of tidak valid (format YYYY-MM-DD)")
			return
		}
		end := t.Add(24*time.Hour - time.Nanosecond)
		asOf = &end
	}

	rec, err := h.svc.ReconcileHouseAR(r.Context(), tenantID, asOf)
	if err != nil {
		if errors.Is(err, ErrHouseARNotConfigured) || errors.Is(err, ErrHouseARLedgerNotConfigured) {
			writeSaleError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if asOf != nil {
		rec.Note = "sisi sub-ledger memakai posisi berjalan; batasi hanya sisi buku besar bila membandingkan tanggal historis"
	}
	writeSaleJSON(w, http.StatusOK, rec)
}

// ── GET /receivables/house/billing-plan ───────────────────────────────────────

// houseBillingPlan menyajikan kewajiban terjadwal pra-BAST — RENCANA PENAGIHAN,
// bukan piutang (BD-1). `as_of` opsional (YYYY-MM-DD, default hari ini) hanya
// menentukan titik penilaian jatuh tempo; tidak ada saldo buku besar yang dibaca
// di sini karena secara akuntansi belum ada apa-apa untuk dibaca.
func (h *Handler) houseBillingPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}

	asOf := time.Now()
	if raw := r.URL.Query().Get("as_of"); raw != "" {
		t, perr := time.Parse("2006-01-02", raw)
		if perr != nil {
			writeSaleError(w, http.StatusBadRequest, "as_of tidak valid (format YYYY-MM-DD)")
			return
		}
		asOf = t
	}

	plan, err := h.svc.BillingPlanFor(r.Context(), tenantID, asOf)
	if err != nil {
		if errors.Is(err, ErrBillingPlanNotConfigured) {
			writeSaleError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, plan)
}

// ── POST /collections/payment ─────────────────────────────────────────────────

type collectionPaymentBody struct {
	ContractID uint64 `json:"contract_id"`
	// ScheduleID (opsional): target SATU cicilan spesifik (mis. baris Kelebihan
	// Tanah) alih-alih waterfall seluruh kontrak — lihat ReceivePaymentRequest.
	// ScheduleID. Reuse penuh: endpoint & engine yang sama dipakai unit/produk
	// tambahan mana pun, tidak ada payment engine kedua.
	ScheduleID      *uint64 `json:"schedule_id,omitempty"`
	Amount          string  `json:"amount"` // string — hindari float
	Date            string  `json:"date"`   // RFC3339
	BankAccountCode string  `json:"bank_account_code"`
	Reference       string  `json:"reference"`
	Notes           string  `json:"notes"`
	IdempotencyKey  string  `json:"idempotency_key"`
	// FinancingSourceID (Increment 3): isi bila penerimaan adalah PENCAIRAN BANK
	// KPR — payment_source otomatis menjadi kpr_disbursement.
	FinancingSourceID *uint64 `json:"financing_source_id,omitempty"`
	// Kind/InstallmentNo (W-13): label "Jenis Penerimaan" dari +Catat Penerimaan.
	// Diabaikan (diturunkan otomatis) bila FinancingSourceID diisi.
	Kind          string `json:"kind,omitempty"`
	InstallmentNo *int   `json:"installment_no,omitempty"`
	// BankFee (UAT 2026-09-03, Rule #5): provisi/administrasi bank yang
	// dipotong dari nominal pencairan (mis. KPR) — ditanggung developer, bukan
	// titipan customer. Kosong/absent = "0" (tanpa perubahan perilaku).
	BankFee string `json:"bank_fee,omitempty"`
}

func (h *Handler) recordCollectionPayment(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	userID, _ := auth.UserIDFrom(r.Context())

	var body collectionPaymentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	amount, err := domain.NewMoney(body.Amount)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "amount tidak valid: "+err.Error())
		return
	}
	date, err := time.Parse(time.RFC3339, body.Date)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "date format tidak valid (gunakan RFC3339)")
		return
	}
	bankFee := domain.Zero
	if body.BankFee != "" {
		bankFee, err = domain.NewMoney(body.BankFee)
		if err != nil {
			writeSaleError(w, http.StatusBadRequest, "bank_fee tidak valid: "+err.Error())
			return
		}
	}

	var createdBy *uint64
	if userID != 0 {
		createdBy = &userID
	}

	// Single payment entry: kwitansi + sinkron invoice ditangani di dalam ReceivePayment.
	contractID := body.ContractID
	source := PaymentSourceCollection
	if body.FinancingSourceID != nil {
		source = PaymentSourceKPRDisbursement // pencairan bank (Increment 3)
	}
	result, err := h.svc.ReceivePayment(r.Context(), tenantID, ReceivePaymentRequest{
		Source:            source,
		ContractID:        &contractID,
		ScheduleID:        body.ScheduleID,
		Amount:            amount,
		Date:              date,
		BankAccountCode:   body.BankAccountCode,
		Reference:         body.Reference,
		Notes:             body.Notes,
		CreatedBy:         createdBy,
		IdempotencyKey:    body.IdempotencyKey,
		FinancingSourceID: body.FinancingSourceID,
		Kind:              TerminKind(body.Kind),
		InstallmentNo:     body.InstallmentNo,
		BankFee:           bankFee,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrContractNotFound):
			writeSaleError(w, http.StatusNotFound, "kontrak tidak ditemukan")
		case errors.Is(err, ErrCollectionAmountInvalid), errors.Is(err, ErrTerminAmountFractional),
			errors.Is(err, ErrTerminAmountZeroOrNeg), errors.Is(err, ErrInvalidBankAccount),
			errors.Is(err, ErrPaymentAccountNotFound), errors.Is(err, ErrPaymentAccountInactive),
			errors.Is(err, ErrBankFeeInvalid):
			writeSaleError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrPaymentExceedsOutstanding), errors.Is(err, ErrPaymentExceedsReceivable),
			errors.Is(err, ErrDisbursementRequiresAkad), errors.Is(err, ErrDisbursementNeedsContract):
			writeSaleError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	status := http.StatusCreated
	if result.AlreadyExisted {
		status = http.StatusOK
	}
	writeSaleJSON(w, status, result)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func saleTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeSaleJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSaleError(w http.ResponseWriter, status int, msg string) {
	writeSaleJSON(w, status, map[string]string{"error": msg})
}

func parseSaleUintParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

// ── GET /units/{unitID}/termins ────────────────────────────────────────────────

func (h *Handler) listTermins(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseSaleUintParam(r, "unitID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}
	termins, err := h.svc.ListTerminsByUnit(r.Context(), tenantID, unitID)
	if err != nil {
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, map[string]any{"tenant_id": tenantID, "unit_id": unitID, "data": termins})
}

// ── POST /units/{unitID}/termins ──────────────────────────────────────────────

type recordTerminRequest struct {
	BankAccountCode string `json:"bank_account_code"`
	Amount          string `json:"amount"` // string untuk menghindari float
	Date            string `json:"date"`   // RFC3339
	Description     string `json:"description"`
	IdempotencyKey  string `json:"idempotency_key"` // proteksi double-submit (opsional)
	// Kind/InstallmentNo (W-13): label "Jenis Penerimaan" — opsional, dipakai
	// jalur fallback +Catat Penerimaan tanpa kontrak (unit belum ber-PPJB).
	Kind          string `json:"kind,omitempty"`
	InstallmentNo *int   `json:"installment_no,omitempty"`
}

func (h *Handler) recordTermin(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseSaleUintParam(r, "unitID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}

	var body recordTerminRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}

	amount, err := domain.NewMoney(body.Amount)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "amount tidak valid: "+err.Error())
		return
	}
	date, err := time.Parse(time.RFC3339, body.Date)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "date format tidak valid (gunakan RFC3339)")
		return
	}

	// ADAPTER KOMPATIBILITAS: endpoint lama tidak memposting jurnal sendiri —
	// delegate ke ReceivePayment (engine penerimaan tunggal). source=unit_termin.
	userID, _ := auth.UserIDFrom(r.Context())
	var createdBy *uint64
	if userID != 0 {
		createdBy = &userID
	}
	uid := unitID
	res, err := h.svc.ReceivePayment(r.Context(), tenantID, ReceivePaymentRequest{
		Source:          PaymentSourceUnitTermin,
		UnitID:          &uid,
		Amount:          amount,
		Date:            date,
		BankAccountCode: body.BankAccountCode,
		Notes:           body.Description,
		CreatedBy:       createdBy,
		IdempotencyKey:  body.IdempotencyKey,
		Kind:            TerminKind(body.Kind),
		InstallmentNo:   body.InstallmentNo,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrTerminAmountFractional),
			errors.Is(err, ErrTerminAmountZeroOrNeg),
			errors.Is(err, ErrCollectionAmountInvalid),
			errors.Is(err, ErrInvalidBankAccount),
			errors.Is(err, ErrPaymentAccountNotFound),
			errors.Is(err, ErrPaymentAccountInactive),
			errors.Is(err, ErrUnitRequired):
			writeSaleError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrUnitAlreadySold):
			writeSaleError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrUnitNotFound):
			writeSaleError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrPaymentExceedsOutstanding):
			writeSaleError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	status := http.StatusCreated
	if res.AlreadyExisted {
		status = http.StatusOK
	}
	writeSaleJSON(w, status, res)
}

// ── POST /units/{unitID}/akad ─────────────────────────────────────────────────

type recordAkadRequest struct {
	SalePrice       string `json:"sale_price"` // DPP, string untuk menghindari float
	IsVAT           bool   `json:"is_vat"`
	VATRate         string `json:"vat_rate,omitempty"` // e.g. "0.11"
	BuyerRef        string `json:"buyer_ref"`
	RecognitionDate string `json:"recognition_date"` // RFC3339 — tanggal Akad
	// BankApprovedAmount (Item 7A, UAT 2026-09-07): Nilai Persetujuan KPR
	// Bank — WAJIB untuk kontrak KPR-financed, diisi SAAT AKAD (bukan saat
	// pembuatan kontrak). Dasar Dana Jaminan Bank. Kosong/nil untuk kontrak
	// non-KPR.
	BankApprovedAmount string `json:"bank_approved_amount,omitempty"`
}

func (h *Handler) recordAkad(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseSaleUintParam(r, "unitID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}

	var body recordAkadRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}

	salePrice, err := domain.NewMoney(body.SalePrice)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "sale_price tidak valid: "+err.Error())
		return
	}
	recognitionDate, err := time.Parse(time.RFC3339, body.RecognitionDate)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "recognition_date format tidak valid (gunakan RFC3339)")
		return
	}

	var vatRate decimal.Decimal
	if body.IsVAT && body.VATRate != "" {
		vatRate, err = decimal.NewFromString(body.VATRate)
		if err != nil {
			writeSaleError(w, http.StatusBadRequest, "vat_rate tidak valid")
			return
		}
	}

	var bankApproved *domain.Money
	if body.BankApprovedAmount != "" {
		m, berr := domain.NewMoney(body.BankApprovedAmount)
		if berr != nil {
			writeSaleError(w, http.StatusBadRequest, "bank_approved_amount tidak valid: "+berr.Error())
			return
		}
		bankApproved = &m
	}

	userID, _ := auth.UserIDFrom(r.Context())
	var akadActor *uint64
	if userID != 0 {
		akadActor = &userID
	}
	req := RecordBASTRequest{
		UnitID:             unitID,
		CreatedBy:          akadActor,
		SalePrice:          salePrice,
		IsVAT:              body.IsVAT,
		VATRate:            vatRate,
		BuyerRef:           body.BuyerRef,
		BASTDate:           recognitionDate,
		BankApprovedAmount: bankApproved,
	}
	record, err := h.svc.RecordAkad(r.Context(), tenantID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrSalePriceFractional),
			errors.Is(err, ErrSalePriceZeroOrNeg),
			errors.Is(err, ErrVATRateRequired),
			errors.Is(err, ErrBASTDateRequired),
			errors.Is(err, ErrUnitRequired),
			errors.Is(err, ErrBankApprovedAmountFractional),
			errors.Is(err, ErrBankApprovedAmountZeroOrNeg):
			writeSaleError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrUnitAlreadySold), errors.Is(err, project.ErrUnitTransitionConflict):
			writeSaleError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrUnitNotFound):
			writeSaleError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrAdvanceExceedsSalePrice), errors.Is(err, ErrUnitNotBASTReady),
			errors.Is(err, ErrAllocationBasisMissing), errors.Is(err, ErrTrueupNotPostedForBAST),
			errors.Is(err, ErrBankApprovedAmountRequired),
			errors.Is(err, scheme.ErrBASTGateNotMet):
			// Gate kebijakan Akad yang tersisa (harga rumah / payment scheme)
			// adalah penolakan bisnis — bukan error server. Gate biaya realisasi
			// sudah dicabut di W-5 (D-3).
			writeSaleError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeSaleJSON(w, http.StatusCreated, record)
}

// ── POST /units/{unitID}/physical-handover ────────────────────────────────────

type recordPhysicalHandoverRequest struct {
	HandoverDate string `json:"handover_date"` // RFC3339
}

func (h *Handler) recordPhysicalHandover(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseSaleUintParam(r, "unitID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}

	var body recordPhysicalHandoverRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	handoverDate, err := time.Parse(time.RFC3339, body.HandoverDate)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "handover_date format tidak valid (gunakan RFC3339)")
		return
	}

	userID, _ := auth.UserIDFrom(r.Context())
	var actor *uint64
	if userID != 0 {
		actor = &userID
	}
	record, err := h.svc.RecordPhysicalHandover(r.Context(), tenantID, RecordPhysicalHandoverRequest{
		UnitID:       unitID,
		CreatedBy:    actor,
		HandoverDate: handoverDate,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrUnitRequired), errors.Is(err, ErrBASTDateRequired):
			writeSaleError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, project.ErrUnitTransitionConflict):
			writeSaleError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrUnitNotFound), errors.Is(err, ErrSaleRecordNotFound):
			writeSaleError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrUnitNotHandoverReady):
			writeSaleError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeSaleJSON(w, http.StatusCreated, record)
}

// ── GET /units/{unitID}/sale-record ──────────────────────────────────────────

func (h *Handler) getSaleRecord(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseSaleUintParam(r, "unitID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}

	var record SaleRecord
	err = h.svc.store.(*GORMRepository).db.
		Where("tenant_id = ? AND unit_id = ?", tenantID, unitID).
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeSaleError(w, http.StatusNotFound, ErrSaleRecordNotFound.Error())
			return
		}
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, record)
}

// ── POST /sale-contracts ──────────────────────────────────────────────────────

type createContractBody struct {
	UnitID       uint64  `json:"unit_id"`
	BuyerName    string  `json:"buyer_name"`
	BuyerID      string  `json:"buyer_id"`
	PaymentType  string  `json:"payment_type"`
	BankKPR      *string `json:"bank_kpr,omitempty"`
	LoanAmount   *string `json:"loan_amount,omitempty"` // string untuk menghindari float
	ContractDate string  `json:"contract_date"`         // RFC3339
	TotalPrice   string  `json:"total_price"`           // string untuk menghindari float
	// Increment 3 — WAJIB untuk kontrak baru (scheme flow aktif di produksi):
	PaymentSchemeID   *uint64 `json:"payment_scheme_id,omitempty"`
	FinancingSourceID *uint64 `json:"financing_source_id,omitempty"`
	CustomerID        *uint64 `json:"customer_id,omitempty"`
	SalesPersonID     *uint64 `json:"sales_person_id,omitempty"`
	// AdminMarketingPersonID (P1 — Sales ≠ Admin Marketing): opsional, penanggung
	// jawab administrasi kontrak/dokumen/KPR/follow-up. Independen dari komisi.
	AdminMarketingPersonID *uint64 `json:"admin_marketing_person_id,omitempty"`
	// Increment 7: konversi booking → kontrak (atomik).
	BookingID *uint64 `json:"booking_id,omitempty"`

	// LandQuantityM2 (kelebihan-tanah-konversi-kontrak-2026-08): komponen
	// opsional Produk Tambahan Kelebihan Tanah — berlaku baik untuk kontrak
	// yang dibuat LANGSUNG maupun konversi booking (BookingID diisi). String
	// kosong/absent = tanpa komponen tanah.
	LandQuantityM2 string `json:"land_quantity_m2,omitempty"`
}

func (h *Handler) createContract(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var body createContractBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}

	totalPrice, err := domain.NewMoney(body.TotalPrice)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "total_price tidak valid: "+err.Error())
		return
	}
	contractDate, err := time.Parse(time.RFC3339, body.ContractDate)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contract_date format tidak valid (gunakan RFC3339)")
		return
	}

	userID, _ := auth.UserIDFrom(r.Context())
	var createdBy *uint64
	if userID != 0 {
		createdBy = &userID
	}
	req := CreateContractRequest{
		UnitID:                 body.UnitID,
		BuyerName:              body.BuyerName,
		BuyerID:                body.BuyerID,
		PaymentType:            PaymentType(body.PaymentType),
		BankKPR:                body.BankKPR,
		ContractDate:           contractDate,
		TotalPrice:             totalPrice,
		PaymentSchemeID:        body.PaymentSchemeID,
		FinancingSourceID:      body.FinancingSourceID,
		CustomerID:             body.CustomerID,
		SalesPersonID:          body.SalesPersonID,
		AdminMarketingPersonID: body.AdminMarketingPersonID,
		CreatedBy:              createdBy,
		BookingID:              body.BookingID,
	}
	if body.LoanAmount != nil {
		m, err := domain.NewMoney(*body.LoanAmount)
		if err != nil {
			writeSaleError(w, http.StatusBadRequest, "loan_amount tidak valid: "+err.Error())
			return
		}
		req.LoanAmount = &m
	}
	if body.LandQuantityM2 != "" {
		qty, qerr := decimal.NewFromString(body.LandQuantityM2)
		if qerr != nil {
			writeSaleError(w, http.StatusBadRequest, "land_quantity_m2 tidak valid: "+qerr.Error())
			return
		}
		req.LandQuantityM2 = &qty
	}

	contract, err := h.svc.CreateContract(r.Context(), tenantID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnitRequired),
			errors.Is(err, ErrContractTotalPriceInvalid),
			errors.Is(err, ErrInvalidPaymentType),
			errors.Is(err, ErrPaymentSchemeRequired),
			errors.Is(err, ErrCustomerRequiredForContract),
			errors.Is(err, ErrSalesPersonRequiredForContract),
			errors.Is(err, scheme.ErrInvalidParams),
			errors.Is(err, scheme.ErrSchemeInactive),
			errors.Is(err, scheme.ErrFinancingSourceRequired),
			errors.Is(err, scheme.ErrFinancingSourceNotAllowed),
			errors.Is(err, ErrLandQuantityInvalid):
			writeSaleError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, scheme.ErrSchemeNotFound),
			errors.Is(err, scheme.ErrFinSourceNotFound),
			errors.Is(err, customer.ErrCustomerNotFound),
			errors.Is(err, salesorg.ErrPersonNotFound),
			errors.Is(err, ErrBookingNotFound),
			errors.Is(err, land.ErrLandStockNotFound):
			writeSaleError(w, http.StatusNotFound, err.Error())
		// Increment 7 — konversi booking:
		case errors.Is(err, ErrBookingNotActive):
			writeSaleError(w, http.StatusConflict, err.Error())
		// kelebihan-tanah-booking-integration-2026-08: reservasi tanah gagal
		// karena race dengan reservasi/penjualan lain (row lock land.ReserveTx) —
		// kegagalan permintaan, bukan bug server.
		case errors.Is(err, land.ErrCapacityExceeded):
			writeSaleError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrBookingUnitMismatch),
			errors.Is(err, ErrBookingCustomerMismatch),
			errors.Is(err, ErrBookingUnitStateInvalid),
			errors.Is(err, ErrTitipanAccountMissing):
			writeSaleError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeSaleJSON(w, http.StatusCreated, contract)
}

// ── PUT /sale-contracts/{contractID}/admin-marketing ─────────────────────────

type setAdminMarketingBody struct {
	// AdminMarketingPersonID: null/absent = hapus penugasan.
	AdminMarketingPersonID *uint64 `json:"admin_marketing_person_id"`
}

func (h *Handler) setAdminMarketing(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	var body setAdminMarketingBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}

	contract, err := h.svc.SetAdminMarketing(r.Context(), tenantID, contractID, body.AdminMarketingPersonID)
	if err != nil {
		switch {
		case errors.Is(err, ErrContractNotFound):
			writeSaleError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, salesorg.ErrPersonNotFound):
			writeSaleError(w, http.StatusNotFound, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeSaleJSON(w, http.StatusOK, contract)
}

// ── POST /sale-contracts/{contractID}/schedule ────────────────────────────────

type scheduleItemBody struct {
	InstallmentNumber int    `json:"installment_number"`
	DueDate           string `json:"due_date"` // RFC3339
	Amount            string `json:"amount"`   // string untuk menghindari float
	Type              string `json:"type"`     // dp|installment|final
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}

	var body []scheduleItemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid (array of schedule items)")
		return
	}

	items := make([]ScheduleItem, 0, len(body))
	for i, b := range body {
		amount, err := domain.NewMoney(b.Amount)
		if err != nil {
			writeSaleError(w, http.StatusBadRequest, fmt.Sprintf("item[%d].amount tidak valid: %v", i, err))
			return
		}
		dueDate, err := time.Parse(time.RFC3339, b.DueDate)
		if err != nil {
			writeSaleError(w, http.StatusBadRequest, fmt.Sprintf("item[%d].due_date tidak valid: %v", i, err))
			return
		}
		items = append(items, ScheduleItem{
			InstallmentNumber: b.InstallmentNumber,
			DueDate:           dueDate,
			Amount:            amount,
			Type:              ScheduleType(b.Type),
		})
	}

	schedules, err := h.svc.CreatePaymentSchedule(r.Context(), tenantID, contractID, items)
	if err != nil {
		if errors.Is(err, ErrContractNotFound) {
			writeSaleError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusCreated, schedules)
}

// ── GET /sale-contracts/{contractID}/schedule ─────────────────────────────────

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}

	items, err := h.svc.ListSchedulesByContract(r.Context(), tenantID, contractID)
	if err != nil {
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, items)
}

// ── GET /sale-contracts/{contractID}/balance ──────────────────────────────────

func (h *Handler) buyerBalance(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}

	remaining, err := h.svc.BuyerRemainingBalance(r.Context(), tenantID, contractID)
	if err != nil {
		if errors.Is(err, ErrContractNotFound) {
			writeSaleError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, map[string]string{"remaining_balance": remaining.String()})
}

// ── GET /sale-contracts/{contractID}/financial-summary (R1) ──────────────────
// Ringkasan finansial kontrak — SATU rumus (H-2). Dipakai preview pencairan,
// statement, dan konsumen lain. Read-only.
func (h *Handler) contractFinancialSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	sum, err := h.svc.ContractFinancialSummaryByID(r.Context(), tenantID, contractID)
	if err != nil {
		if errors.Is(err, ErrContractNotFound) {
			writeSaleError(w, http.StatusNotFound, "kontrak tidak ditemukan")
			return
		}
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, sum)
}

// ── GET /sale-contracts/{contractID}/allocations (FE-2 · P4) ─────────────────

func (h *Handler) contractAllocations(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	items, err := h.svc.ListContractAllocations(r.Context(), tenantID, contractID)
	if err != nil {
		if errors.Is(err, ErrContractNotFound) {
			writeSaleError(w, http.StatusNotFound, "kontrak tidak ditemukan")
			return
		}
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, items)
}

// ── GET /termins/{terminID}/allocations (FE-2 · P4) ──────────────────────────

func (h *Handler) terminAllocations(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	terminID, err := parseSaleUintParam(r, "terminID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "terminID tidak valid")
		return
	}
	items, err := h.svc.ListTerminAllocations(r.Context(), tenantID, terminID)
	if err != nil {
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, items)
}

// ── GET /sale-contracts/{contractID}/buyer-credit (FE-3) ─────────────────────

func (h *Handler) buyerCredit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	view, err := h.svc.GetBuyerCredit(r.Context(), tenantID, contractID)
	if err != nil {
		if errors.Is(err, ErrContractNotFound) {
			writeSaleError(w, http.StatusNotFound, "kontrak tidak ditemukan")
			return
		}
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, view)
}

// ── POST /sale-contracts/{contractID}/apply-credit (FE-3) ────────────────────

type applyCreditBody struct {
	ScheduleID     uint64 `json:"schedule_id"`
	Amount         string `json:"amount"` // opsional; kosong → min(saldo, sisa cicilan)
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h *Handler) applyCredit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	userID, _ := auth.UserIDFrom(r.Context())

	var body applyCreditBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	req := ApplyCreditRequest{
		ScheduleID:     body.ScheduleID,
		Reason:         body.Reason,
		IdempotencyKey: body.IdempotencyKey,
	}
	if userID != 0 {
		req.AppliedBy = &userID
	}
	if body.Amount != "" {
		amt, aerr := domain.NewMoney(body.Amount)
		if aerr != nil {
			writeSaleError(w, http.StatusBadRequest, "amount tidak valid: "+aerr.Error())
			return
		}
		req.Amount = &amt
	}

	result, err := h.svc.ApplyCredit(r.Context(), tenantID, contractID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrContractNotFound), errors.Is(err, ErrScheduleNotFound):
			writeSaleError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrNoCreditAvailable), errors.Is(err, ErrCreditExceedsAvailable),
			errors.Is(err, ErrScheduleOverpaid), errors.Is(err, ErrScheduleAlreadyPaid),
			errors.Is(err, ErrCreditAmountFractional), errors.Is(err, ErrCreditAmountZeroOrNeg):
			writeSaleError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeSaleJSON(w, http.StatusOK, result)
}

// ── GET /sale-contracts/{contractID}/statement ────────────────────────────────

func (h *Handler) customerStatement(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}

	asOf := time.Now()
	if s := r.URL.Query().Get("as_of"); s != "" {
		if t, perr := time.Parse("2006-01-02", s); perr == nil {
			asOf = t
		}
	}

	stmt, err := h.svc.GetCustomerStatement(r.Context(), tenantID, contractID, asOf)
	if err != nil {
		switch {
		case errors.Is(err, ErrContractNotFound), errors.Is(err, ErrContractStoreNotConfigured):
			writeSaleError(w, http.StatusNotFound, "kontrak tidak ditemukan")
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeSaleJSON(w, http.StatusOK, stmt)
}

// ── GET /schedules/due ────────────────────────────────────────────────────────

func (h *Handler) listDue(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "parameter 'from' tidak valid (gunakan RFC3339)")
		return
	}
	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "parameter 'to' tidak valid (gunakan RFC3339)")
		return
	}

	items, err := h.svc.ListDueInPeriod(r.Context(), tenantID, from, to)
	if err != nil {
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, items)
}

// ── POST /schedules/{scheduleID}/received ────────────────────────────────────

type recordInstallmentBody struct {
	BankAccountCode string `json:"bank_account_code"`
	ReceivedAt      string `json:"received_at"` // RFC3339
	Description     string `json:"description"`
}

func (h *Handler) recordInstallmentPaid(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	scheduleID, err := parseSaleUintParam(r, "scheduleID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "scheduleID tidak valid")
		return
	}

	var body recordInstallmentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	receivedAt, err := time.Parse(time.RFC3339, body.ReceivedAt)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "received_at format tidak valid (gunakan RFC3339)")
		return
	}

	updated, err := h.svc.RecordInstallmentPaid(r.Context(), tenantID, RecordInstallmentPaidRequest{
		ScheduleID:      scheduleID,
		BankAccountCode: body.BankAccountCode,
		ReceivedAt:      receivedAt,
		Description:     body.Description,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrScheduleNotFound):
			writeSaleError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrInstallmentAlreadyReceived):
			writeSaleError(w, http.StatusConflict, err.Error())
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	// Sinkron invoice sudah ditangani di dalam ReceivePayment (lewat adapter).
	writeSaleJSON(w, http.StatusOK, updated)
}

// ── POST /schedules/mark-overdue ─────────────────────────────────────────────

type markOverdueBody struct {
	AsOf string `json:"as_of"` // RFC3339 — deterministik, TIDAK time.Now()
}

func (h *Handler) markOverdue(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var body markOverdueBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	asOf, err := time.Parse(time.RFC3339, body.AsOf)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "as_of format tidak valid (gunakan RFC3339)")
		return
	}

	count, err := h.svc.MarkOverdue(r.Context(), tenantID, asOf)
	if err != nil {
		writeSaleError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSaleJSON(w, http.StatusOK, map[string]int{"marked_overdue": count})
}

// ── GET /units/{unitID}/contract ─────────────────────────────────────────────

// getUnitContract mengembalikan kontrak jual beli yang terkait dengan sebuah unit.
// Digunakan oleh frontend billing untuk mendapat contractID tanpa state di URL.
func (h *Handler) getUnitContract(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseSaleUintParam(r, "unitID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}

	contract, err := h.svc.GetContractByUnitID(r.Context(), tenantID, unitID)
	if err != nil {
		switch {
		case errors.Is(err, ErrContractNotFound), errors.Is(err, ErrContractStoreNotConfigured):
			writeSaleError(w, http.StatusNotFound, "kontrak tidak ditemukan untuk unit ini")
		default:
			writeSaleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeSaleJSON(w, http.StatusOK, contract)
}
