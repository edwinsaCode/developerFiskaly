package tax

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// GORMRepository mengimplementasi TaxRateProvider, AccountFinder, JournalWriter, dan TaxStore.
type GORMRepository struct {
	db      *gorm.DB
	posting *ledger.PostingService
}

func NewGORMRepository(db *gorm.DB, posting *ledger.PostingService) *GORMRepository {
	return &GORMRepository{db: db, posting: posting}
}

// ── AccountFinder ─────────────────────────────────────────────────────────────

func (r *GORMRepository) FindAccountIDByCode(ctx context.Context, tenantID uint64, code string) (uint64, error) {
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Select("id").
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		return 0, fmt.Errorf("akun %q tidak ditemukan: %w", code, err)
	}
	return acc.ID, nil
}

// ResolvePaymentAccountID mencari akun `code` lalu memvalidasinya sebagai akun
// kas/bank lewat otoritas klasifikasi tunggal ledger.ValidatePaymentAccount
// (W-3.0 B-1) — sama seperti sale, charge, dan notary. Tidak ada daftar kode
// bank di dalam package tax.
func (r *GORMRepository) ResolvePaymentAccountID(ctx context.Context, tenantID uint64, code string) (uint64, error) {
	if code == "" {
		return 0, ErrInvalidBankAccount
	}
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		return 0, ErrInvalidBankAccount
	}
	if err := ledger.ValidatePaymentAccount(&acc); err != nil {
		return 0, ErrInvalidBankAccount
	}
	return acc.ID, nil
}

// ── TxRunner (W-3.0 B-2) ──────────────────────────────────────────────────────

// InTx menjalankan fn dalam satu transaksi dengan kolaborator terikat transaksi.
// PostingService dibangun ulang di atas repo transaksional mengikuti idiom yang
// sudah dipakai AccruePPhFinalInTx.
func (r *GORMRepository) InTx(ctx context.Context, fn func(JournalWriter, TaxStore) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
		txRepo := &GORMRepository{db: tx, posting: txPosting}
		return fn(txRepo, txRepo)
	})
}

// ── JournalWriter ─────────────────────────────────────────────────────────────

func (r *GORMRepository) CreateJournal(ctx context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (uint64, error) {
	req := ledger.CreateJournalRequest{
		TenantID:    tenantID,
		Date:        date,
		Description: description,
		Lines:       toledgerLines(lines),
	}
	entry, err := r.posting.Create(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("buat jurnal pajak: %w", err)
	}
	return entry.ID, nil
}

// PostJournal memposting jurnal pajak. Untuk setoran kas (O-2) dokumen BKK
// terbit di transaksi yang sama lewat PostDraft.
func (r *GORMRepository) PostJournal(ctx context.Context, tenantID, journalID uint64, cashOut bool) error {
	if !cashOut {
		_, err := r.posting.Post(ctx, tenantID, journalID)
		return err
	}
	_, err := r.posting.PostDraft(ctx, tenantID, journalID, ledger.DocumentSpec{TypeCode: ledger.DocCashOut})
	return err
}

// ── TaxRateProvider ───────────────────────────────────────────────────────────

// GetCurrentRate mengembalikan tarif aktif per tanggal referensi.
// Tarif aktif = tarif dengan EffectiveFrom terbesar yang ≤ referenceDate.
// Jika tidak ditemukan → ErrRateNotConfigured (bukan default hardcoded).
func (r *GORMRepository) GetCurrentRate(ctx context.Context, tenantID uint64, rateCode string, referenceDate time.Time) (*TaxRate, error) {
	var rate TaxRate
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND rate_code = ? AND effective_from <= ?", tenantID, rateCode, referenceDate).
		Order("effective_from DESC").
		First(&rate).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRateNotConfigured
		}
		return nil, fmt.Errorf("GetCurrentRate: %w", err)
	}
	return &rate, nil
}

func (r *GORMRepository) SaveRate(ctx context.Context, rate *TaxRate) error {
	if err := r.db.WithContext(ctx).Create(rate).Error; err != nil {
		return fmt.Errorf("SaveRate: %w", err)
	}
	return nil
}

// ── TaxRuleResolver (Increment 4 — blueprint §7) ─────────────────────────────

// ResolveRule memilih Tax Rule untuk (rate_code, kategori proyek, tanggal event):
// aktif, berlaku pada tanggal (effective_from ≤ date ≤ effective_to|∞), dan
// applies_to cocok — kategori SPESIFIK menang atas 'all' (fallback); pada
// specificity sama, effective_from terbaru menang. Tidak ada rule →
// ErrRateNotConfigured (tidak pernah default diam-diam).
func (r *GORMRepository) ResolveRule(ctx context.Context, tenantID uint64, rateCode string, category domain.TaxCategory, referenceDate time.Time) (*TaxRate, error) {
	if !category.Valid() {
		// Tanpa kategori valid, hanya rule 'all' yang boleh cocok.
		category = domain.TaxCategory(AppliesToAll)
	}
	var rule TaxRate
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND rate_code = ? AND is_active = 1", tenantID, rateCode).
		Where("effective_from <= ?", referenceDate).
		Where("effective_to IS NULL OR effective_to >= ?", referenceDate).
		Where("applies_to IN ?", []string{string(category), string(AppliesToAll)}).
		// Specificity: kategori spesifik menang atas 'all'; lalu effective terbaru.
		// Nilai category berasal dari enum tervalidasi (bukan input bebas).
		Order(fmt.Sprintf("CASE WHEN applies_to = '%s' THEN 0 ELSE 1 END, effective_from DESC", string(category))).
		First(&rule).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRateNotConfigured
		}
		return nil, fmt.Errorf("ResolveRule: %w", err)
	}
	return &rule, nil
}

// ListRules mengembalikan seluruh rule tenant (untuk layar konfigurasi).
func (r *GORMRepository) ListRules(ctx context.Context, tenantID uint64) ([]*TaxRate, error) {
	var out []*TaxRate
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("rate_code ASC, applies_to ASC, effective_from DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListRules: %w", err)
	}
	return out, nil
}

// GetProjectTaxCategory membaca projects.tax_category (penentu tarif) tanpa
// mengimpor package project (query kolom langsung; tenant-scoped).
func (r *GORMRepository) GetProjectTaxCategory(ctx context.Context, tenantID, projectID uint64) (domain.TaxCategory, error) {
	var cat string
	err := r.db.WithContext(ctx).
		Table("projects").
		Select("tax_category").
		Where("id = ? AND tenant_id = ?", projectID, tenantID).
		Scan(&cat).Error
	if err != nil {
		return "", fmt.Errorf("GetProjectTaxCategory: %w", err)
	}
	if cat == "" {
		return "", ErrProjectNotFoundForTax
	}
	return domain.TaxCategory(cat), nil
}

// ResolveUnitProductPolicy membaca kebijakan produk sebuah unit (join
// units.unit_type → product_types.code) tanpa mengimpor package project —
// pola sama seperti GetProjectTaxCategory di atas. Rule klien UAT #3: dipakai
// AccrueTax untuk lapis penentu tarif per PRODUK (rumah_subsidi/komersial).
func (r *GORMRepository) ResolveUnitProductPolicy(ctx context.Context, tenantID, unitID uint64) (domain.ProductPolicy, error) {
	var row struct {
		Code               string  `gorm:"column:code"`
		Name               string  `gorm:"column:name"`
		Category           string  `gorm:"column:category"`
		RevenueAccountCode string  `gorm:"column:revenue_account_code"`
		TaxCategory        *string `gorm:"column:tax_category"`
		IsActive           bool    `gorm:"column:is_active"`
	}
	err := r.db.WithContext(ctx).
		Table("units u").
		Select("pt.code, pt.name, pt.category, pt.revenue_account_code, pt.tax_category, pt.is_active").
		Joins("JOIN product_types pt ON pt.tenant_id = u.tenant_id AND pt.code = u.unit_type").
		Where("u.tenant_id = ? AND u.id = ?", tenantID, unitID).
		Take(&row).Error
	if err != nil {
		return domain.ProductPolicy{}, fmt.Errorf("ResolveUnitProductPolicy(unit=%d): %w", unitID, err)
	}
	if !row.IsActive {
		return domain.ProductPolicy{}, fmt.Errorf("ResolveUnitProductPolicy(unit=%d): product type %q nonaktif", unitID, row.Code)
	}
	var taxCat *domain.TaxCategory
	if row.TaxCategory != nil {
		c := domain.TaxCategory(*row.TaxCategory)
		taxCat = &c
	}
	return domain.ProductPolicy{
		Code:               row.Code,
		Name:               row.Name,
		Category:           domain.ProductCategory(row.Category),
		RevenueAccountCode: row.RevenueAccountCode,
		TaxCategory:        taxCat,
	}, nil
}

// ── TaxStore ──────────────────────────────────────────────────────────────────

func (r *GORMRepository) SaveObligation(ctx context.Context, o *TaxObligation) error {
	if err := r.db.WithContext(ctx).Create(o).Error; err != nil {
		return fmt.Errorf("SaveObligation: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindObligationByID(ctx context.Context, tenantID, id uint64) (*TaxObligation, error) {
	var o TaxObligation
	err := r.db.WithContext(ctx).
		First(&o, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrObligationNotFound
		}
		return nil, fmt.Errorf("FindObligationByID: %w", err)
	}
	return &o, nil
}

func (r *GORMRepository) UpdateObligationStatus(ctx context.Context, tenantID, id uint64, status TaxObligationStatus) error {
	res := r.db.WithContext(ctx).Model(&TaxObligation{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", string(status))
	if res.Error != nil {
		return fmt.Errorf("UpdateObligationStatus: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrObligationNotFound
	}
	return nil
}

func (r *GORMRepository) SavePayment(ctx context.Context, p *TaxPayment) error {
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		return fmt.Errorf("SavePayment: %w", err)
	}
	return nil
}

func (r *GORMRepository) ListObligationsByPeriod(ctx context.Context, tenantID uint64, from, to time.Time) ([]*TaxObligation, error) {
	var obligations []*TaxObligation
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND accrual_date >= ? AND accrual_date <= ?", tenantID, from, to).
		Order("accrual_date ASC").
		Find(&obligations).Error
	if err != nil {
		return nil, fmt.Errorf("ListObligationsByPeriod: %w", err)
	}
	return obligations, nil
}

// ListObligationsForUnits mencari semua kewajiban PPh Final untuk sekumpulan unit.
// Digunakan oleh penjaga audit (ListBASTWithoutPPhFinal).
func (r *GORMRepository) ListObligationsForUnits(ctx context.Context, tenantID uint64, rateCode string, unitIDs []uint64) ([]*TaxObligation, error) {
	if len(unitIDs) == 0 {
		return nil, nil
	}
	var obligations []*TaxObligation
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND rate_code = ? AND unit_id IN ?", tenantID, rateCode, unitIDs).
		Find(&obligations).Error
	if err != nil {
		return nil, fmt.Errorf("ListObligationsForUnits: %w", err)
	}
	return obligations, nil
}

// ── BASTReader ────────────────────────────────────────────────────────────────

// ListBASTUnitIDsInPeriod mengembalikan unit_id yang sudah Akad (recognition_date) dalam periode.
// Membaca tabel sale_records (dikelola package sale; tidak ada import siklik karena hanya nama tabel).
func (r *GORMRepository) ListBASTUnitIDsInPeriod(ctx context.Context, tenantID uint64, from, to time.Time) ([]uint64, error) {
	type row struct {
		UnitID uint64 `gorm:"column:unit_id"`
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Table("sale_records").
		Select("unit_id").
		Where("tenant_id = ? AND recognition_date >= ? AND recognition_date <= ?", tenantID, from, to).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("ListBASTUnitIDsInPeriod: %w", err)
	}
	ids := make([]uint64, len(rows))
	for i, row := range rows {
		ids[i] = row.UnitID
	}
	return ids, nil
}

// ── LedgerAccountReader ───────────────────────────────────────────────────────

// SumCreditByAccountCodeAndPeriod menghitung Σ kredit dari sisi jurnal untuk satu kode akun.
// Hanya jurnal yang sudah diposting (posted_at IS NOT NULL) yang dihitung.
// Digunakan untuk rekonsiliasi PPN Keluaran ke akun 2-3000.
func (r *GORMRepository) SumCreditByAccountCodeAndPeriod(ctx context.Context, tenantID uint64, accountCode string, from, to time.Time) (domain.Money, error) {
	type sumResult struct {
		Total domain.Money `gorm:"column:total"`
	}
	var res sumResult
	err := r.db.WithContext(ctx).
		Table("journal_lines jl").
		Select("COALESCE(SUM(jl.credit), 0) AS total").
		Joins("INNER JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("INNER JOIN accounts a ON a.id = jl.account_id").
		Where("jl.tenant_id = ? AND a.code = ? AND je.posted_at IS NOT NULL AND je.date >= ? AND je.date <= ?",
			tenantID, accountCode, from, to).
		Scan(&res).Error
	if err != nil {
		return domain.Zero, fmt.Errorf("SumCreditByAccountCodeAndPeriod: %w", err)
	}
	return res.Total, nil
}

// SumDebitByAccountCodeAndPeriod menghitung Σ debit dari sisi jurnal untuk satu kode akun.
// Hanya jurnal yang sudah diposting yang dihitung.
// Digunakan untuk rekonsiliasi PPN Masukan ke akun 1-5100.
func (r *GORMRepository) SumDebitByAccountCodeAndPeriod(ctx context.Context, tenantID uint64, accountCode string, from, to time.Time) (domain.Money, error) {
	type sumResult struct {
		Total domain.Money `gorm:"column:total"`
	}
	var res sumResult
	err := r.db.WithContext(ctx).
		Table("journal_lines jl").
		Select("COALESCE(SUM(jl.debit), 0) AS total").
		Joins("INNER JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("INNER JOIN accounts a ON a.id = jl.account_id").
		Where("jl.tenant_id = ? AND a.code = ? AND je.posted_at IS NOT NULL AND je.date >= ? AND je.date <= ?",
			tenantID, accountCode, from, to).
		Scan(&res).Error
	if err != nil {
		return domain.Zero, fmt.Errorf("SumDebitByAccountCodeAndPeriod: %w", err)
	}
	return res.Total, nil
}

// ── PPhFinalAccruer (implements sale.PPhFinalAccruer) ─────────────────────────

// AccruePPhFinalInTx executes PPh Final accrual entirely within the caller's DB transaction.
// Used by sale.GORMRepository.Execute() so the tax journal is part of the Akad atomicity.
// Creates tx-scoped repos to avoid using the outer non-transactional db.
func (r *GORMRepository) AccruePPhFinalInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID, projectID uint64, transferValue domain.Money, recognitionDate time.Time) error {
	txLedgerRepo := ledger.NewGORMRepository(tx)
	txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
	txRepo := &GORMRepository{db: tx, posting: txPosting}
	svc := NewService(txRepo, txRepo, txRepo, txRepo,
		WithRuleResolution(txRepo, txRepo), // Increment 4: tarif per kategori proyek
		WithUnitProductPolicy(txRepo))      // rule klien UAT #3: tarif per PRODUK unit
	_, err := svc.AccrueTax(ctx, tenantID, AccrueTaxRequest{
		UnitID:        &unitID,
		ProjectID:     &projectID,
		TransferValue: transferValue,
		AccrualDate:   recognitionDate,
		RateCode:      RateCodePPhFinalPengalihan,
	})
	return err
}

// ── SeedDefaultRates — tarif PP 34/2016 ──────────────────────────────────────

// SeedDefaultRates menanam tarif default PPh Final 2,5% (PP 34/2016).
// Dipanggil saat tenant baru dibuat atau dari cmd/seed.
// Inilah satu-satunya tempat di mana angka 0.025 muncul — bukan di kode posting.
func SeedDefaultRates(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	effectiveDate := time.Date(2016, 5, 8, 0, 0, 0, 0, time.UTC) // PP 34/2016

	// Seed default — angka tarif hanya ada di sini, TIDAK di kode posting.
	// Increment 4: rule per kategori (subsidi 1% / komersial 2,5%) + fallback
	// 'all' 2,5% + akun jurnal dari konfigurasi. Sumber angka yang SAMA dengan
	// seed migrasi 000039 untuk tenant existing — jaga tetap sinkron.
	defaults := []*TaxRate{
		{
			TenantID: tenantID, RateCode: RateCodePPhFinalPengalihan,
			Name: "PPh Final Pengalihan (default)", Rate: decimal.RequireFromString("0.025000"),
			AppliesTo: AppliesToAll, TriggerEvent: "bast", CalcBase: "transfer_value",
			DebitAccount: DefaultPPhExpenseAccount, CreditAccount: DefaultPPhPayableAccount,
			EffectiveFrom: effectiveDate, IsActive: true,
			Description: "Tarif default PP 34/2016: 2,5% atas nilai pengalihan bruto",
		},
		{
			TenantID: tenantID, RateCode: RateCodePPhFinalPengalihan,
			Name: "PPh Final Properti Subsidi", Rate: decimal.RequireFromString("0.010000"),
			AppliesTo: AppliesToSubsidi, TriggerEvent: "bast", CalcBase: "transfer_value",
			DebitAccount: DefaultPPhExpenseAccount, CreditAccount: DefaultPPhPayableAccount,
			EffectiveFrom: effectiveDate, IsActive: true,
			Description: "PP 34/2016: 1% atas pengalihan RS/RSS oleh developer",
		},
		{
			TenantID: tenantID, RateCode: RateCodePPhFinalPengalihan,
			Name: "PPh Final Properti Komersial", Rate: decimal.RequireFromString("0.025000"),
			AppliesTo: AppliesToKomersial, TriggerEvent: "bast", CalcBase: "transfer_value",
			DebitAccount: DefaultPPhExpenseAccount, CreditAccount: DefaultPPhPayableAccount,
			EffectiveFrom: effectiveDate, IsActive: true,
			Description: "PP 34/2016: 2,5% atas pengalihan umum",
		},
	}
	for _, rate := range defaults {
		var n int64
		if err := db.WithContext(ctx).Model(&TaxRate{}).
			Where("tenant_id = ? AND rate_code = ? AND applies_to = ? AND effective_from = ?",
				tenantID, rate.RateCode, rate.AppliesTo, rate.EffectiveFrom).
			Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if err := db.WithContext(ctx).Create(rate).Error; err != nil {
			return err
		}
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toledgerLines(lines []JournalLineInput) []ledger.LineInput {
	out := make([]ledger.LineInput, len(lines))
	for i, l := range lines {
		out[i] = ledger.LineInput{
			AccountID:   l.AccountID,
			Debit:       l.Debit,
			Credit:      l.Credit,
			ProjectID:   l.ProjectID,
			PhaseID:     nil,
			UnitID:      l.UnitID,
			Description: l.Description,
		}
	}
	return out
}
