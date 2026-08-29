package tax

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── Interfaces ────────────────────────────────────────────────────────────────

// TaxRateProvider menyediakan tarif pajak bertanggal dari database.
// TIDAK ADA konstanta tarif di kode posting — semua tarif diambil dari sini.
type TaxRateProvider interface {
	GetCurrentRate(ctx context.Context, tenantID uint64, rateCode string, referenceDate time.Time) (*TaxRate, error)
	SaveRate(ctx context.Context, r *TaxRate) error
}

// TaxRuleResolver adalah SEAM resolusi Tax Rule (Increment 4, blueprint §7):
// memilih rule berdasarkan (rate_code, kategori proyek, tanggal event) dengan
// specificity kategori > 'all'. Kode posting tidak tahu aturan pemilihan —
// hanya memakai hasil resolve (tarif + akun).
type TaxRuleResolver interface {
	ResolveRule(ctx context.Context, tenantID uint64, rateCode string, category domain.TaxCategory, referenceDate time.Time) (*TaxRate, error)
}

// ProjectTaxCategoryReader membaca penentu tarif dari proyek (projects.tax_category).
type ProjectTaxCategoryReader interface {
	GetProjectTaxCategory(ctx context.Context, tenantID, projectID uint64) (domain.TaxCategory, error)
}

// AccountFinder melihat ID akun berdasarkan kode COA (tenant-scoped).
type AccountFinder interface {
	FindAccountIDByCode(ctx context.Context, tenantID uint64, code string) (uint64, error)
	// ResolvePaymentAccountID mengembalikan ID akun kas/bank untuk `code`,
	// tervalidasi COA-driven (ledger.ValidatePaymentAccount) — bukan daftar kode
	// di dalam kode program (W-3.0 B-1). Error: ErrInvalidBankAccount.
	ResolvePaymentAccountID(ctx context.Context, tenantID uint64, code string) (uint64, error)
}

// TxRunner menjalankan fn dalam SATU transaksi database dengan kolaborator yang
// terikat transaksi tersebut (W-3.0 B-2).
//
// PayTax menulis empat kali — buat jurnal, posting, ubah status obligation,
// simpan payment. Tanpa transaksi, gagal di tengah bisa memposting jurnal kas
// yang mengkredit bank sementara kewajiban tetap outstanding dan tidak ada baris
// pembayaran: kas bergerak, dokumen tidak pernah lahir.
//
// nil = jalur lama tanpa transaksi (unit test dengan mock) — perilaku dipertahankan.
type TxRunner interface {
	InTx(ctx context.Context, fn func(journals JournalWriter, store TaxStore) error) error
}

// JournalWriter membuat dan memposting satu jurnal.
type JournalWriter interface {
	CreateJournal(ctx context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (uint64, error)
	// PostJournal memposting jurnal; cashOut menandai posting yang menggerakkan
	// kas keluar sehingga wajib berdokumen BKK (INV-DOC-1). Boolean, bukan kode
	// jenis dokumen: setoran pajak selalu kas perusahaan sendiri, dan menyalin
	// literal kode ke package ini berarti dua daftar yang bisa berselisih.
	PostJournal(ctx context.Context, tenantID, journalID uint64, cashOut bool) error
}

// TaxStore mengelola TaxObligation dan TaxPayment.
type TaxStore interface {
	SaveObligation(ctx context.Context, o *TaxObligation) error
	FindObligationByID(ctx context.Context, tenantID, id uint64) (*TaxObligation, error)
	UpdateObligationStatus(ctx context.Context, tenantID, id uint64, status TaxObligationStatus) error
	SavePayment(ctx context.Context, p *TaxPayment) error
	ListObligationsByPeriod(ctx context.Context, tenantID uint64, from, to time.Time) ([]*TaxObligation, error)
	// ListObligationsForUnits digunakan oleh penjaga: cari unit yang sudah akrual PPh Final.
	ListObligationsForUnits(ctx context.Context, tenantID uint64, rateCode string, unitIDs []uint64) ([]*TaxObligation, error)
}

// BASTReader membaca unit yang sudah BAST dalam satu periode (dari sale_records).
type BASTReader interface {
	ListBASTUnitIDsInPeriod(ctx context.Context, tenantID uint64, from, to time.Time) ([]uint64, error)
}

// LedgerAccountReader membaca saldo sisi debit/kredit dari jurnal (rekonsiliasi ke ledger).
type LedgerAccountReader interface {
	SumCreditByAccountCodeAndPeriod(ctx context.Context, tenantID uint64, accountCode string, from, to time.Time) (domain.Money, error)
	SumDebitByAccountCodeAndPeriod(ctx context.Context, tenantID uint64, accountCode string, from, to time.Time) (domain.Money, error)
}

// ── Service ───────────────────────────────────────────────────────────────────

type Service struct {
	rates        TaxRateProvider
	accounts     AccountFinder
	journals     JournalWriter
	store        TaxStore
	bastReader   BASTReader          // opsional; wajib untuk ListBASTWithoutPPhFinal
	ledgerReader LedgerAccountReader // opsional; wajib untuk GetVATReport
	// Increment 4 (opsional via WithRuleResolution): resolusi Tax Rule per
	// kategori proyek + akun konfigurable. Bila nil → jalur legacy
	// (GetCurrentRate + akun default) — unit test lama tetap valid.
	ruleResolver      TaxRuleResolver
	projectCategories ProjectTaxCategoryReader
	// formulas: seam perhitungan (hardening). NewService memasang default
	// (proportional); formula baru = Register, tanpa ubah resolver/flow.
	formulas *FormulaRegistry
	// tx (W-3.0): seam transaksi untuk PayTax. nil = tanpa transaksi.
	tx TxRunner
}

// ServiceOption adalah functional option untuk Service (Phase 8).
type ServiceOption func(*Service)

func WithBASTReader(br BASTReader) ServiceOption {
	return func(s *Service) { s.bastReader = br }
}

func WithLedgerReader(lr LedgerAccountReader) ServiceOption {
	return func(s *Service) { s.ledgerReader = lr }
}

// WithRuleResolution mengaktifkan resolusi Tax Rule konfiguratif (Increment 4):
// tarif per kategori proyek (subsidi 1% vs komersial 2,5%) + akun dari rule.
func WithRuleResolution(rr TaxRuleResolver, pc ProjectTaxCategoryReader) ServiceOption {
	return func(s *Service) {
		s.ruleResolver = rr
		s.projectCategories = pc
	}
}

// WithFormulaRegistry mengganti registry formula (default: proportional saja).
func WithFormulaRegistry(fr *FormulaRegistry) ServiceOption {
	return func(s *Service) { s.formulas = fr }
}

// WithTxRunner memasang seam transaksi (W-3.0). Wajib pada wiring produksi:
// tanpa ini PayTax bisa menghasilkan posting parsial.
func WithTxRunner(tr TxRunner) ServiceOption {
	return func(s *Service) { s.tx = tr }
}

func NewService(rates TaxRateProvider, accounts AccountFinder, journals JournalWriter, store TaxStore, opts ...ServiceOption) *Service {
	s := &Service{rates: rates, accounts: accounts, journals: journals, store: store}
	for _, opt := range opts {
		opt(s)
	}
	if s.formulas == nil {
		s.formulas = DefaultFormulaRegistry()
	}
	return s
}

// ── AccrueTax — Event 5a ──────────────────────────────────────────────────────

// AccrueTax mencatat akrual kewajiban PPh Final saat pengalihan (Event 5a).
// Tarif diambil dari TaxRateProvider (bukan konstanta inline).
// Jurnal: Dr 5-2000 Beban PPh Final / Cr 2-4000 Hutang PPh Final.
// TaxAmount = round(TransferValue × tarif, 0) — rupiah bulat (Invariant #2).
//
// // TODO(tax-advisor): konfirmasi bahwa basis = nilai pengalihan bruto (bukan DPP PPN)
// dan pajak ini final (tidak dapat dikreditkan). Konfirmasi perlakuan HGB/leasehold LITHOS.
func (s *Service) AccrueTax(ctx context.Context, tenantID uint64, req AccrueTaxRequest) (*TaxObligation, error) {
	if !req.TransferValue.IsWholeRupiah() {
		return nil, ErrTransferValueFractional
	}
	if req.TransferValue.IsZero() || req.TransferValue.IsNeg() {
		return nil, ErrTransferValueZeroOrNeg
	}
	if req.RateCode == "" {
		req.RateCode = RateCodePPhFinalPengalihan
	}
	if req.AccrualDate.IsZero() {
		req.AccrualDate = time.Now()
	}

	// Pilih Tax Rule (Increment 4): resolusi per KATEGORI PROYEK (subsidi 1% vs
	// komersial 2,5%) + akun dari konfigurasi rule. Tanpa resolver terpasang →
	// jalur legacy (tarif per rate_code saja, akun default).
	var taxRate *TaxRate
	var err error
	if s.ruleResolver != nil {
		category := domain.TaxCategoryKomersial // default konservatif (tarif tertinggi)
		if req.ProjectID != nil && s.projectCategories != nil {
			category, err = s.projectCategories.GetProjectTaxCategory(ctx, tenantID, *req.ProjectID)
			if err != nil {
				return nil, err
			}
		}
		taxRate, err = s.ruleResolver.ResolveRule(ctx, tenantID, req.RateCode, category, req.AccrualDate)
	} else {
		taxRate, err = s.rates.GetCurrentRate(ctx, tenantID, req.RateCode, req.AccrualDate)
	}
	if err != nil {
		return nil, err
	}

	// Hitung via FORMULA rule (seam TaxFormula): proportional = rate × base,
	// rupiah bulat. Formula lain menyusul tanpa menyentuh alur ini.
	formula, err := s.formulas.Formula(taxRate.Formula)
	if err != nil {
		return nil, err
	}
	taxAmount, err := formula.Compute(taxRate, req.TransferValue)
	if err != nil {
		return nil, err
	}

	// Resolve account IDs — akun dari KONFIGURASI rule (default legacy bila kosong).
	debitCode := taxRate.DebitAccountOrDefault()
	creditCode := taxRate.CreditAccountOrDefault()
	bebanAccID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, debitCode)
	if err != nil {
		return nil, fmt.Errorf("cari akun %s: %w", debitCode, err)
	}
	hutangAccID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, creditCode)
	if err != nil {
		return nil, fmt.Errorf("cari akun %s: %w", creditCode, err)
	}

	// Build jurnal 5a: Dr 5-2000 / Cr 2-4000.
	lines := []JournalLineInput{
		{AccountID: bebanAccID, Debit: taxAmount, UnitID: req.UnitID, ProjectID: req.ProjectID,
			Description: fmt.Sprintf("PPh Final pengalihan — tarif %s", taxRate.Rate.String())},
		{AccountID: hutangAccID, Credit: taxAmount, UnitID: req.UnitID, ProjectID: req.ProjectID,
			Description: fmt.Sprintf("Hutang PPh Final pengalihan — tarif %s", taxRate.Rate.String())},
	}

	var unitDesc string
	if req.UnitID != nil {
		unitDesc = fmt.Sprintf("%d", *req.UnitID)
	} else {
		unitDesc = "?"
	}
	journalID, err := s.journals.CreateJournal(ctx, tenantID, req.AccrualDate,
		fmt.Sprintf("Akrual PPh Final Event 5a — unit %s", unitDesc), lines)
	if err != nil {
		return nil, fmt.Errorf("buat jurnal 5a: %w", err)
	}
	// Akrual: Dr Beban PPh / Cr Utang PPh — belum ada kas yang bergerak.
	if err := s.journals.PostJournal(ctx, tenantID, journalID, false); err != nil {
		return nil, fmt.Errorf("posting jurnal 5a: %w", err)
	}

	// Provenance rule (Increment 4): rule mana yang dipakai + cakupannya —
	// audit "kenapa unit ini kena 1%" terjawab dari obligation itu sendiri.
	var ruleID *uint64
	var ruleRevision *int
	if taxRate.ID != 0 {
		id := taxRate.ID
		ruleID = &id
		rev := taxRate.Revision
		if rev == 0 {
			rev = 1
		}
		ruleRevision = &rev
	}
	obligation := &TaxObligation{
		TenantID:       tenantID,
		UnitID:         req.UnitID,
		ProjectID:      req.ProjectID,
		RateCode:       req.RateCode,
		TransferValue:  req.TransferValue,
		Rate:           taxRate.Rate, // snapshot tarif saat akrual
		TaxAmount:      taxAmount,
		Status:         ObligationStatusOutstanding,
		AccrualDate:    req.AccrualDate,
		JournalEntryID: journalID,
		TaxRuleID:       ruleID,
		TaxRuleRevision: ruleRevision,
		AppliesTo:       string(taxRate.AppliesTo),
	}
	if err := s.store.SaveObligation(ctx, obligation); err != nil {
		return nil, fmt.Errorf("simpan obligation: %w", err)
	}
	return obligation, nil
}

// ── PayTax — Event 5b ─────────────────────────────────────────────────────────

// PayTax mencatat pelunasan kewajiban PPh Final ke kas negara (Event 5b).
// Jurnal: Dr 2-4000 Hutang PPh Final / Cr 1-13xx Bank.
// W-3.0 (B-1 + B-2): akun bank divalidasi COA-driven, dan keempat tulisan
// (jurnal, posting, status obligation, baris payment) berjalan dalam satu
// transaksi. Ini jalur kas keluar O-2 — dokumen BKK menyusul di W-3.2.
func (s *Service) PayTax(ctx context.Context, tenantID uint64, req PayTaxRequest) (*TaxPayment, error) {
	// B-1: kas/bank menurut COA, bukan daftar kode di dalam kode program.
	bankAccID, err := s.accounts.ResolvePaymentAccountID(ctx, tenantID, req.BankAccountCode)
	if err != nil {
		return nil, err
	}
	hutangAccID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, "2-4000")
	if err != nil {
		return nil, fmt.Errorf("cari akun 2-4000: %w", err)
	}
	if req.PaymentDate.IsZero() {
		req.PaymentDate = time.Now()
	}

	var payment *TaxPayment
	err = s.inTx(ctx, func(journals JournalWriter, store TaxStore) error {
		obligation, err := store.FindObligationByID(ctx, tenantID, req.ObligationID)
		if err != nil {
			return err
		}
		if obligation.Status == ObligationStatusPaid {
			return ErrObligationAlreadyPaid
		}

		// Build jurnal 5b: Dr 2-4000 / Cr Bank.
		amount := obligation.TaxAmount
		lines := []JournalLineInput{
			{AccountID: hutangAccID, Debit: amount,
				Description: fmt.Sprintf("Lunasi Hutang PPh Final obligation#%d", obligation.ID)},
			{AccountID: bankAccID, Credit: amount,
				Description: fmt.Sprintf("Setor PPh Final ke kas negara obligation#%d", obligation.ID)},
		}

		journalID, err := journals.CreateJournal(ctx, tenantID, req.PaymentDate,
			fmt.Sprintf("Setor PPh Final Event 5b — obligation %d", obligation.ID), lines)
		if err != nil {
			return fmt.Errorf("buat jurnal 5b: %w", err)
		}
		// Setoran ke kas negara — kas keluar (O-2), dokumen BKK.
		if err := journals.PostJournal(ctx, tenantID, journalID, true); err != nil {
			return fmt.Errorf("posting jurnal 5b: %w", err)
		}

		// Update status obligation → paid.
		if err := store.UpdateObligationStatus(ctx, tenantID, obligation.ID, ObligationStatusPaid); err != nil {
			return fmt.Errorf("update status obligation: %w", err)
		}

		p := &TaxPayment{
			TenantID:        tenantID,
			ObligationID:    obligation.ID,
			BankAccountCode: req.BankAccountCode,
			Amount:          amount,
			PaymentDate:     req.PaymentDate,
			JournalEntryID:  journalID,
		}
		if err := store.SavePayment(ctx, p); err != nil {
			return fmt.Errorf("simpan payment: %w", err)
		}
		payment = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return payment, nil
}

// inTx menjalankan fn dengan kolaborator terikat transaksi.
// Tanpa seam transaksi, fn berjalan dengan kolaborator biasa (perilaku lama).
func (s *Service) inTx(ctx context.Context, fn func(JournalWriter, TaxStore) error) error {
	if s.tx == nil {
		return fn(s.journals, s.store)
	}
	return s.tx.InTx(ctx, fn)
}

// ── SetTaxRate ────────────────────────────────────────────────────────────────

// SetTaxRate menyimpan Tax Rule baru yang berlaku mulai tanggal tertentu.
// Inilah satu-satunya tempat di mana tarif/cakupan/akun bisa dikonfigurasi —
// tidak ada konstanta tarif di kode posting manapun. Regulasi berubah = baris
// rule baru dengan effective_from baru (append, bukan edit — histori terjaga).
func (s *Service) SetTaxRate(ctx context.Context, tenantID uint64, req SetTaxRateRequest) error {
	if req.RateCode == "" {
		return ErrRateCodeRequired
	}
	if req.Rate.LessThanOrEqual(decimal.Zero) || req.Rate.GreaterThan(decimal.NewFromInt(1)) {
		return ErrRateValueInvalid
	}
	if req.EffectiveFrom.IsZero() {
		return ErrEffectiveDateRequired
	}
	if req.AppliesTo == "" {
		req.AppliesTo = AppliesToAll
	}
	if !req.AppliesTo.Valid() {
		return ErrAppliesToInvalid
	}
	if req.EffectiveTo != nil && req.EffectiveTo.Before(req.EffectiveFrom) {
		return ErrEffectiveRangeInvalid
	}
	// Akun jurnal: keduanya diisi atau keduanya kosong (kosong = default legacy).
	if (req.DebitAccount == "") != (req.CreditAccount == "") {
		return ErrRuleAccountsIncomplete
	}
	if req.TriggerEvent == "" {
		req.TriggerEvent = TriggerBAST
	}
	if !req.TriggerEvent.Valid() {
		return ErrTriggerEventInvalid
	}
	if req.CalcBase == "" {
		req.CalcBase = "transfer_value"
	}
	if req.Formula == "" {
		req.Formula = FormulaProportional
	}
	if !req.Formula.Valid() {
		return ErrFormulaInvalid
	}
	// Formula harus terimplementasi di registry (vocabulary boleh lebih luas,
	// tapi rule aktif tidak boleh menunjuk formula yang belum ada).
	if _, err := s.formulas.Formula(req.Formula); err != nil {
		return err
	}

	rate := &TaxRate{
		TenantID:      tenantID,
		RateCode:      req.RateCode,
		Name:          req.Name,
		Rate:          req.Rate,
		AppliesTo:     req.AppliesTo,
		TriggerEvent:  req.TriggerEvent,
		CalcBase:      req.CalcBase,
		Formula:       req.Formula,
		Revision:      1,
		DebitAccount:  req.DebitAccount,
		CreditAccount: req.CreditAccount,
		EffectiveFrom: req.EffectiveFrom,
		EffectiveTo:   req.EffectiveTo,
		IsActive:      true,
		Description:   req.Description,
	}
	return s.rates.SaveRate(ctx, rate)
}

// ── GetTaxReport ──────────────────────────────────────────────────────────────

// ── ListBASTWithoutPPhFinal — penjaga audit ───────────────────────────────────

// ListBASTWithoutPPhFinal mengembalikan unit yang sudah BAST dalam periode tetapi
// BELUM ada akrual PPh Final (Event 5a). Ini adalah penjaga audit — operator
// melihat daftar ini dan memanggil POST /units/{id}/tax/accrue untuk tiap unit.
//
// Membutuhkan WithBASTReader dan WithLedgerReader terpasang.
func (s *Service) ListBASTWithoutPPhFinal(ctx context.Context, tenantID uint64, from, to time.Time) ([]BASTWithoutPPhFinalItem, error) {
	if s.bastReader == nil {
		return nil, ErrBASTReaderNotConfigured
	}

	bastUnitIDs, err := s.bastReader.ListBASTUnitIDsInPeriod(ctx, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("ListBASTUnitIDsInPeriod: %w", err)
	}
	if len(bastUnitIDs) == 0 {
		return nil, nil
	}

	obligations, err := s.store.ListObligationsForUnits(ctx, tenantID, RateCodePPhFinalPengalihan, bastUnitIDs)
	if err != nil {
		return nil, fmt.Errorf("ListObligationsForUnits: %w", err)
	}

	obligatedUnits := make(map[uint64]bool, len(obligations))
	for _, o := range obligations {
		if o.UnitID != nil {
			obligatedUnits[*o.UnitID] = true
		}
	}

	var result []BASTWithoutPPhFinalItem
	for _, uid := range bastUnitIDs {
		if !obligatedUnits[uid] {
			result = append(result, BASTWithoutPPhFinalItem{UnitID: uid})
		}
	}
	return result, nil
}

// ── GetVATReport — laporan PPN rekonsiliasi ke ledger ────────────────────────

// GetVATReport menghitung laporan PPN per periode.
// PPNKeluaran = Σ kredit akun 2-3000 di jurnal yang diposting dalam periode.
// PPNMasukan  = Σ debit  akun 1-5100 di jurnal yang diposting dalam periode.
// PPNTerutang = PPNKeluaran − PPNMasukan.
//
// Angka ini DIREKONSILIASI ke ledger — bukan dari tabel terpisah.
// Membutuhkan WithLedgerReader terpasang.
func (s *Service) GetVATReport(ctx context.Context, tenantID uint64, from, to time.Time) (*VATReport, error) {
	if s.ledgerReader == nil {
		return nil, ErrLedgerReaderNotConfigured
	}

	ppnKeluaran, err := s.ledgerReader.SumCreditByAccountCodeAndPeriod(ctx, tenantID, "2-3000", from, to)
	if err != nil {
		return nil, fmt.Errorf("sum kredit 2-3000: %w", err)
	}
	ppnMasukan, err := s.ledgerReader.SumDebitByAccountCodeAndPeriod(ctx, tenantID, "1-5100", from, to)
	if err != nil {
		return nil, fmt.Errorf("sum debit 1-5100: %w", err)
	}

	terutangDec := ppnKeluaran.Decimal().Sub(ppnMasukan.Decimal())
	ppnTerutang := domain.FromDecimal(terutangDec)

	return &VATReport{
		PeriodFrom:  from,
		PeriodTo:    to,
		PPNKeluaran: ppnKeluaran.String(),
		PPNMasukan:  ppnMasukan.String(),
		PPNTerutang: ppnTerutang.String(),
	}, nil
}

// ── GetCombinedTaxReport — laporan kewajiban pajak gabungan ──────────────────

// GetCombinedTaxReport menggabungkan laporan PPh Final dan PPN dalam satu respons.
// Membutuhkan WithLedgerReader terpasang (untuk bagian PPN).
func (s *Service) GetCombinedTaxReport(ctx context.Context, tenantID uint64, from, to time.Time) (*CombinedTaxReport, error) {
	pphReport, err := s.GetTaxReport(ctx, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("GetTaxReport: %w", err)
	}

	vatReport, err := s.GetVATReport(ctx, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("GetVATReport: %w", err)
	}

	return &CombinedTaxReport{
		PeriodFrom: from,
		PeriodTo:   to,
		PPHFinal:   pphReport,
		PPN:        vatReport,
	}, nil
}

// ── GetTaxReport ──────────────────────────────────────────────────────────────

// GetTaxReport mengembalikan laporan kewajiban PPh Final per periode untuk satu tenant.
func (s *Service) GetTaxReport(ctx context.Context, tenantID uint64, from, to time.Time) (*TaxReport, error) {
	obligations, err := s.store.ListObligationsByPeriod(ctx, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list obligations: %w", err)
	}

	var totalObl, totalPaid, totalOutstanding domain.Money
	items := make([]TaxReportItem, 0, len(obligations))

	for _, o := range obligations {
		totalObl = totalObl.Add(o.TaxAmount)
		if o.Status == ObligationStatusPaid {
			totalPaid = totalPaid.Add(o.TaxAmount)
		} else {
			totalOutstanding = totalOutstanding.Add(o.TaxAmount)
		}
		items = append(items, TaxReportItem{
			ObligationID:  o.ID,
			UnitID:        o.UnitID,
			RateCode:      o.RateCode,
			TransferValue: o.TransferValue.String(),
			Rate:          o.Rate.String(),
			TaxAmount:     o.TaxAmount.String(),
			Status:        o.Status,
			AccrualDate:   o.AccrualDate,
		})
	}

	return &TaxReport{
		PeriodFrom:       from,
		PeriodTo:         to,
		Items:            items,
		TotalObligation:  totalObl.String(),
		TotalPaid:        totalPaid.String(),
		TotalOutstanding: totalOutstanding.String(),
	}, nil
}
