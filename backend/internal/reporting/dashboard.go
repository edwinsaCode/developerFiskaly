package reporting

// PS-2 — Dashboard Owner V2: SATU endpoint komposit agar dashboard termuat
// sekali jalan (owner paham kondisi perusahaan ≤10 detik). Semua angka DERIVED
// dari ledger posted + tabel dokumen — tidak ada saldo tersimpan (ledger-centric).

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ContractFinanceReader (S3): sumber KANONIK outstanding/total_paid kontrak —
// diimplementasi oleh sale.Service (ContractFinancialSummary). Reporting membaca
// ini, tidak menghitung `gross − Σtermin` sendiri di SQL.
type ContractFinanceReader interface {
	PortfolioOutstanding(ctx context.Context, tenantID uint64) (domain.Money, error)
	ContractOutstandingPaid(ctx context.Context, tenantID, contractID uint64) (outstanding, totalPaid domain.Money, err error)
	// PortfolioFinancials (R4): baris kanonik per kontrak aktif — dasar
	// contract_value/collected per proyek & per salesperson. SATU definisi
	// "collected" == TotalPaid kanonik (counts_toward_price=TRUE).
	PortfolioFinancials(ctx context.Context, tenantID uint64) ([]domain.ContractPortfolioRow, error)
}

// BudgetTotalsReader (S5): sumber KANONIK total RAB aktif per proyek —
// diimplementasi oleh budget.Service (registry #12). Dashboard tidak lagi
// menjumlahkan budget_items lewat SQL sendiri.
type BudgetTotalsReader interface {
	TotalActiveBudgetByProject(ctx context.Context, tenantID uint64) (map[uint64]domain.Money, error)
}

// ── Response shapes (semua uang string desimal) ───────────────────────────────

type DashboardKPI struct {
	// Baris 1 — posisi hari ini (saldo akun, posted-only).
	Cash              string `json:"cash"`               // Σ akun category cash|bank (debit-normal)
	Receivable        string `json:"receivable"`         // 1-2xxx piutang (1-2000 + 1-2100 + 1-2200)
	Payable           string `json:"payable"`            // 2-1000 hutang usaha
	BookingDeposits   string `json:"booking_deposits"`   // 2-2100 titipan booking
	CommissionPayable string `json:"commission_payable"` // 2-6200 utang komisi

	// W-11 (R-11). Retensi kontraktor SENGAJA tidak dilebur ke Payable:
	// RolePayable juga dipakai posting biaya untuk memilih akun kredit, sehingga
	// menambah 2-1100 ke sana akan mengubah perilaku PENCATATAN demi angka
	// laporan. Retensi juga bukan hutang yang jatuh tempo bersama termin — ia
	// tertahan sampai masa pemeliharaan selesai, jadi menjumlahkannya ke satu
	// KPI akan menyesatkan pembaca soal kas yang harus disiapkan bulan ini.
	// Kewajiban vendor total = Payable + RetentionPayable.
	RetentionPayable string `json:"retention_payable"` // 2-1100 hutang retensi kontraktor

	// Baris 2 — bulan berjalan & pipeline.
	SalesMTD string `json:"sales_mtd"` // ComputePL bulan ini: pendapatan inti (S4, = Laba Rugi)

	// P3 (item A, 2026-08-27): pendapatan inti bulan ini dipecah Cash vs KPR
	// via payment_type kontrak (LEFT JOIN sale_contracts pada unit_id — baris
	// tanpa kontrak tidak masuk salah satu sisi, jadi Cash+KPR bisa < SalesMTD
	// bila ada pendapatan yang belum/tidak ber-kontrak).
	PendapatanCashMTD string `json:"pendapatan_cash_mtd"`
	PendapatanKPRMTD  string `json:"pendapatan_kpr_mtd"`
	// KontrakValueCash/KPR: komposisi nilai SELURUH kontrak (GrossAmount, semua
	// waktu, bukan MTD) — gambaran portofolio, bukan pendapatan diakui.
	KontrakValueCash string `json:"kontrak_value_cash"`
	KontrakValueKPR  string `json:"kontrak_value_kpr"`

	BookingActive   int    `json:"booking_active"`    // count booking status=active
	BookingFeeHeld  string `json:"booking_fee_held"`  // Σ fee ber-disposisi 'held' (dokumen; rekonsiliasi == saldo 2-2100). R4: termasuk fee converted yang menetap di titipan
	UnitsSoldMTD    int    `json:"units_sold_mtd"`    // BAST bulan ini (non-cancelled)
	MarginMTD       string `json:"margin_mtd"`        // ComputePL bulan ini: pendapatan − beban (S4, = laba P&L)
	CashInMTD       string `json:"cash_in_mtd"`       // Σ debit akun kas/bank bulan ini
	CashOutMTD      string `json:"cash_out_mtd"`      // Σ kredit akun kas/bank bulan ini
	NetCashFlowMTD  string `json:"net_cash_flow_mtd"` // in − out
	RefundPending   int    `json:"refund_pending"`
	CxAwaiting      int    `json:"cancellation_awaiting"`  // requested|approved
	CommissionQueue int    `json:"commission_queue"`       // calculated|approved|payable
	TrueupAwaiting  int    `json:"trueup_awaiting"`        // completion finalized tanpa run posted
	OverdueCount    int    `json:"overdue_schedules"`      // cicilan overdue
}

type DashboardProject struct {
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`

	// Progress fisik-finansial: realisasi biaya posted vs RAB aktif (0–100; >100 dibulatkan turun ke 100 di FE bila mau).
	BudgetTotal   string `json:"budget_total"`   // RAB aktif — budget.Service (kanonik, S5)
	ActualCost    string `json:"actual_cost"`    // ledger kanonik: debit − kredit-reversal, kode inventory registry (S5)
	ProgressPct   string `json:"progress_pct"`   // actual/budget × 100 (2 desimal; "0" bila RAB 0)

	// Funnel unit.
	UnitsAvailable int `json:"units_available"`
	UnitsBooked    int `json:"units_booked"`
	UnitsReserved  int `json:"units_reserved"`
	UnitsPPJB      int `json:"units_ppjb"`
	UnitsSold      int `json:"units_sold"`
	UnitsOther     int `json:"units_other"` // occupied|hold|blocked|maintenance
	UnitsTotal     int `json:"units_total"`

	// Kinerja (kumulatif, posted, tag project) — S4: DERIVED dari ComputePL
	// (satu definisi dengan Laba Rugi; revenue = SEMUA 4-%, margin = laba).
	Revenue    string `json:"revenue"`     // ComputePL proyek: total pendapatan (semua 4-%)
	HPP        string `json:"hpp"`         // Σ baris beban ber-peran cogs (registry)
	Margin     string `json:"margin"`      // ComputePL proyek: laba (pendapatan − beban)
	MarginPct  string `json:"margin_pct"`  // margin/revenue ×100 ("0" bila revenue 0)

	// Collection (R4): dari baris portfolio KANONIK (sale.PortfolioFinancials).
	ContractValue string `json:"contract_value"` // Σ NetContract kontrak aktif proyek ini
	Collected     string `json:"collected"`      // Σ TotalPaid kanonik (counts_toward_price=TRUE)
	CollectionPct string `json:"collection_pct"`

	// R3 — Progress FISIK (input manual append-only, entri terbaru).
	// nil = belum pernah diinput. Warning: biaya% − fisik% > 15pt →
	// indikasi biaya mendahului prestasi (over budget / prestasi tertinggal).
	PhysicalPct     *string `json:"physical_pct,omitempty"`
	PhysicalAsOf    *string `json:"physical_as_of,omitempty"`
	CostAheadWarning bool   `json:"cost_ahead_warning"`
}

// CashFlowMonth adalah satu titik seri arus kas bulanan (posted-only, akun
// category cash|bank) untuk chart Cash In vs Cash Out.
type CashFlowMonth struct {
	Month   string `json:"month"` // "2026-02"
	CashIn  string `json:"cash_in"`
	CashOut string `json:"cash_out"`
}

// DashboardFunnel adalah corong penjualan lintas dokumen: lead (CRM) →
// booking → kontrak → BAST. Count-only; nilai uang tetap dibaca dari ledger.
type DashboardFunnel struct {
	LeadsActive     int `json:"leads_active"`     // new|contacted|qualified
	LeadsConverted  int `json:"leads_converted"`
	BookingActive   int `json:"booking_active"`
	ContractsActive int `json:"contracts_active"` // scheme_state != cancelled
	UnitsSold       int `json:"units_sold"`       // sale_records non-cancelled (BAST)
	// S9/R-9: conversion % antar-stage dihitung BACKEND — FE hanya display.
	BookingConvPct  string `json:"booking_conv_pct"`  // booking/leads ×100
	ContractConvPct string `json:"contract_conv_pct"` // contracts/booking ×100
	SoldConvPct     string `json:"sold_conv_pct"`     // sold/contracts ×100
}

// DashboardBurn (S9/R-9): burn rate & runway dihitung backend dari seri kas
// kanonik (3 bulan aktif terakhir) — FE hanya menampilkan.
type DashboardBurn struct {
	AvgCashIn    string  `json:"avg_cash_in"`
	AvgCashOut   string  `json:"avg_cash_out"`
	MonthlyBurn  string  `json:"monthly_burn"`             // out − in; positif = kas menyusut
	RunwayMonths *string `json:"runway_months,omitempty"`  // nil = tidak menyusut / kas nol
}

// DashboardKPR (R1 → dibaca R5): agregat pipeline KPR.
type DashboardKPR struct {
	CountPreparation int    `json:"count_preparation"`
	CountSubmitted   int    `json:"count_submitted"`
	CountApproved    int    `json:"count_approved"` // SP3K
	CountAkad        int    `json:"count_akad"`
	CountDisbursed   int    `json:"count_disbursed"`
	CountSettled     int    `json:"count_settled"`
	TotalDisbursed   string `json:"total_disbursed"`
	TotalOutstanding string `json:"total_outstanding"`
	// R5 — kontrak KPR macet: >30 hari diam di satu state proses
	// (pengajuan/SP3K/akad) → masuk attention strip.
	StuckCount int `json:"stuck_count"`
}

// DashboardReceivables (R5): outstanding total + forecast kas + top penunggak.
// Rumus outstanding MENCERMINKAN ContractFinancialSummary (satu definisi):
// gross_amount − Σ termin_payments unit; hanya kontrak aktif; hanya yang > 0.
type DashboardReceivables struct {
	OutstandingTotal string `json:"outstanding_total"`
	// Forecast kas masuk: Σ (amount − paid_amount) jadwal belum lunas yang
	// jatuh tempo dalam horizon (kumulatif 30/60/90 hari dari as_of).
	Forecast30 string       `json:"forecast_30"`
	Forecast60 string       `json:"forecast_60"`
	Forecast90 string       `json:"forecast_90"`
	TopDebtors []DebtorRow  `json:"top_debtors"` // maksimum 5, urut overdue desc
}

type DebtorRow struct {
	ContractID   uint64 `json:"contract_id"`
	BuyerName    string `json:"buyer_name"`
	UnitCode     string `json:"unit_code"`
	OverdueTotal string `json:"overdue_total"`
	OverdueCount int    `json:"overdue_count"`
}

// UnitProfitRow (R5): profit per unit terjual (dari sale_records + jurnal BAST).
type UnitProfitRow struct {
	UnitID    uint64 `json:"unit_id"`
	UnitCode  string `json:"unit_code"`
	Project   string `json:"project"`
	Revenue   string `json:"revenue"` // sale_price (DPP diakui)
	HPP       string `json:"hpp"`     // total HPP diakui unit
	Margin    string `json:"margin"`
	MarginPct string `json:"margin_pct"`
}

type DashboardReport struct {
	AsOf      time.Time          `json:"as_of"`
	KPI       DashboardKPI       `json:"kpi"`
	Projects  []DashboardProject `json:"projects"`
	CashFlow  []CashFlowMonth    `json:"cash_flow"` // 6 bulan terakhir, urut naik
	Funnel    DashboardFunnel    `json:"funnel"`
	KPR       *DashboardKPR      `json:"kpr,omitempty"` // R1 additive
	// R5 additive:
	Receivables *DashboardReceivables `json:"receivables,omitempty"`
	UnitProfits []UnitProfitRow       `json:"unit_profits,omitempty"` // top 10 margin desc
	// S9 additive: burn rate & runway (dihitung backend dari seri kas kanonik).
	Burn *DashboardBurn `json:"burn,omitempty"`
}

// ── Query engine ──────────────────────────────────────────────────────────────

type dashboardQuerier struct {
	db      *gorm.DB
	finance ContractFinanceReader // S3: outstanding kanonik (sale.Service)
	repo    *GORMRepository       // S4: baris P&L kanonik (queryPLRows) utk revenue/HPP/margin
	budget  BudgetTotalsReader    // S5: total RAB aktif kanonik (budget.Service)
	house   HouseReceivableReader // W-8: piutang harga rumah — sumber yang SAMA dengan AR Aging
}

// balancesByExpr menjalankan SUM ledger posted dengan ekspresi arah tertentu.
func (q *dashboardQuerier) scanMoney(ctx context.Context, dest interface{}, sql string, args ...interface{}) error {
	return q.db.WithContext(ctx).Raw(sql, args...).Scan(dest).Error
}

func nz(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// GetDashboard merakit seluruh angka dashboard dalam satu panggilan.
func (q *dashboardQuerier) GetDashboard(ctx context.Context, tenantID uint64, now time.Time) (*DashboardReport, error) {
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	rep := &DashboardReport{AsOf: now}

	// ── Baris 1: saldo akun — via LedgerBalanceService (S2, kanonik) ──────────
	// asOf nil = SEMUA posted (posisi berjalan; perilaku existing dipertahankan,
	// nol perubahan angka). Tidak ada lagi SUM(journal_lines) manual di sini.
	balSvc := ledger.NewLedgerBalanceService(ledger.NewQueryService(q.db))
	roleBal, err := balSvc.RoleBalances(ctx, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("dashboard saldo: %w", err)
	}
	rep.KPI.Cash = roleBal[ledger.RoleCashBank].String()
	rep.KPI.Receivable = roleBal[ledger.RoleReceivable].String()
	rep.KPI.Payable = roleBal[ledger.RolePayable].String()
	rep.KPI.BookingDeposits = roleBal[ledger.RoleBookingLiability].String()
	rep.KPI.CommissionPayable = roleBal[ledger.RoleCommissionLiability].String()
	rep.KPI.RetentionPayable = roleBal[ledger.RoleRetentionPayable].String()

	// ── Baris 2: bulan berjalan ───────────────────────────────────────────────
	// S4: SalesMTD & MarginMTD DERIVED dari ComputePL (kanonik registry #9-#11,
	// definisi disempitkan item B 2026-08-27: TotalPendapatan = pendapatan INTI,
	// 4-2000 Pendapatan Luar Usaha dikeluarkan — SATU definisi dgn Laba Rugi);
	// margin = pendapatan − beban (definisi laba P&L, bukan 4-x − 5-1000 sendiri).
	mtdRows, err := q.repo.GetConsolidatedPLRowsRange(ctx, tenantID, &monthStart, nil)
	if err != nil {
		return nil, fmt.Errorf("dashboard MTD: %w", err)
	}
	mtdPL := ComputePL(mtdRows, nil, now)
	rep.KPI.SalesMTD = mtdPL.TotalPendapatan
	rep.KPI.MarginMTD = mtdPL.LabaRugiBersih

	// P3 (item A): pendapatan INTI bulan berjalan dipecah Cash vs KPR via
	// payment_type kontrak (sumber kanonik sale.PaymentType — SATU-SATUNYA
	// field yang dipakai, termasuk kontrak ber-scheme via LegacyPaymentType).
	// Baris tanpa kontrak (payment_type kosong) SENGAJA tidak dipaksa ke salah
	// satu sisi — no silent misclassification (cross-cutting requirement I).
	revSplit, err := q.repo.GetRevenueByPaymentType(ctx, tenantID, &monthStart, nil)
	if err != nil {
		return nil, fmt.Errorf("dashboard revenue split: %w", err)
	}
	rep.KPI.PendapatanCashMTD, rep.KPI.PendapatanKPRMTD = sumByPaymentType(revSplit)

	// Cash vs KPR nilai kontrak (portofolio, bukan MTD) — komposisi SELURUH
	// kontrak (GrossAmount) terlepas dari status realisasi pendapatan.
	cvSplit, err := q.repo.GetContractValueByPaymentType(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dashboard nilai kontrak split: %w", err)
	}
	rep.KPI.KontrakValueCash, rep.KPI.KontrakValueKPR = sumByPaymentType(cvSplit)

	// S6: kas masuk/keluar (MTD + seri 6 bulan) dari SATU pembaca kanonik
	// GetMonthlyCashFlow (posted, COA category cash|bank) — tidak ada dua SQL.
	seriesStart := monthStart.AddDate(0, -5, 0)
	cashMonths, err := q.repo.GetMonthlyCashFlow(ctx, tenantID, seriesStart)
	if err != nil {
		return nil, fmt.Errorf("dashboard kas bulanan: %w", err)
	}
	byMonth := map[string]CashFlowMonth{}
	for _, m := range cashMonths {
		byMonth[m.Month] = m
	}
	curMonth := byMonth[monthStart.Format("2006-01")]
	rep.KPI.CashInMTD = nz(curMonth.CashIn)
	rep.KPI.CashOutMTD = nz(curMonth.CashOut)
	rep.KPI.NetCashFlowMTD = dec(curMonth.CashIn).Sub(dec(curMonth.CashOut)).String()

	// Counter dokumen (perhatian + pipeline).
	var counts struct {
		BookingActive  int
		BookingFee     string
		UnitsSoldMTD   int
		RefundPending  int
		CxAwaiting     int
		CommQueue      int
		TrueupAwaiting int
	}
	if err := q.scanMoney(ctx, &counts, `
		SELECT
		  (SELECT COUNT(*) FROM bookings b WHERE b.tenant_id = ? AND b.status = 'active') AS booking_active,
		  (SELECT COALESCE(SUM(b.booking_fee),0) FROM bookings b WHERE b.tenant_id = ? AND b.fee_disposition = 'held') AS booking_fee,
		  (SELECT COUNT(*) FROM sale_records sr WHERE sr.tenant_id = ? AND sr.cancelled_at IS NULL AND sr.recognition_date >= ?) AS units_sold_mtd,
		  (SELECT COUNT(*) FROM refunds r WHERE r.tenant_id = ? AND r.status = 'pending') AS refund_pending,
		  (SELECT COUNT(*) FROM cancellations c WHERE c.tenant_id = ? AND c.status IN ('requested','approved')) AS cx_awaiting,
		  (SELECT COUNT(*) FROM commissions cm WHERE cm.tenant_id = ? AND cm.status IN ('calculated','approved','payable')) AS comm_queue,
		  (SELECT COUNT(*) FROM project_completion_events pce
		     WHERE pce.tenant_id = ? AND pce.status = 'finalized'
		       AND NOT EXISTS (SELECT 1 FROM hpp_trueup_runs r
		                       WHERE r.tenant_id = pce.tenant_id AND r.project_id = pce.project_id
		                         AND r.status = 'posted')) AS trueup_awaiting`,
		tenantID, tenantID, tenantID, monthStart, tenantID, tenantID, tenantID, tenantID); err != nil {
		return nil, fmt.Errorf("dashboard counters: %w", err)
	}
	rep.KPI.BookingActive = counts.BookingActive
	rep.KPI.BookingFeeHeld = nz(counts.BookingFee)
	rep.KPI.UnitsSoldMTD = counts.UnitsSoldMTD
	rep.KPI.RefundPending = counts.RefundPending
	rep.KPI.CxAwaiting = counts.CxAwaiting
	rep.KPI.CommissionQueue = counts.CommQueue
	rep.KPI.TrueupAwaiting = counts.TrueupAwaiting

	// ── T-4 (keputusan klien 2026-08-05): cicilan menunggak DERIVED ───────────
	// Dulu: COUNT(payment_schedules.status='overdue') — kolom tersimpan yang
	// hanya terisi kalau job mark-overdue dijalankan, sehingga dashboard bisa
	// menulis "0 tunggakan" padahal aging melaporkan ada. Sekarang dihitung dari
	// MESIN YANG SAMA dengan AR Aging (BuildARAging): tanggal + sisa cicilan.
	// Bukan best-effort — KPI ini tidak boleh diam-diam jatuh ke 0 saat gagal.
	//
	// W-8: sumbernya kini pembaca house AR yang sama dengan AR Aging. Sebelumnya
	// dashboard membaca `payment_schedules` langsung, sehingga "cicilan
	// menunggak" di beranda menghitung jadwal unit yang belum BAST — angka yang
	// tidak pernah bisa dicocokkan dengan laporan piutang maupun buku besar.
	if q.house == nil {
		return nil, ErrARReaderNotConfigured
	}
	arRows, err := q.house.ReceivableRows(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dashboard piutang: %w", err)
	}
	aging := BuildARAging(arRows, now)
	rep.KPI.OverdueCount = aging.Buckets.B1_30.Count + aging.Buckets.B31_60.Count +
		aging.Buckets.B61_90.Count + aging.Buckets.B90Plus.Count

	// ── Baris 3: pipeline per proyek ──────────────────────────────────────────
	// S4: revenue/HPP/margin DERIVED dari baris P&L kanonik (ComputePL).
	// S5: budget_total dari budget.Service (registry #12); actual_cost dari
	// ledger.ActualCostTotalsAllProjects (netting kanonik, kode registry).
	// Tidak ada lagi subquery SQL yang menghitung angka uang di sini.
	var rows []struct {
		ID   uint64
		Name string
		Status string
		UnitsAvailable int
		UnitsBooked    int
		UnitsReserved  int
		UnitsPpjb      int
		UnitsSold      int
		UnitsOther     int
		UnitsTotal     int
		PhysicalPct   *string
		PhysicalAsOf  *string
	}
	err = q.db.WithContext(ctx).Raw(`
		SELECT p.id, p.name, p.status,
		  COALESCE(u.avail,0) AS units_available, COALESCE(u.booked,0) AS units_booked,
		  COALESCE(u.reserved,0) AS units_reserved, COALESCE(u.ppjb,0) AS units_ppjb,
		  COALESCE(u.sold,0) AS units_sold, COALESCE(u.other,0) AS units_other,
		  COALESCE(u.total,0) AS units_total,
		  phys.progress_pct AS physical_pct,
		  DATE_FORMAT(phys.as_of_date, '%Y-%m-%d') AS physical_as_of
		FROM projects p
		LEFT JOIN (
		  SELECT un.project_id,
		    SUM(un.status = 'available') AS avail,
		    SUM(un.status = 'booked') AS booked,
		    SUM(un.status = 'reserved') AS reserved,
		    SUM(un.status = 'ppjb') AS ppjb,
		    SUM(un.status = 'sold') AS sold,
		    SUM(un.status IN ('occupied','hold','blocked','maintenance')) AS other,
		    COUNT(*) AS total
		  FROM units un WHERE un.tenant_id = ?
		  GROUP BY un.project_id
		) u ON u.project_id = p.id
		LEFT JOIN (
		  SELECT ppe.project_id, ppe.progress_pct, ppe.as_of_date
		  FROM project_progress_entries ppe
		  JOIN (
		    SELECT project_id, MAX(id) AS max_id
		    FROM project_progress_entries WHERE tenant_id = ?
		    GROUP BY project_id
		  ) latest ON latest.max_id = ppe.id
		) phys ON phys.project_id = p.id
		WHERE p.tenant_id = ?
		ORDER BY p.id`,
		tenantID, tenantID, tenantID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("dashboard pipeline: %w", err)
	}

	// R4: contract_value & collected per proyek dari baris portfolio KANONIK
	// (sale.PortfolioFinancials) — "collected" == Σ TotalPaid kanonik
	// (counts_toward_price=TRUE; booking fee di luar harga TIDAK dihitung).
	portfolio, err := q.finance.PortfolioFinancials(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dashboard portfolio: %w", err)
	}
	type projAgg struct{ value, collected domain.Money }
	aggByProject := map[uint64]*projAgg{}
	for _, pr := range portfolio {
		a := aggByProject[pr.ProjectID]
		if a == nil {
			a = &projAgg{value: domain.Zero, collected: domain.Zero}
			aggByProject[pr.ProjectID] = a
		}
		a.value = a.value.Add(pr.NetContract)
		a.collected = a.collected.Add(pr.TotalPaid)
	}
	// ── Seri arus kas 6 bulan (chart Cash In vs Cash Out) — sumber sama S6 ────
	// Bulan tanpa transaksi tetap muncul (nol) agar sumbu chart kontinu.
	for i := 0; i < 6; i++ {
		ym := seriesStart.AddDate(0, i, 0).Format("2006-01")
		if m, ok := byMonth[ym]; ok {
			rep.CashFlow = append(rep.CashFlow, m)
		} else {
			rep.CashFlow = append(rep.CashFlow, CashFlowMonth{Month: ym, CashIn: "0", CashOut: "0"})
		}
	}

	// ── Sales funnel lintas dokumen ───────────────────────────────────────────
	var fn struct {
		LeadsActive     int
		LeadsConverted  int
		ContractsActive int
		UnitsSold       int
	}
	if err := q.scanMoney(ctx, &fn, `
		SELECT
		  (SELECT COUNT(*) FROM leads l WHERE l.tenant_id = ? AND l.status IN ('new','contacted','qualified')) AS leads_active,
		  (SELECT COUNT(*) FROM leads l WHERE l.tenant_id = ? AND l.status = 'converted') AS leads_converted,
		  (SELECT COUNT(*) FROM sale_contracts sc WHERE sc.tenant_id = ?
		     AND (sc.scheme_state IS NULL OR sc.scheme_state <> 'cancelled')) AS contracts_active,
		  (SELECT COUNT(*) FROM sale_records sr WHERE sr.tenant_id = ? AND sr.cancelled_at IS NULL) AS units_sold`,
		tenantID, tenantID, tenantID, tenantID); err != nil {
		return nil, fmt.Errorf("dashboard funnel: %w", err)
	}
	rep.Funnel = DashboardFunnel{
		LeadsActive:     fn.LeadsActive,
		LeadsConverted:  fn.LeadsConverted,
		BookingActive:   counts.BookingActive,
		ContractsActive: fn.ContractsActive,
		UnitsSold:       fn.UnitsSold,
		// S9: conversion antar-stage (dibulatkan ke % bulat, pola tampilan lama).
		BookingConvPct:  countPct(counts.BookingActive, fn.LeadsActive),
		ContractConvPct: countPct(fn.ContractsActive, counts.BookingActive),
		SoldConvPct:     countPct(fn.UnitsSold, fn.ContractsActive),
	}

	// S9: burn rate & runway dari seri kas kanonik (3 bulan aktif terakhir).
	rep.Burn = computeBurn(rep.CashFlow, rep.KPI.Cash)

	// ── R1: agregat KPR (dibaca R5) — reuse querier pipeline ──────────────────
	if kprRep, kerr := (&kprPipelineQuerier{db: q.db, finance: q.finance}).GetKPRPipeline(ctx, tenantID, now); kerr == nil {
		rep.KPR = &DashboardKPR{
			CountPreparation: kprRep.CountPreparation,
			CountSubmitted:   kprRep.CountSubmitted,
			CountApproved:    kprRep.CountApproved,
			CountAkad:        kprRep.CountAkad,
			CountDisbursed:   kprRep.CountDisbursed,
			CountSettled:     kprRep.CountSettled,
			TotalDisbursed:   kprRep.TotalDisbursed,
			TotalOutstanding: kprRep.TotalOutstanding,
			StuckCount:       kprStuckCount(kprRep),
		}
	}

	// S4: revenue/HPP/margin per proyek dari baris P&L kanonik (ComputePL) —
	// SATU definisi dengan laporan Laba Rugi (semua 4-%, semua 5-%).
	plByProject, err := q.repo.GetPLRowsAllByProject(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dashboard PL per proyek: %w", err)
	}
	// S5: budget_total kanonik (budget.Service, registry #12) + actual_cost
	// kanonik (ledger, netting debit − kredit-reversal, kode dari registry —
	// kapitalisasi; relief BAST TIDAK mengurangi, keputusan PO #2).
	budgetByProject := map[uint64]domain.Money{}
	if q.budget != nil {
		if budgetByProject, err = q.budget.TotalActiveBudgetByProject(ctx, tenantID); err != nil {
			return nil, fmt.Errorf("dashboard budget total: %w", err)
		}
	}
	actualByProject, err := ledger.NewQueryService(q.db).ActualCostTotalsAllProjects(
		ctx, tenantID, ledger.RoleCodeList(ledger.RoleInventory))
	if err != nil {
		return nil, fmt.Errorf("dashboard actual cost: %w", err)
	}

	for _, r := range rows {
		pid := r.ID
		projPL := ComputePL(plByProject[pid], &pid, now)
		hpp := projPL.TotalHPP
		budgetTotal := budgetByProject[pid]
		actualCost := actualByProject[pid]
		cValue, cCollected := domain.Zero, domain.Zero
		if a := aggByProject[pid]; a != nil {
			cValue, cCollected = a.value, a.collected
		}
		dp := DashboardProject{
			ID: r.ID, Name: r.Name, Status: r.Status,
			BudgetTotal: budgetTotal.String(), ActualCost: actualCost.String(),
			ProgressPct: pctOf(actualCost.String(), budgetTotal.String()),
			UnitsAvailable: r.UnitsAvailable, UnitsBooked: r.UnitsBooked, UnitsReserved: r.UnitsReserved,
			UnitsPPJB: r.UnitsPpjb, UnitsSold: r.UnitsSold, UnitsOther: r.UnitsOther, UnitsTotal: r.UnitsTotal,
			Revenue: projPL.TotalPendapatan, HPP: hpp, Margin: projPL.LabaRugiBersih,
			MarginPct: pctOf(projPL.LabaRugiBersih, projPL.TotalPendapatan),
			ContractValue: cValue.String(), Collected: cCollected.String(),
			CollectionPct: pctOf(cCollected.String(), cValue.String()),
			PhysicalPct: r.PhysicalPct, PhysicalAsOf: r.PhysicalAsOf,
		}
		// R3 — warning biaya mendahului fisik: biaya% − fisik% > 15pt.
		if r.PhysicalPct != nil {
			if phys, perr := strconv.ParseFloat(*r.PhysicalPct, 64); perr == nil {
				if cost, cerr := strconv.ParseFloat(nz(dp.ProgressPct), 64); cerr == nil {
					dp.CostAheadWarning = cost-phys > 15
				}
			}
		}
		rep.Projects = append(rep.Projects, dp)
	}

	// ── R5: Outstanding + forecast kas + top penunggak (best-effort) ─────────
	if recv, rerr := q.receivables(ctx, tenantID, aging); rerr == nil {
		rep.Receivables = recv
	}
	// ── R5: profit per unit terjual (top 10 margin) ──────────────────────────
	if profits, perr := q.unitProfits(ctx, tenantID); perr == nil {
		rep.UnitProfits = profits
	}

	return rep, nil
}

// kprStuckCount (R5): kontrak >30 hari diam di state proses (belum final).
func kprStuckCount(rep *KPRPipelineReport) int {
	n := 0
	for _, r := range rep.Rows {
		switch r.SchemeState {
		case "submitted_to_bank", "bank_approved", "akad":
			if r.StateAgeDays > 30 {
				n++
			}
		}
	}
	return n
}

// receivables (S6): outstanding dari sumber kanonik (S3), forecast 30/60/90 +
// top 5 penunggak DERIVED dari BuildARAging (registry #19) — dashboard tidak
// punya SQL jadwal sendiri lagi. Satu engine dengan laporan Piutang Customer.
// receivables memakai laporan aging yang SUDAH dihitung pemanggil (T-4) —
// satu pembacaan cicilan, satu klasifikasi umur, dipakai KPI dan blok piutang.
func (q *dashboardQuerier) receivables(ctx context.Context, tenantID uint64, ar ARAgingReport) (*DashboardReceivables, error) {
	out := &DashboardReceivables{TopDebtors: []DebtorRow{}}

	// S3: outstanding portfolio dari sumber KANONIK (ContractFinancialSummary),
	// bukan `gross − Σtermin` SQL sendiri. Definisi tunggal, nol perubahan angka.
	outstanding, err := q.finance.PortfolioOutstanding(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("portfolio outstanding: %w", err)
	}
	out.OutstandingTotal = outstanding.String()

	out.Forecast30, out.Forecast60, out.Forecast90 = ar.Expected30, ar.Expected60, ar.Expected90

	// Top 5 penunggak: agregasi baris aging OVERDUE per kontrak (engine sama).
	type acc struct {
		row   DebtorRow
		total domain.Money
	}
	byContract := map[uint64]*acc{}
	for _, r := range ar.Rows {
		if r.DaysOverdue <= 0 {
			continue
		}
		a := byContract[r.ContractID]
		if a == nil {
			a = &acc{row: DebtorRow{ContractID: r.ContractID, BuyerName: r.BuyerName, UnitCode: r.UnitCode}, total: domain.Zero}
			byContract[r.ContractID] = a
		}
		m, merr := domain.NewMoney(r.Outstanding)
		if merr != nil {
			return nil, fmt.Errorf("parse outstanding aging: %w", merr)
		}
		a.total = a.total.Add(m)
		a.row.OverdueCount++
	}
	debtors := make([]*acc, 0, len(byContract))
	for _, a := range byContract {
		a.row.OverdueTotal = a.total.String()
		debtors = append(debtors, a)
	}
	sort.Slice(debtors, func(i, j int) bool {
		di, dj := debtors[i].total.Decimal(), debtors[j].total.Decimal()
		if !di.Equal(dj) {
			return di.GreaterThan(dj)
		}
		return debtors[i].row.ContractID < debtors[j].row.ContractID
	})
	if len(debtors) > 5 {
		debtors = debtors[:5]
	}
	for _, a := range debtors {
		out.TopDebtors = append(out.TopDebtors, a.row)
	}
	return out, nil
}

// unitProfits (S4): profit per unit terjual — SCOPE unit dari sale_records
// (dokumen BAST non-cancelled), ANGKA dari ledger posted ber-tag unit
// (pendapatan 4-%, HPP peran cogs). Menggantikan pembacaan snapshot
// `sale_records.sale_price/hpp_*` yang menyimpang pasca true-up. Top 10 margin.
func (q *dashboardQuerier) unitProfits(ctx context.Context, tenantID uint64) ([]UnitProfitRow, error) {
	// Scope + metadata unit terjual (dokumen — bukan angka keuangan).
	var units []struct {
		UnitID   uint64
		UnitCode string
		Project  string
	}
	if err := q.db.WithContext(ctx).Raw(`
		SELECT sr.unit_id, un.code AS unit_code, p.name AS project
		FROM sale_records sr
		JOIN units un ON un.id = sr.unit_id
		JOIN projects p ON p.id = sr.project_id
		WHERE sr.tenant_id = ? AND sr.cancelled_at IS NULL`, tenantID).Scan(&units).Error; err != nil {
		return nil, err
	}
	if len(units) == 0 {
		return []UnitProfitRow{}, nil
	}

	plRows, err := q.repo.GetUnitPLRows(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	byUnit := make(map[uint64]UnitPLRow, len(plRows))
	for _, r := range plRows {
		byUnit[r.UnitID] = r
	}

	out := make([]UnitProfitRow, 0, len(units))
	for _, u := range units {
		pl := byUnit[u.UnitID] // zero value bila belum ada jurnal ber-tag unit
		margin := pl.Revenue.Sub(pl.HPP)
		out = append(out, UnitProfitRow{
			UnitID: u.UnitID, UnitCode: u.UnitCode, Project: u.Project,
			Revenue: pl.Revenue.String(), HPP: pl.HPP.String(),
			Margin: margin.String(), MarginPct: pctOf(margin.String(), pl.Revenue.String()),
		})
	}
	// Top 10 margin desc (deterministik: margin, lalu unit_id).
	sort.Slice(out, func(i, j int) bool {
		mi, mj := dec(out[i].Margin), dec(out[j].Margin)
		if !mi.Equal(mj) {
			return mi.GreaterThan(mj)
		}
		return out[i].UnitID < out[j].UnitID
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out, nil
}

// pctOf: part/whole × 100 (2 desimal, "0" bila whole 0) — presentasi, bukan
// angka keuangan baru.
func pctOf(part, whole string) string {
	w := dec(whole)
	if w.IsZero() {
		return "0"
	}
	return dec(part).Div(w).Mul(decimal.NewFromInt(100)).Round(2).String()
}

// countPct: rasio dua hitungan (× 100, % bulat) — presentasi funnel.
func countPct(part, whole int) string {
	if whole <= 0 {
		return "0"
	}
	return decimal.NewFromInt(int64(part)).
		Div(decimal.NewFromInt(int64(whole))).
		Mul(decimal.NewFromInt(100)).Round(0).String()
}

// computeBurn (S9): burn rate = rata-rata (out − in) dari maksimal 3 bulan
// terakhir yang punya aktivitas kas; runway = kas sekarang ÷ burn (1 desimal).
// Rumus identik dgn kartu FE lama (charts.tsx BurnRateCard) — kini SATU tempat.
func computeBurn(series []CashFlowMonth, cashNow string) *DashboardBurn {
	start := len(series) - 3
	if start < 0 {
		start = 0
	}
	recent := series[start:]
	var n int64
	sumIn, sumOut := decimal.Zero, decimal.Zero
	for _, m := range recent {
		in, out := dec(m.CashIn), dec(m.CashOut)
		if in.IsZero() && out.IsZero() {
			continue
		}
		n++
		sumIn = sumIn.Add(in)
		sumOut = sumOut.Add(out)
	}
	burn := &DashboardBurn{AvgCashIn: "0", AvgCashOut: "0", MonthlyBurn: "0"}
	if n == 0 {
		return burn
	}
	avgIn := sumIn.Div(decimal.NewFromInt(n)).Round(2)
	avgOut := sumOut.Div(decimal.NewFromInt(n)).Round(2)
	net := avgOut.Sub(avgIn)
	burn.AvgCashIn = avgIn.String()
	burn.AvgCashOut = avgOut.String()
	burn.MonthlyBurn = net.String()
	cash := dec(cashNow)
	if net.IsPositive() && cash.IsPositive() {
		rw := cash.Div(net).Round(1).String()
		burn.RunwayMonths = &rw
	}
	return burn
}
