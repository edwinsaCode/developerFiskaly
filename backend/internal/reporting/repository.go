package reporting

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// GORMRepository mengimplementasikan PLReader, PipelineReader, CashFlowReader.
// Semua query adalah READ-ONLY — tidak ada operasi tulis.
type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ── PLReader ──────────────────────────────────────────────────────────────────

func (r *GORMRepository) GetProjectPLRows(ctx context.Context, tenantID, projectID uint64, from *time.Time, asOf time.Time) ([]PLRawRow, error) {
	return r.queryPLRows(ctx, tenantID, &projectID, from, &asOf)
}

func (r *GORMRepository) GetConsolidatedPLRows(ctx context.Context, tenantID uint64, from *time.Time, asOf time.Time) ([]PLRawRow, error) {
	return r.queryPLRows(ctx, tenantID, nil, from, &asOf)
}

// queryPLRows mengambil baris P&L (4-xxxx dan 5-xxxx) untuk satu atau semua proyek.
// projectID=nil → konsolidasi (semua proyek + korporat).
// projectID!=nil → per-proyek. from/asOf nil = tanpa batas tanggal sisi tersebut.
func (r *GORMRepository) queryPLRows(ctx context.Context, tenantID uint64, projectID *uint64, from, asOf *time.Time) ([]PLRawRow, error) {
	q := r.db.WithContext(ctx).
		Table("journal_lines jl").
		Select("a.code as account_code, a.name as account_name, "+
			"COALESCE(SUM(jl.debit), 0) as total_debit, "+
			"COALESCE(SUM(jl.credit), 0) as total_credit").
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("JOIN accounts a ON a.id = jl.account_id").
		Where("jl.tenant_id = ? AND je.posted_at IS NOT NULL AND (a.code LIKE ? OR a.code LIKE ?)",
			tenantID, "4-%", "5-%")

	if from != nil {
		q = q.Where("je.date >= ?", *from)
	}
	if asOf != nil {
		q = q.Where("je.date <= ?", *asOf)
	}
	if projectID != nil {
		q = q.Where("jl.project_id = ?", *projectID)
	}

	var rows []PLRawRow
	if err := q.Group("a.id, a.code, a.name").Order("a.code").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("reporting: queryPLRows: %w", err)
	}
	return rows, nil
}

// GetPLRowsAllByProject (S4): baris P&L SELURUH proyek dalam satu query,
// dikelompokkan per project_id (hanya baris ber-tag project). Dashboard memakai
// ini + ComputePL per proyek — menggantikan subquery revenue/hpp `led` yang
// menghitung sendiri (dulu exclude 4-2000; kini SATU definisi dengan P&L).
func (r *GORMRepository) GetPLRowsAllByProject(ctx context.Context, tenantID uint64) (map[uint64][]PLRawRow, error) {
	type projRow struct {
		ProjectID uint64 `gorm:"column:project_id"`
		PLRawRow
	}
	var rows []projRow
	err := r.db.WithContext(ctx).
		Table("journal_lines jl").
		Select("jl.project_id as project_id, a.code as account_code, a.name as account_name, "+
			"COALESCE(SUM(jl.debit), 0) as total_debit, "+
			"COALESCE(SUM(jl.credit), 0) as total_credit").
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("JOIN accounts a ON a.id = jl.account_id").
		Where("jl.tenant_id = ? AND je.posted_at IS NOT NULL AND jl.project_id IS NOT NULL AND (a.code LIKE ? OR a.code LIKE ?)",
			tenantID, "4-%", "5-%").
		Group("jl.project_id, a.id, a.code, a.name").
		Order("jl.project_id, a.code").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("reporting: GetPLRowsAllByProject: %w", err)
	}
	out := make(map[uint64][]PLRawRow)
	for _, r2 := range rows {
		out[r2.ProjectID] = append(out[r2.ProjectID], r2.PLRawRow)
	}
	return out, nil
}

// GetUnitPLRows (S4, disempitkan rule klien 2026-07-29): pendapatan PENJUALAN
// RUMAH (peran unit_sales_revenue = 4-1000) dan HPP (peran cogs) per UNIT dari
// ledger posted ber-tag unit. Pendapatan Booking (4-2100) / pendapatan lain
// SENGAJA dikecualikan — laporan per-unit adalah laporan PENJUALAN RUMAH;
// booking hanya tampil di Laba Rugi sebagai akun tersendiri.
func (r *GORMRepository) GetUnitPLRows(ctx context.Context, tenantID uint64) ([]UnitPLRow, error) {
	cogs := ledger.RoleCodeList(ledger.RoleCOGS)
	houseRev := ledger.RoleCodeList(ledger.RoleUnitSalesRevenue)
	var rows []UnitPLRow
	err := r.db.WithContext(ctx).
		Table("journal_lines jl").
		Select("jl.unit_id as unit_id, "+
			"COALESCE(SUM(CASE WHEN a.code IN ? THEN jl.credit - jl.debit ELSE 0 END), 0) as revenue, "+
			"COALESCE(SUM(CASE WHEN a.code IN ? THEN jl.debit - jl.credit ELSE 0 END), 0) as hpp", houseRev, cogs).
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("JOIN accounts a ON a.id = jl.account_id").
		Where("jl.tenant_id = ? AND je.posted_at IS NOT NULL AND jl.unit_id IS NOT NULL", tenantID).
		Where("(a.code IN ? OR a.code IN ?)", houseRev, cogs).
		Group("jl.unit_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("reporting: GetUnitPLRows: %w", err)
	}
	return rows, nil
}

// GetRevenueByPaymentType (P3, item A — Dashboard Cash vs KPR): pendapatan
// INTI (semua 4-x KECUALI RoleOtherIncome — definisi SAMA dengan
// PLReport.TotalPendapatan) dipecah per payment_type kontrak (sale.PaymentType,
// kanonik, sudah termasuk kontrak ber-scheme via LegacyPaymentType) lewat LEFT
// JOIN sale_contracts pada unit_id. Baris tanpa unit_id atau tanpa kontrak
// keluar dengan payment_type = "" — TIDAK dipaksa ke salah satu sisi (no
// silent misclassification, cross-cutting requirement I). from/asOf nil =
// tanpa batas tanggal sisi tersebut.
func (r *GORMRepository) GetRevenueByPaymentType(ctx context.Context, tenantID uint64, from, asOf *time.Time) ([]RevenueByPaymentTypeRow, error) {
	otherIncome := ledger.RoleCodeList(ledger.RoleOtherIncome)

	q := r.db.WithContext(ctx).
		Table("journal_lines jl").
		Select("COALESCE(sc.payment_type, '') as payment_type, "+
			"COALESCE(SUM(jl.credit - jl.debit), 0) as amount").
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Joins("JOIN accounts a ON a.id = jl.account_id").
		Joins("LEFT JOIN sale_contracts sc ON sc.unit_id = jl.unit_id AND sc.tenant_id = jl.tenant_id").
		Where("jl.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code LIKE ?", tenantID, "4-%")

	if len(otherIncome) > 0 {
		q = q.Where("a.code NOT IN ?", otherIncome)
	}
	if from != nil {
		q = q.Where("je.date >= ?", *from)
	}
	if asOf != nil {
		q = q.Where("je.date <= ?", *asOf)
	}

	var rows []RevenueByPaymentTypeRow
	if err := q.Group("payment_type").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("reporting: GetRevenueByPaymentType: %w", err)
	}
	return rows, nil
}

// GetContractValueByPaymentType (P3, item A): total nilai kontrak (GrossAmount,
// seluruh kontrak tenant, tidak dibatasi tanggal) per payment_type — komposisi
// PORTOFOLIO, bukan pendapatan diakui.
func (r *GORMRepository) GetContractValueByPaymentType(ctx context.Context, tenantID uint64) ([]RevenueByPaymentTypeRow, error) {
	var rows []RevenueByPaymentTypeRow
	err := r.db.WithContext(ctx).
		Table("sale_contracts").
		Select("payment_type, COALESCE(SUM(gross_amount), 0) as amount").
		Where("tenant_id = ?", tenantID).
		Group("payment_type").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("reporting: GetContractValueByPaymentType: %w", err)
	}
	return rows, nil
}

// S7: GetTaxLiabilityReport DIHAPUS dari repository — duplikat agregasi
// tax_obligations. Laporan pajak kini dibaca reporting.Service lewat pembaca
// KANONIK tax.Service.GetTaxReport (registry #14).

// ── PipelineReader ────────────────────────────────────────────────────────────

func (r *GORMRepository) GetPipelineStats(ctx context.Context, tenantID uint64) (*PipelineStats, error) {
	type statusRow struct {
		Status string       `gorm:"column:status"`
		Count  int64        `gorm:"column:cnt"`
		Sum    domain.Money `gorm:"column:list_sum"`
	}
	var statusRows []statusRow
	if err := r.db.WithContext(ctx).
		Table("units").
		Select("status, COUNT(*) as cnt, COALESCE(SUM(list_price), 0) as list_sum").
		Where("tenant_id = ?", tenantID).
		Group("status").
		Scan(&statusRows).Error; err != nil {
		return nil, fmt.Errorf("reporting: GetPipelineStats units: %w", err)
	}

	var stats PipelineStats
	for _, row := range statusRows {
		cnt := int(row.Count)
		switch row.Status {
		case "available":
			stats.AvailableCount = cnt
			stats.AvailableSum = row.Sum
		case "reserved":
			stats.ReservedCount = cnt
			stats.ReservedSum = row.Sum
		case "sold":
			stats.SoldCount = cnt
		}
	}

	type moneyResult struct {
		Total domain.Money `gorm:"column:total"`
	}

	// R4: TotalAdvance TIDAK lagi dihitung Σ termin_payments mentah di sini —
	// reporting.Service mengisinya dari sale.PortfolioFinancials (kanonik #8).

	var contractResult moneyResult
	if err := r.db.WithContext(ctx).
		Table("sale_records").
		Select("COALESCE(SUM(sale_price), 0) as total").
		Where("tenant_id = ?", tenantID).
		Scan(&contractResult).Error; err != nil {
		return nil, fmt.Errorf("reporting: GetPipelineStats contract: %w", err)
	}
	stats.SoldContractSum = contractResult.Total

	return &stats, nil
}

// ── (W-8) Piutang harga rumah tidak lagi dibaca dari sini ─────────────────────
//
// GetARScheduleRows dulu mengambil seluruh `payment_schedules` non-superseded
// dan menyebutnya piutang. Query itu tidak pernah menyentuh `sale_records`
// maupun jurnal, sehingga hasilnya tidak punya hubungan dengan akun kontrol
// piutang di buku besar: jadwal unit yang belum BAST ikut terhitung, sementara
// unit yang sudah BAST tanpa jadwal tidak muncul sama sekali. Keduanya terjadi
// bersamaan di data nyata (lihat docs/w8-house-ar-design.md §2).
//
// Pemiliknya sekarang paket `sale` — yang menulis peristiwa piutangnya —
// lewat HouseReceivableReader. Fungsinya dihapus, bukan dibiarkan menganggur:
// query yang masih bisa dipanggil adalah query yang cepat atau lambat dipanggil.

// ── CashFlowReader ────────────────────────────────────────────────────────────

type cashMovementRow struct {
	EntryID       uint64       `gorm:"column:entry_id"`
	Date          time.Time    `gorm:"column:entry_date"`
	Description   string       `gorm:"column:description"`
	NetBankChange domain.Money `gorm:"column:net_bank_change"`
	HasAsetTetap  bool         `gorm:"column:has_aset_tetap"`
	HasPendanaan  bool         `gorm:"column:has_pendanaan"`
}

// GetCashMovements mengambil semua gerakan kas/bank dalam periode dan mengklasifikasikannya.
// Klasifikasi: IsInvesting (ada 1-4xxx) → investasi; IsPendanaan (ada 2-5000/3-xxx) → pendanaan; sisanya → operasi.
// S6 (registry #1): keanggotaan kas/bank via COA category (cash|bank) — otoritas
// klasifikasi tunggal (RoleCashBank), BUKAN daftar kode hardcoded.
func (r *GORMRepository) GetCashMovements(ctx context.Context, tenantID uint64, from, to time.Time) ([]CashMovement, error) {
	sql := `
SELECT
    je.id AS entry_id,
    je.date AS entry_date,
    je.description,
    CAST(SUM(CASE WHEN a.category IN ('cash','bank')
                  THEN jl.debit - jl.credit ELSE 0 END) AS DECIMAL(20,4)) AS net_bank_change,
    MAX(CASE WHEN a.code LIKE '1-4%' AND a.code != '1-4900' THEN 1 ELSE 0 END) AS has_aset_tetap,
    MAX(CASE WHEN a.code = '2-5000' OR a.code LIKE '3-%' THEN 1 ELSE 0 END)    AS has_pendanaan
FROM journal_entries je
JOIN journal_lines jl ON jl.journal_entry_id = je.id AND jl.tenant_id = je.tenant_id
JOIN accounts a ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND je.date BETWEEN ? AND ?
GROUP BY je.id, je.date, je.description
HAVING SUM(CASE WHEN a.category IN ('cash','bank')
               THEN ABS(jl.debit) + ABS(jl.credit) ELSE 0 END) > 0
ORDER BY je.date, je.id`

	var rows []cashMovementRow
	if err := r.db.WithContext(ctx).Raw(sql, tenantID, from, to).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("reporting: GetCashMovements: %w", err)
	}

	result := make([]CashMovement, len(rows))
	for i, row := range rows {
		result[i] = CashMovement{
			EntryID:       row.EntryID,
			Date:          row.Date,
			Description:   row.Description,
			NetBankChange: row.NetBankChange,
			IsInvesting:   row.HasAsetTetap,
			IsPendanaan:   row.HasPendanaan,
		}
	}
	return result, nil
}

// GetMonthlyCashFlow (S6): SATU-SATUNYA agregat kas masuk/keluar per bulan
// (posted-only, keanggotaan akun via COA category cash|bank). Dashboard memakai
// ini untuk KPI CashIn/Out MTD DAN seri chart 6 bulan — satu implementasi.
// ── Print helpers (label lookup, tak menghitung apa pun) ─────────────────────

// TenantName mengambil nama perusahaan untuk header cetak laporan.
func (r *GORMRepository) TenantName(ctx context.Context, tenantID uint64) (string, error) {
	var name string
	err := r.db.WithContext(ctx).Raw(`SELECT name FROM tenants WHERE id = ?`, tenantID).Scan(&name).Error
	if err != nil {
		return "", fmt.Errorf("reporting: TenantName: %w", err)
	}
	return name, nil
}

// ProjectName mengambil nama proyek untuk judul cetak Laba Rugi per Proyek.
func (r *GORMRepository) ProjectName(ctx context.Context, tenantID, projectID uint64) (string, error) {
	var name string
	err := r.db.WithContext(ctx).Raw(`SELECT name FROM projects WHERE id = ? AND tenant_id = ?`, projectID, tenantID).Scan(&name).Error
	if err != nil {
		return "", fmt.Errorf("reporting: ProjectName: %w", err)
	}
	return name, nil
}

func (r *GORMRepository) GetMonthlyCashFlow(ctx context.Context, tenantID uint64, from time.Time) ([]CashFlowMonth, error) {
	var rows []struct {
		Month   string
		CashIn  string
		CashOut string
	}
	if err := r.db.WithContext(ctx).Raw(`
		SELECT DATE_FORMAT(je.date, '%Y-%m') AS month,
		  COALESCE(SUM(jl.debit), 0) AS cash_in,
		  COALESCE(SUM(jl.credit), 0) AS cash_out
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL
		  AND a.category IN ('cash','bank') AND je.date >= ?
		GROUP BY DATE_FORMAT(je.date, '%Y-%m')
		ORDER BY month`, tenantID, from).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("reporting: GetMonthlyCashFlow: %w", err)
	}
	out := make([]CashFlowMonth, 0, len(rows))
	for _, r2 := range rows {
		out = append(out, CashFlowMonth{Month: r2.Month, CashIn: nz(r2.CashIn), CashOut: nz(r2.CashOut)})
	}
	return out, nil
}
