//go:build integration

package reporting

// ═════════════════════════════════════════════════════════════════════════════
// FINANCIAL CONSISTENCY SUITE — PRODUK NON-PROPERTI (Product Catalog hardening)
//
// Skenario yang SAMA persis dengan suite utama, tapi proyeknya juga menjual
// produk NON-PROPERTI (sambungan PDAM). Yang dibuktikan di sini:
//
//   1. HPP unit rumah TIDAK terdilusi oleh produk non-properti — walau unit
//      non-properti punya saleable_area besar (250 m², lebih besar dari kedua
//      rumah digabung), basis alokasi hanya berisi unit properti.
//   2. Produk non-properti TIDAK pernah mengakui HPP: nol baris 5-1000, nol
//      kredit persediaan, nol snapshot alokasi, hpp_method = 'none'.
//   3. Produk non-properti TETAP produk dagang penuh: bisa dikontrak, ditagih,
//      dibayar, terbit kwitansi, muncul di statement & collection, dan masuk
//      Laba Rugi lewat akun pendapatannya sendiri (4-2200 test-only, bukan 4-1000).
//   4. Seluruh pembaca tetap SETARA satu sama lain (dashboard, KPI, P&L,
//      neraca, trial balance, statement, collection, cost) — sama seperti
//      suite utama, hanya dengan produk campuran.
//   5. Progress FISIK proyek tidak bergerak karena produk non-properti
//      (progress = input manual append-only, bukan turunan daftar unit).
//
// Angka pembanding "kalau salah": bila unit PDAM ikut basis alokasi, HPP rumah
// akan menjadi 300jt × 100/450 = 66.666.666,6667 — bukan 150.000.000. Selisih
// itulah yang dijaga test ini.
// ═════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/allocation"
	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// seedTypedUnit menambah unit dengan tipe produk apa pun (katalog harus sudah
// memuat kode tipe tersebut — fail-closed).
func (e *eqEnv) seedTypedUnit(t *testing.T, code, unitType string, area, price int64) uint64 {
	t.Helper()
	if err := e.db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`,
		eqTenant, e.projectID, code, unitType, domain.FromInt(area), domain.FromInt(price), "reserved").Error; err != nil {
		t.Fatalf("seed unit %s: %v", code, err)
	}
	var id uint64
	e.db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func (e *eqEnv) schemeIDByCode(t *testing.T, code string) uint64 {
	t.Helper()
	var id uint64
	e.db.Raw(`SELECT id FROM payment_schemes WHERE tenant_id = ? AND code = ?`, eqTenant, code).Scan(&id)
	if id == 0 {
		t.Fatalf("skema %s tidak ada di seed", code)
	}
	return id
}

// sumByAccount: Σ debit/kredit posted untuk satu akun (opsional difilter unit).
func (e *eqEnv) sumByAccount(t *testing.T, code string, unitID *uint64) (domain.Money, domain.Money) {
	t.Helper()
	var row struct {
		Dr string `gorm:"column:dr"`
		Cr string `gorm:"column:cr"`
	}
	q := `SELECT COALESCE(SUM(jl.debit),0) AS dr, COALESCE(SUM(jl.credit),0) AS cr
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = ?`
	args := []interface{}{eqTenant, code}
	if unitID != nil {
		q += ` AND jl.unit_id = ?`
		args = append(args, *unitID)
	}
	e.db.Raw(q, args...).Scan(&row)
	dr, err := domain.NewMoney(row.Dr)
	if err != nil {
		t.Fatalf("debit %s bukan money: %q", code, row.Dr)
	}
	cr, err := domain.NewMoney(row.Cr)
	if err != nil {
		t.Fatalf("kredit %s bukan money: %q", code, row.Cr)
	}
	return dr, cr
}

func TestConsistency_NonPropertyProduct(t *testing.T) {
	env := eqSetup(t)
	ctx := context.Background()
	day := time.Now().Truncate(24 * time.Hour).UTC()

	// ── Katalog produk ───────────────────────────────────────────────────────
	// 'rumah' sudah ter-seed oleh eqSetup.seedUnit (property → 4-1000).
	// Akun 4-2200 dibuat KHUSUS untuk test ini (bukan bagian COA produksi) — sengaja
	// TIDAK pakai 4-2000: akun itu berperan RoleOtherIncome (Pendapatan Luar Usaha)
	// dan sejak ValidateRevenueAccount di-hardening, tidak lagi boleh dipetakan ke
	// produk katalog manapun (lihat internal/project/product_type.go).
	if err := env.db.Exec(`INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_active, created_at, updated_at)
		VALUES (?,?,?,?,?,TRUE,NOW(3),NOW(3))`,
		eqTenant, "4-2200", "Pendapatan Produk Tambahan (test)", "revenue", "credit").Error; err != nil {
		t.Fatalf("seed akun 4-2200: %v", err)
	}
	if err := env.db.Exec(`INSERT INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, eqTenant, "pdam", "Sambungan PDAM", "non_property", "4-2200").Error; err != nil {
		t.Fatalf("seed product type pdam: %v", err)
	}

	// Unit non-properti dengan area BESAR — kalau ikut basis alokasi, dilusi
	// HPP rumah akan sangat terasa (100/450 vs 100/200).
	pdamUnit := env.seedTypedUnit(t, "PDAM-01", "pdam", 250, 5_000_000)

	cashScheme := env.schemeIDByCode(t, "CASH")

	// ── Skenario ─────────────────────────────────────────────────────────────
	// 1. Biaya konstruksi aktual PROJECT-WIDE 300jt.
	env.postCost(t, 300_000_000, day)

	// 2. Rumah unit A: kontrak tunai 500jt, LUNAS (gate BAST tunai = lunas), BAST.
	cA, err := env.svc.CreateContract(ctx, eqTenant, sale.CreateContractRequest{
		UnitID: env.unitA, BuyerName: "Budi", BuyerID: "3201", ContractDate: day,
		TotalPrice:      domain.FromInt(500_000_000),
		PaymentSchemeID: &cashScheme,
		CustomerID:      &env.customerID, SalesPersonID: &env.salesID,
	})
	if err != nil {
		t.Fatalf("kontrak rumah: %v", err)
	}
	if _, err := env.svc.CreatePaymentSchedule(ctx, eqTenant, cA.ID, []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: day.AddDate(0, 0, 10), Amount: domain.FromInt(500_000_000), Type: sale.ScheduleTypeFinal},
	}); err != nil {
		t.Fatalf("jadwal rumah: %v", err)
	}
	env.pay(t, cA.ID, 500_000_000, day, nil)

	// 2b. Rumah unit B: kontrak 500jt baru dibayar 100jt, BELUM BAST — sumber
	//     outstanding nyata agar pembanding piutang/collection tidak nol-vs-nol.
	cB, err := env.svc.CreateContract(ctx, eqTenant, sale.CreateContractRequest{
		UnitID: env.unitB, BuyerName: "Budi", BuyerID: "3201", ContractDate: day,
		TotalPrice:      domain.FromInt(500_000_000),
		PaymentSchemeID: &cashScheme,
		CustomerID:      &env.customerID, SalesPersonID: &env.salesID,
	})
	if err != nil {
		t.Fatalf("kontrak rumah B: %v", err)
	}
	if _, err := env.svc.CreatePaymentSchedule(ctx, eqTenant, cB.ID, []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: day.AddDate(0, 0, 10), Amount: domain.FromInt(100_000_000), Type: sale.ScheduleTypeDP},
		{InstallmentNumber: 2, DueDate: day.AddDate(0, 0, 40), Amount: domain.FromInt(400_000_000), Type: sale.ScheduleTypeFinal},
	}); err != nil {
		t.Fatalf("jadwal rumah B: %v", err)
	}
	env.pay(t, cB.ID, 100_000_000, day, nil)

	// 3. Sambungan PDAM: kontrak tunai 5jt, LUNAS.
	cP, err := env.svc.CreateContract(ctx, eqTenant, sale.CreateContractRequest{
		UnitID: pdamUnit, BuyerName: "Budi", BuyerID: "3201", ContractDate: day,
		TotalPrice:      domain.FromInt(5_000_000),
		PaymentSchemeID: &cashScheme,
		CustomerID:      &env.customerID, SalesPersonID: &env.salesID,
	})
	if err != nil {
		t.Fatalf("kontrak pdam: %v", err)
	}
	if _, err := env.svc.CreatePaymentSchedule(ctx, eqTenant, cP.ID, []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: day.AddDate(0, 0, 10), Amount: domain.FromInt(5_000_000), Type: sale.ScheduleTypeFinal},
	}); err != nil {
		t.Fatalf("jadwal pdam: %v", err)
	}
	env.pay(t, cP.ID, 5_000_000, day, nil)

	// 4. BAST keduanya — rumah mengakui HPP, PDAM tidak.
	//    Lifecycle unit (available→reserved) bukan subjek test ini — gate-nya
	//    diuji di suite sale; di sini unit A dinaikkan langsung agar fokus
	//    tetap pada perilaku katalog produk.
	if err := env.db.Exec(`UPDATE units SET status = 'reserved' WHERE tenant_id = ? AND id = ?`,
		eqTenant, env.unitA).Error; err != nil {
		t.Fatalf("set status unit A: %v", err)
	}
	recRumah, err := env.svc.RecordAkad(ctx, eqTenant, sale.RecordBASTRequest{
		UnitID: env.unitA, SalePrice: domain.FromInt(500_000_000),
		BuyerRef: "Budi", BASTDate: day,
	})
	if err != nil {
		t.Fatalf("BAST rumah: %v", err)
	}
	recPDAM, err := env.svc.RecordAkad(ctx, eqTenant, sale.RecordBASTRequest{
		UnitID: pdamUnit, SalePrice: domain.FromInt(5_000_000),
		BuyerRef: "Budi", BASTDate: day,
	})
	if err != nil {
		t.Fatalf("BAST pdam: %v", err)
	}

	// 5. Progress fisik proyek 40% (input manual, append-only).
	if err := env.db.Exec(`INSERT INTO project_progress_entries
		(tenant_id, project_id, progress_pct, as_of_date, notes, created_at, updated_at)
		VALUES (?,?,?,?,?,NOW(3),NOW(3))`,
		eqTenant, env.projectID, "40.00", day, "consistency").Error; err != nil {
		t.Fatalf("seed progress fisik: %v", err)
	}

	// ── Snapshot pembacaan ───────────────────────────────────────────────────
	asOf := day.AddDate(0, 0, 1)
	dash, err := env.dash.GetDashboard(ctx, eqTenant, time.Now())
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	tb, err := env.ledgerQ.TrialBalance(ctx, eqTenant, asOf)
	if err != nil {
		t.Fatalf("trial balance: %v", err)
	}
	pl, err := env.repSvc.GetProjectPL(ctx, eqTenant, env.projectID, asOf)
	if err != nil {
		t.Fatalf("project PL: %v", err)
	}
	neraca, err := env.repSvc.GetNeraca(ctx, eqTenant, asOf)
	if err != nil {
		t.Fatalf("neraca: %v", err)
	}
	var dp *DashboardProject
	for i := range dash.Projects {
		if dash.Projects[i].ID == env.projectID {
			dp = &dash.Projects[i]
		}
	}
	if dp == nil {
		t.Fatal("proyek tidak ada di dashboard")
	}

	// ═════ NP#1 HPP TIDAK TERDILUSI ═════
	t.Run("EQ_NP_HPP_Undiluted", func(t *testing.T) {
		// Basis alokasi = HANYA unit properti (unitA + unitB, @100 m²).
		inputs, err := allocation.NewGORMRepository(env.db).GetUnitInputs(ctx, eqTenant, env.projectID)
		if err != nil {
			t.Fatalf("GetUnitInputs: %v", err)
		}
		if len(inputs) != 2 {
			t.Fatalf("basis alokasi = %d unit, want 2 (unit non-properti harus di luar basis)", len(inputs))
		}
		for _, in := range inputs {
			if in.UnitID == pdamUnit {
				t.Fatal("unit non-properti masuk basis alokasi HPP")
			}
		}
		// 300jt × 100/200 = 150jt PERSIS. Kalau PDAM ikut: 66.666.666,6667.
		moneyEq(t, "HPP rumah: 300jt × (100/200) tanpa dilusi",
			recRumah.HPPTotal().String(), domain.FromInt(150_000_000).String())
		dr5, _ := env.sumByAccount(t, "5-1000", nil)
		moneyEq(t, "HPP: ledger 5-1000 vs sale_record rumah", dr5.String(), recRumah.HPPTotal().String())
		moneyEq(t, "HPP: dashboard proyek vs ledger 5-1000", dp.HPP, dr5.String())
	})

	// ═════ NP#2 NON-PROPERTI: PENDAPATAN TANPA HPP ═════
	t.Run("EQ_NP_NonProperty_NoCOGS", func(t *testing.T) {
		if recPDAM.HPPMethod != string(sale.HPPMethodNone) {
			t.Errorf("hpp_method PDAM = %q, want none", recPDAM.HPPMethod)
		}
		if !recPDAM.HPPTotal().IsZero() {
			t.Errorf("HPP PDAM = %s, want nol", recPDAM.HPPTotal())
		}
		if dr, _ := env.sumByAccount(t, "5-1000", &pdamUnit); !dr.IsZero() {
			t.Errorf("debit HPP unit non-properti = %s, want nol", dr)
		}
		for _, inv := range []string{"1-3000", "1-3100", "1-3200", "1-3300"} {
			if _, cr := env.sumByAccount(t, inv, &pdamUnit); !cr.IsZero() {
				t.Errorf("kredit persediaan %s unit non-properti = %s, want nol", inv, cr)
			}
		}
		var snaps int64
		env.db.Raw(`SELECT COUNT(*) FROM allocation_snapshots WHERE tenant_id = ? AND unit_id = ?`,
			eqTenant, pdamUnit).Scan(&snaps)
		if snaps != 0 {
			t.Errorf("snapshot alokasi untuk unit non-properti = %d baris, want 0", snaps)
		}
	})

	// ═════ NP#3 ROUTING PENDAPATAN PER KATALOG ═════
	t.Run("EQ_NP_RevenueRouting_PerCatalog", func(t *testing.T) {
		_, crRumah := env.sumByAccount(t, "4-1000", &env.unitA)
		moneyEq(t, "pendapatan rumah → 4-1000", crRumah.String(), domain.FromInt(500_000_000).String())
		_, crPdamOnHouse := env.sumByAccount(t, "4-1000", &pdamUnit)
		moneyEq(t, "produk non-properti TIDAK menyentuh pendapatan properti", crPdamOnHouse.String(), "0")
		_, crPdam := env.sumByAccount(t, "4-2200", &pdamUnit)
		moneyEq(t, "pendapatan sambungan PDAM → 4-2200 (akun katalog)", crPdam.String(), domain.FromInt(5_000_000).String())
	})

	// ═════ NP#4 LABA RUGI & DASHBOARD ═════
	t.Run("EQ_NP_Revenue_Dashboard_vs_PL", func(t *testing.T) {
		moneyEq(t, "revenue: dashboard proyek vs P&L", dp.Revenue, pl.TotalPendapatan)
		// P&L memuat KEDUA jenis pendapatan (properti + non-properti).
		moneyEq(t, "revenue: total 500jt + 5jt", pl.TotalPendapatan, domain.FromInt(505_000_000).String())
	})
	t.Run("EQ_NP_Margin_Dashboard_vs_PL", func(t *testing.T) {
		moneyEq(t, "margin: dashboard proyek vs P&L laba", dp.Margin, pl.LabaRugiBersih)
		// 505jt pendapatan − 150jt HPP = 355jt (produk non-properti menambah
		// laba penuh karena memang tidak punya HPP di modul ini).
		moneyEq(t, "margin: 505jt − 150jt", pl.LabaRugiBersih, domain.FromInt(355_000_000).String())
	})
	t.Run("EQ_NP_RevenueMTD_KPI_vs_PL", func(t *testing.T) {
		moneyEq(t, "sales MTD: dashboard KPI vs P&L pendapatan", dash.KPI.SalesMTD, pl.TotalPendapatan)
	})

	// ═════ NP#5 NERACA & TRIAL BALANCE ═════
	t.Run("EQ_NP_Neraca_Balanced", func(t *testing.T) {
		moneyEq(t, "neraca: total aset vs kewajiban+ekuitas", neraca.TotalAset, neraca.TotalKewajibanEkuitas)
		if !neraca.IsBalanced {
			t.Error("neraca tidak balanced dengan produk campuran")
		}
		var diff string
		env.db.Raw(`SELECT COALESCE(SUM(jl.debit) - SUM(jl.credit),0) FROM journal_lines jl
			JOIN journal_entries je ON je.id = jl.journal_entry_id
			WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL`, eqTenant).Scan(&diff)
		moneyEq(t, "Invariant #1: Σ debit − Σ kredit seluruh jurnal posted", diff, "0")
	})
	t.Run("EQ_NP_Cash_Dashboard_vs_TrialBalance", func(t *testing.T) {
		want := env.tbBalance(t, tb, func(_, cat string) bool { return cat == "cash" || cat == "bank" })
		moneyEq(t, "cash: dashboard KPI vs Σ trial-balance(cash|bank)", dash.KPI.Cash, want)
		// 500jt (rumah A) + 100jt (rumah B) + 5jt (PDAM) — non-properti tetap
		// menghasilkan kas nyata.
		moneyEq(t, "cash: 500jt + 100jt + 5jt", want, domain.FromInt(605_000_000).String())
	})
	t.Run("EQ_NP_AR_Dashboard_vs_TrialBalance", func(t *testing.T) {
		want := env.tbBalance(t, tb, func(code, _ string) bool {
			return code == "1-2000" || code == "1-2100" || code == "1-2200"
		})
		moneyEq(t, "receivable: dashboard KPI vs trial-balance 1-2xxx", dash.KPI.Receivable, want)
	})

	// ═════ NP#6 COLLECTION & STATEMENT UNTUK PRODUK NON-PROPERTI ═════
	t.Run("EQ_NP_Statement_vs_Summary_NonProperty", func(t *testing.T) {
		sumP, err := env.svc.ContractFinancialSummaryByID(ctx, eqTenant, cP.ID)
		if err != nil {
			t.Fatalf("summary pdam: %v", err)
		}
		moneyEq(t, "pdam: total_paid summary", sumP.TotalPaid.String(), domain.FromInt(5_000_000).String())
		moneyEq(t, "pdam: outstanding summary (lunas)", sumP.Outstanding.String(), "0")

		st, err := env.svc.GetCustomerStatement(ctx, eqTenant, cP.ID, asOf)
		if err != nil {
			t.Fatalf("statement pdam: %v", err)
		}
		moneyEq(t, "pdam: outstanding statement vs summary", st.RemainingBalance, sumP.Outstanding.String())
		moneyEq(t, "pdam: total_paid statement vs summary", st.TotalPaid, sumP.TotalPaid.String())

		// Kwitansi terbit untuk produk non-properti (bukti terima sah).
		var receipts string
		env.db.Raw(`SELECT COALESCE(SUM(r.amount),0) FROM receipts r
			JOIN termin_payments tp ON tp.id = r.termin_payment_id AND tp.tenant_id = r.tenant_id
			WHERE r.tenant_id = ? AND tp.unit_id = ?`, eqTenant, pdamUnit).Scan(&receipts)
		moneyEq(t, "pdam: Σ kwitansi vs total_paid", receipts, sumP.TotalPaid.String())
	})
	t.Run("EQ_NP_Outstanding_Dashboard_vs_SummarySum", func(t *testing.T) {
		if dash.Receivables == nil {
			t.Fatal("dashboard.receivables kosong")
		}
		sumA, err := env.svc.ContractFinancialSummaryByID(ctx, eqTenant, cA.ID)
		if err != nil {
			t.Fatalf("summary rumah A: %v", err)
		}
		sumB, err := env.svc.ContractFinancialSummaryByID(ctx, eqTenant, cB.ID)
		if err != nil {
			t.Fatalf("summary rumah B: %v", err)
		}
		sumP, err := env.svc.ContractFinancialSummaryByID(ctx, eqTenant, cP.ID)
		if err != nil {
			t.Fatalf("summary pdam: %v", err)
		}
		total := sumA.Outstanding.Add(sumB.Outstanding).Add(sumP.Outstanding)
		moneyEq(t, "outstanding: dashboard vs Σ summary (rumah + non-properti)",
			dash.Receivables.OutstandingTotal, total.String())
		moneyEq(t, "outstanding: 400jt dari rumah B saja", total.String(), domain.FromInt(400_000_000).String())
		moneyEq(t, "collected proyek vs Σ TotalPaid kanonik",
			dp.Collected, sumA.TotalPaid.Add(sumB.TotalPaid).Add(sumP.TotalPaid).String())
	})

	// ═════ NP#7 BIAYA AKTUAL TIDAK BERUBAH ═════
	t.Run("EQ_NP_ActualCost_Dashboard_vs_CostRepo", func(t *testing.T) {
		bd, err := cost.NewGORMRepository(env.db).AccumulatedByProject(ctx, eqTenant, env.projectID)
		if err != nil {
			t.Fatalf("AccumulatedByProject: %v", err)
		}
		moneyEq(t, "actual cost: dashboard vs cost repo kanonik", dp.ActualCost, bd.Total().String())
		moneyEq(t, "actual cost: 300jt (produk non-properti tidak menambah/mengurangi)",
			dp.ActualCost, domain.FromInt(300_000_000).String())
	})

	// ═════ NP#8 PROGRESS FISIK TIDAK DIPENGARUHI KATALOG ═════
	t.Run("EQ_NP_PhysicalProgress_UnaffectedByNonProperty", func(t *testing.T) {
		// Progress fisik = entri manual append-only (project_progress_entries),
		// BUKAN turunan daftar unit — menambah produk non-properti tidak
		// menggeser angka prestasi proyek.
		if dp.PhysicalPct == nil {
			t.Fatal("physical_pct kosong padahal entri progress ada")
		}
		if *dp.PhysicalPct != "40.00" {
			t.Errorf("physical_pct = %q, want 40.00", *dp.PhysicalPct)
		}
	})
}
