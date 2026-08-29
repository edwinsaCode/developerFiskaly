//go:build integration

package charge_test

// W-4 — SATU EKSPOSUR PIUTANG CUSTOMER.
//
// Sebelum W-4 sistem punya dua daftar piutang yang tidak pernah bertemu:
// cicilan harga rumah di /reports/ar-aging dan tagihan biaya realisasi di
// /charges/aging. Tidak ada satu angka pun yang menjawab "customer ini masih
// berutang berapa" — dan pertanyaan itulah yang ditanyakan penagih setiap hari.
//
// Suite ini menegakkan dua invarian W-4 terhadap DB nyata:
//
//	INV-AR-2  Σ(laporan gabungan) == Σ(house) + Σ(realization), persis.
//	INV-AR-3  Exposure di customer statement == baris aging kontrak itu.
//
// Keduanya diuji lewat SERVICE produksi dengan wiring seperti cmd/api/main.go —
// bukan lewat pemanggilan langsung mesin bucketing, karena yang bisa rusak
// justru kabelnya (reader tidak terpasang, sumber tersaring dua kali).

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/allocation"
	"esaproperti/internal/charge"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/receivable"
	"esaproperti/internal/reporting"
	"esaproperti/internal/sale"
)

// w4AsOf sengaja jatuh SETELAH jatuh tempo tagihan realisasi dan cicilan
// pertama, sehingga kolom "menunggak" benar-benar teruji — asOf yang terlalu
// dini membuat seluruh aging berisi nol dan lulus tanpa membuktikan apa pun.
var w4AsOf = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

type w4Env struct {
	*cgEnv
	rep  *reporting.Service
	sale *sale.Service
}

// w4Setup memasang wiring produksi: reporting membaca tagihan realisasi lewat
// charge.Service, dan sale.Service membaca eksposur realisasi dari service yang
// SAMA. Dua pembaca berbeda atas satu sumber — itu justru yang diuji.
func w4Setup(t *testing.T) *w4Env {
	t.Helper()
	env := cgSetup(t)

	ledgerRepo := ledger.NewGORMRepository(env.db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).
		WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(env.db)
	saleRepo := sale.NewGORMRepository(env.db, posting,
		allocation.NewService(allocRepo, allocRepo, allocRepo))
	saleSvc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo),
		sale.WithHouseARStore(saleRepo))
	saleSvc.SetRealizationExposure(env.svc)

	repRepo := reporting.NewGORMRepository(env.db)
	ledgerQ := ledger.NewQueryService(env.db)
	repSvc := reporting.NewService(ledgerQ, repRepo, repRepo, repRepo).
		// W-8: piutang harga rumah dibaca dari pemilik sub-ledgernya (sale),
		// bukan dari tabel jadwal. Wiring ini sama dengan cmd/api/main.go.
		WithHouseReader(saleSvc).
		WithRealizationReader(env.svc)

	return &w4Env{cgEnv: env, rep: repSvc, sale: saleSvc}
}

// seedBAST menulis SaleRecord — peristiwa yang MELAHIRKAN piutang harga rumah
// (BD-1). Sejak W-8 tidak ada jalan lain: jadwal saja bukan piutang.
//
// Di-seed langsung ke tabelnya karena yang diuji di sini adalah READ MODEL
// piutang, bukan cara BAST terbentuk (itu dijaga suite sale). Yang penting
// bentuk barisnya sama dengan produksi.
func (e *w4Env) seedBAST(t *testing.T, price int64, bastDate time.Time) {
	t.Helper()
	if err := e.db.Exec(`INSERT INTO sale_records
		(tenant_id, unit_id, project_id, sale_price, is_vat, vat_rate, buyer_ref, recognition_date, revenue_journal_id)
		VALUES (?,?,?,?,0,0,'Budi',?,0)`,
		cgTenant, e.unitID, e.projectID, domain.FromInt(price), bastDate).Error; err != nil {
		t.Fatalf("seed BAST: %v", err)
	}
}

// seedHousePayment menulis penerimaan yang MENGURANGI harga rumah
// (counts_toward_price = TRUE) — sisi kedua dari rumus outstanding W-8.
func (e *w4Env) seedHousePayment(t *testing.T, amount int64, date time.Time) {
	t.Helper()
	if err := e.db.Exec(`INSERT INTO termin_payments
		(tenant_id, unit_id, project_id, amount, bank_account_code, date, description,
		 journal_entry_id, counts_toward_price)
		VALUES (?,?,?,?,'1-1100',?,'termin harga rumah',0,TRUE)`,
		cgTenant, e.unitID, e.projectID, domain.FromInt(amount), date).Error; err != nil {
		t.Fatalf("seed termin harga rumah: %v", err)
	}
}

// seedSchedule menulis satu cicilan harga rumah. Sejak W-8 jadwal hanya memasok
// STRUKTUR JATUH TEMPO; berapa piutangnya ditentukan BAST dikurangi penerimaan.
func (e *w4Env) seedSchedule(t *testing.T, no int, due time.Time, amount, paid int64, status string) {
	t.Helper()
	if err := e.db.Exec(`INSERT INTO payment_schedules
		(tenant_id, sale_contract_id, unit_id, installment_number, due_date, amount, paid_amount, type, status)
		VALUES (?,?,?,?,?,?,?, 'installment', ?)`,
		cgTenant, e.contractID, e.unitID, no, due,
		domain.FromInt(amount), domain.FromInt(paid), status).Error; err != nil {
		t.Fatalf("seed cicilan #%d: %v", no, err)
	}
}

// seedRealization membuat charge group biaya realisasi, MENERBITKAN invoice-nya,
// lalu membayar sebagiannya lewat jalur produksi (ReceivePayment → port
// sale.RecordChargeReceiptInTx).
//
// Invoice bukan hiasan di sini: sejak W-5 (R-5) piutang lahir saat invoice
// terbit, bukan saat tagihan dibuat. Tagihan tanpa invoice adalah kewajiban
// titipan yang belum ditagihkan — benar bila ia tidak muncul di aging. Jadi
// fixture ini menerbitkan invoice lebih dulu supaya yang diuji tetap "satu
// eksposur piutang", bukan kebetulan bahwa charge apa pun dianggap piutang.
func (e *w4Env) seedRealization(t *testing.T, due time.Time) uint64 {
	t.Helper()
	ctx := context.Background()
	sum, err := e.svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: e.contractID,
		Kind:           charge.KindRealization,
		Label:          "Biaya Realisasi",
		Items: []charge.NewItemInput{
			{Label: "Notaris", ChargeTypeCode: "notaris", Amount: domain.FromInt(10_000_000), DueDate: &due},
			{Label: "PDAM", ChargeTypeCode: "pdam", Amount: domain.FromInt(5_000_000), DueDate: &due},
		},
	})
	if err != nil {
		t.Fatalf("create charge group: %v", err)
	}
	itemID := map[string]uint64{}
	for _, it := range sum.Items {
		itemID[it.Label] = it.ItemID
	}
	if _, _, _, err := e.svc.IssueInvoice(ctx, cgTenant, sum.GroupID, 0, due,
		"tagihan biaya realisasi"); err != nil {
		t.Fatalf("terbitkan invoice realisasi: %v", err)
	}
	// Bayar 4jt, dialokasikan ke PDAM — menyisakan Notaris 10jt + PDAM 1jt.
	if _, err := e.svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(4_000_000),
		Date: due, BankAccountCode: "1-1100",
		Allocations: []charge.AllocationInput{
			{ChargeItemID: itemID["PDAM"], Amount: domain.FromInt(4_000_000)},
		},
	}); err != nil {
		t.Fatalf("terima pembayaran realisasi: %v", err)
	}
	return sum.GroupID
}

// sumOutstanding menjumlahkan sisa tagihan dari baris aging yang sudah jadi.
func sumOutstanding(rows []receivable.AgingRow) domain.Money {
	var total domain.Money
	for _, r := range rows {
		m, err := domain.NewMoney(r.Outstanding)
		if err != nil {
			continue
		}
		total = total.Add(m)
	}
	return total
}

func moneyStrEq(t *testing.T, label, got, want string) {
	t.Helper()
	g, err := domain.NewMoney(got)
	if err != nil {
		t.Fatalf("%s: parse got %q: %v", label, got, err)
	}
	w, err := domain.NewMoney(want)
	if err != nil {
		t.Fatalf("%s: parse want %q: %v", label, want, err)
	}
	if !g.Sub(w).IsZero() {
		t.Errorf("%s: dapat %s, harap %s", label, g, w)
	}
}

// TestIntegration_W4_SatuEksposurPiutang menegakkan INV-AR-2 dan INV-AR-3.
func TestIntegration_W4_SatuEksposurPiutang(t *testing.T) {
	env := w4Setup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()

	// ── Piutang harga rumah: unit SUDAH BAST (BD-1) seharga 180jt, 70jt sudah
	//    diterima → sisa 110jt. Jadwalnya memberi struktur jatuh tempo: satu
	//    cicilan menunggak (sisa 60jt), satu belum jatuh tempo (50jt), satu sudah
	//    lunas (tidak menampung sisa apa pun).
	env.seedBAST(t, 180_000_000, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	env.seedHousePayment(t, 70_000_000, time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC))
	env.seedSchedule(t, 1, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), 100_000_000, 40_000_000, "scheduled")
	env.seedSchedule(t, 2, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 50_000_000, 0, "scheduled")
	env.seedSchedule(t, 3, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 30_000_000, 30_000_000, "received")

	// ── Piutang biaya realisasi: 15jt ditagih, 4jt dibayar → sisa 11jt, menunggak.
	env.seedRealization(t, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))

	all, err := env.rep.GetARAging(ctx, cgTenant, w4AsOf, "")
	if err != nil {
		t.Fatalf("aging gabungan: %v", err)
	}
	house, err := env.rep.GetARAging(ctx, cgTenant, w4AsOf, receivable.SourceHouse)
	if err != nil {
		t.Fatalf("aging house: %v", err)
	}
	real, err := env.rep.GetARAging(ctx, cgTenant, w4AsOf, receivable.SourceRealization)
	if err != nil {
		t.Fatalf("aging realization: %v", err)
	}

	// Angka absolut — kalau salah satu sisi diam-diam hilang, kesetaraan di
	// bawah tetap lulus; jadi nilainya dipatok dulu.
	moneyStrEq(t, "piutang harga rumah", house.TotalPiutang, "110000000") // 60jt + 50jt
	moneyStrEq(t, "piutang biaya realisasi", real.TotalPiutang, "11000000")
	moneyStrEq(t, "menunggak harga rumah", house.Overdue, "60000000")
	moneyStrEq(t, "menunggak biaya realisasi", real.Overdue, "11000000")

	// ── INV-AR-2 ──
	moneyStrEq(t, "INV-AR-2 total",
		all.TotalPiutang,
		mustAdd(t, house.TotalPiutang, real.TotalPiutang))
	moneyStrEq(t, "INV-AR-2 menunggak",
		all.Overdue,
		mustAdd(t, house.Overdue, real.Overdue))
	if got, want := len(all.Rows), len(house.Rows)+len(real.Rows); got != want {
		t.Errorf("INV-AR-2 jumlah baris: gabungan %d, house+realization %d", got, want)
	}

	// by_source harus konsisten dengan laporan tersaringnya — dua jalan menuju
	// angka yang sama, dan keduanya dipakai layar.
	if len(all.BySource) != 2 {
		t.Fatalf("by_source = %d entri, harap 2: %+v", len(all.BySource), all.BySource)
	}
	bySrc := map[receivable.Source]receivable.SourceSummary{}
	for _, s := range all.BySource {
		bySrc[s.Source] = s
	}
	moneyStrEq(t, "by_source house", bySrc[receivable.SourceHouse].Outstanding, house.TotalPiutang)
	moneyStrEq(t, "by_source realization", bySrc[receivable.SourceRealization].Outstanding, real.TotalPiutang)

	// Baris realisasi harus benar-benar hadir di laporan gabungan, membawa
	// keterangannya sendiri di Label — bukan dititipkan ke kolom kode unit.
	var sawRealization bool
	for _, r := range all.Rows {
		if r.Source != receivable.SourceRealization {
			continue
		}
		sawRealization = true
		if r.UnitCode != "CG-A1" {
			t.Errorf("kode unit baris realisasi tercemar keterangan: %q", r.UnitCode)
		}
		if r.Label == "" {
			t.Errorf("baris realisasi tanpa keterangan: %+v", r)
		}
	}
	if !sawRealization {
		t.Error("tidak ada baris realisasi di laporan gabungan — TD-2 tidak berjalan")
	}

	// ── INV-AR-3: statement satu kontrak == baris aging kontrak itu ──
	stmt, err := env.sale.GetCustomerStatement(ctx, cgTenant, env.contractID, w4AsOf)
	if err != nil {
		t.Fatalf("customer statement: %v", err)
	}
	if stmt.Exposure == nil {
		t.Fatal("statement tanpa blok exposure")
	}
	if !stmt.Exposure.RealizationAvailable {
		t.Fatal("realization_available=false padahal pembacanya terpasang — layar akan menampilkan Rp0 untuk data yang sebenarnya ada")
	}

	var houseRows, realRows []receivable.AgingRow
	for _, r := range all.Rows {
		if r.ContractID != env.contractID {
			continue
		}
		if r.Source == receivable.SourceRealization {
			realRows = append(realRows, r)
		} else {
			houseRows = append(houseRows, r)
		}
	}
	moneyStrEq(t, "INV-AR-3 harga rumah",
		stmt.Exposure.HouseOutstanding, sumOutstanding(houseRows).String())
	moneyStrEq(t, "INV-AR-3 biaya realisasi",
		stmt.Exposure.RealizationOutstanding, sumOutstanding(realRows).String())
	moneyStrEq(t, "INV-AR-3 total",
		stmt.Exposure.TotalOutstanding,
		mustAdd(t, stmt.Exposure.HouseOutstanding, stmt.Exposure.RealizationOutstanding))

	// ── INV-OWN-1 (efek TD-1): penerimaan realisasi ditulis lewat port sale,
	//    jadi bentuk barisnya wajib benar — di luar harga rumah, sumber jelas.
	var tp struct {
		Cnt    int
		Counts int
	}
	if err := env.db.Raw(`SELECT COUNT(*) AS cnt, COALESCE(SUM(counts_toward_price),0) AS counts
		FROM termin_payments WHERE tenant_id = ? AND payment_source = 'realization'`, cgTenant).
		Scan(&tp).Error; err != nil {
		t.Fatalf("baca termin_payments: %v", err)
	}
	if tp.Cnt != 1 {
		t.Errorf("penerimaan realisasi = %d baris, harap 1", tp.Cnt)
	}
	if tp.Counts != 0 {
		t.Errorf("penerimaan realisasi ikut menghitung harga rumah (counts_toward_price=1) — K-1 dilanggar")
	}
	// Yang dibayar ke harga rumah tetap 70jt: pembayaran biaya realisasi 4jt
	// TIDAK boleh ikut menguranginya (K-1). Angkanya dipatok, bukan sekadar
	// "tidak berubah", supaya seed yang hilang ketahuan.
	mustEq(t, "penerimaan harga rumah tak tersentuh titipan", env.housePaid(t), 70_000_000)
}

// TestIntegration_W4_TanpaPembacaRealisasi menjaga tenant yang belum memakai
// Charge Group: laporan harga rumah tetap benar, dan statement JUJUR bahwa
// angka realisasi tidak diketahui — bukan diam-diam menampilkan Rp0.
func TestIntegration_W4_TanpaPembacaRealisasi(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()

	w4 := &w4Env{cgEnv: env}
	w4.seedBAST(t, 70_000_000, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err := env.db.Exec(`INSERT INTO payment_schedules
		(tenant_id, sale_contract_id, unit_id, installment_number, due_date, amount, paid_amount, type, status)
		VALUES (?,?,?,1,?,?,'0','installment','scheduled')`,
		cgTenant, env.contractID, env.unitID,
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), domain.FromInt(70_000_000)).Error; err != nil {
		t.Fatalf("seed cicilan: %v", err)
	}

	ledgerRepo := ledger.NewGORMRepository(env.db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).
		WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(env.db)
	saleRepo := sale.NewGORMRepository(env.db, posting,
		allocation.NewService(allocRepo, allocRepo, allocRepo))
	saleSvc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo),
		sale.WithHouseARStore(saleRepo)) // sengaja TANPA SetRealizationExposure

	repRepo := reporting.NewGORMRepository(env.db)
	repSvc := reporting.NewService(ledger.NewQueryService(env.db), repRepo, repRepo, repRepo).
		WithHouseReader(saleSvc) // sengaja TANPA WithRealizationReader

	rpt, err := repSvc.GetARAging(ctx, cgTenant, w4AsOf, "")
	if err != nil {
		t.Fatalf("aging tanpa pembaca realisasi: %v", err)
	}
	moneyStrEq(t, "piutang harga rumah", rpt.TotalPiutang, "70000000")

	stmt, err := saleSvc.GetCustomerStatement(ctx, cgTenant, env.contractID, w4AsOf)
	if err != nil {
		t.Fatalf("customer statement: %v", err)
	}
	if stmt.Exposure == nil {
		t.Fatal("statement tanpa blok exposure")
	}
	if stmt.Exposure.RealizationAvailable {
		t.Error("realization_available=true padahal pembacanya tidak terpasang")
	}
	moneyStrEq(t, "total eksposur = harga rumah saja", stmt.Exposure.TotalOutstanding, "70000000")
}

// mustAdd menjumlahkan dua nominal string lewat domain.Money (bukan aritmetika
// string) dan mengembalikan hasilnya sebagai string.
func mustAdd(t *testing.T, a, b string) string {
	t.Helper()
	x, err := domain.NewMoney(a)
	if err != nil {
		t.Fatalf("parse %q: %v", a, err)
	}
	y, err := domain.NewMoney(b)
	if err != nil {
		t.Fatalf("parse %q: %v", b, err)
	}
	return x.Add(y).String()
}
