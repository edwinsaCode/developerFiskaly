//go:build integration

package sale_test

// P1 — "Fix Kelebihan Tanah Outstanding & Payment Flow dari Unit Detail"
// (2026-09-04): pembeli bisa membayar cicilan Kelebihan Tanah LANGSUNG dari
// Unit Detail (POST /collections/payment dgn schedule_id) tanpa waterfall
// seluruh kontrak — tidak boleh menyentuh cicilan Rumah, harus reconcile
// persis dengan rekening koran (statement/exposure — mesin AR yang sama
// dipakai halaman Piutang), dan tidak boleh membuat engine pembayaran/AR/
// alokasi/kwitansi baru (satu pintu ReceivePayment yang sama dgn kontrak).
//
// Skenario ini menutup: A) belum bayar, B) bayar sebagian (SEBAGIAN), C)
// pelunasan sisa (LUNAS), D) angka Rumah dan Kelebihan Tanah tidak boleh
// bercampur, E) jalur kontrak-level (tanpa schedule_id) tetap waterfall
// seperti sebelumnya (dibuktikan terpisah oleh
// TestIntegration_Land_ReceivePayment_CoversUnitAndLandAR di
// land_integration_test.go, tidak diulang di sini).
//
// Prasyarat: TEST_DB_DSN + DB termigrasi (>= 000090).

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/sale"
)

func TestIntegration_Land_ScheduleTargetedPayment_IndependentOfHouse(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "reserved")
	// Skenario klien persis: Rumah Rp185jt + Kelebihan Tanah Rp2jt (4 m² x Rp500rb).
	pool := lbMustPool(t, db, projectID, "1000", "500000")
	_ = pool

	hppStub := &lbStubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: domain.FromInt(200_000),
		Method:    land.HPPMethodActual,
	}}
	svc := lbWire(db, hppStub, lbZeroUnitHPPResolver{})

	const housePrice = 185_000_000
	const landQtyM2 = "4"
	const landGross = 2_000_000

	qty := decimal.RequireFromString(landQtyM2)
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LB Schedule",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(housePrice),
		CustomerID:     &cust,
		LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	rec, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(housePrice), BuyerRef: "Buyer LB Schedule",
		BASTDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if rec.RevenueJournalID == 0 {
		t.Fatal("rumah: RevenueJournalID harus ada")
	}

	type schedRow struct {
		ID         uint64
		Type       string
		Amount     string
		PaidAmount string
		Status     string
	}
	loadSchedules := func() []schedRow {
		var rows []schedRow
		if err := db.Table("payment_schedules").
			Select("id, type, amount, paid_amount, status").
			Where("tenant_id = ? AND sale_contract_id = ?", lbTenant, contract.ID).
			Order("installment_number ASC").
			Scan(&rows).Error; err != nil {
			t.Fatalf("baca payment_schedules: %v", err)
		}
		return rows
	}

	schedules := loadSchedules()
	if len(schedules) != 2 {
		t.Fatalf("jumlah baris payment_schedules pasca-Akad = %d, want 2 (rumah+tanah), got %+v", len(schedules), schedules)
	}
	var landScheduleID, houseScheduleID uint64
	for _, r := range schedules {
		if r.Type == "land" {
			landScheduleID = r.ID
		} else {
			houseScheduleID = r.ID
		}
	}
	if landScheduleID == 0 || houseScheduleID == 0 {
		t.Fatalf("gagal identifikasi baris jadwal rumah/tanah: %+v", schedules)
	}

	journalBalanced := func(journalID uint64) {
		t.Helper()
		type row struct {
			Debit  domain.Money `gorm:"column:d"`
			Credit domain.Money `gorm:"column:c"`
		}
		var r row
		if err := db.Table("journal_lines").
			Select("COALESCE(SUM(debit),0) AS d, COALESCE(SUM(credit),0) AS c").
			Where("tenant_id = ? AND journal_entry_id = ?", lbTenant, journalID).Scan(&r).Error; err != nil {
			t.Fatalf("sum journal %d: %v", journalID, err)
		}
		if r.Debit.String() != r.Credit.String() {
			t.Errorf("jurnal %d tidak balanced: debit=%s credit=%s", journalID, r.Debit, r.Credit)
		}
	}

	// ── Skenario A: belum bayar sama sekali ─────────────────────────────────
	stmt, err := svc.GetCustomerStatement(ctx, lbTenant, contract.ID, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetCustomerStatement (A): %v", err)
	}
	if stmt.Exposure == nil {
		t.Fatal("Exposure harus terisi")
	}
	if stmt.Exposure.HouseOutstanding != "185000000" {
		t.Errorf("A: HouseOutstanding = %s, want 185000000", stmt.Exposure.HouseOutstanding)
	}
	if stmt.Exposure.LandOutstanding != "2000000" {
		t.Errorf("A: LandOutstanding = %s, want 2000000", stmt.Exposure.LandOutstanding)
	}
	if stmt.Exposure.TotalOutstanding != "187000000" {
		t.Errorf("A: TotalOutstanding = %s, want 187000000 (185jt rumah + 2jt tanah)", stmt.Exposure.TotalOutstanding)
	}

	// ── Skenario B: bayar SEBAGIAN (Rp1jt) LANGSUNG ke schedule tanah ───────
	sid := landScheduleID
	partial, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ScheduleID: &sid,
		Amount: domain.FromInt(1_000_000), Date: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lk-sched-partial-1",
	})
	if err != nil {
		t.Fatalf("ReceivePayment (B, parsial tanah): %v", err)
	}
	if partial.ReceiptNumber == "" || partial.ReceiptID == 0 {
		t.Errorf("B: kwitansi harus terbit untuk pembayaran sebagian, got number=%q id=%d", partial.ReceiptNumber, partial.ReceiptID)
	}
	var partialJournalID uint64
	db.Raw(`SELECT journal_entry_id FROM termin_payments WHERE tenant_id = ? AND id = ?`, lbTenant, partial.TerminID).Scan(&partialJournalID)
	journalBalanced(partialJournalID)

	schedules = loadSchedules()
	for _, r := range schedules {
		switch r.ID {
		case landScheduleID:
			if r.PaidAmount != "1000000.0000" {
				t.Errorf("B: tanah paid_amount = %s, want 1000000.0000", r.PaidAmount)
			}
			if r.Status != "scheduled" {
				t.Errorf("B: tanah status = %s, want scheduled (belum lunas penuh)", r.Status)
			}
		case houseScheduleID:
			if r.PaidAmount != "0.0000" {
				t.Errorf("BUG: pembayaran schedule-targeted tanah ikut membayar cicilan RUMAH: paid_amount=%s, want 0.0000", r.PaidAmount)
			}
		}
	}

	stmt, err = svc.GetCustomerStatement(ctx, lbTenant, contract.ID, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetCustomerStatement (B): %v", err)
	}
	if stmt.Exposure.HouseOutstanding != "185000000" {
		t.Errorf("B: HouseOutstanding harus TETAP 185000000 (tak tersentuh), got %s", stmt.Exposure.HouseOutstanding)
	}
	if stmt.Exposure.LandOutstanding != "1000000" {
		t.Errorf("B: LandOutstanding = %s, want 1000000 (2jt - 1jt)", stmt.Exposure.LandOutstanding)
	}
	if stmt.Exposure.TotalOutstanding != "186000000" {
		t.Errorf("B: TotalOutstanding = %s, want 186000000", stmt.Exposure.TotalOutstanding)
	}

	// ── Skenario C: pelunasan SISA (Rp1jt) — LUNAS ──────────────────────────
	full, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ScheduleID: &sid,
		Amount: domain.FromInt(1_000_000), Date: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lk-sched-full-1",
	})
	if err != nil {
		t.Fatalf("ReceivePayment (C, pelunasan tanah): %v", err)
	}
	if full.ReceiptNumber == "" || full.ReceiptID == 0 {
		t.Errorf("C: kwitansi harus terbit untuk pelunasan, got number=%q id=%d", full.ReceiptNumber, full.ReceiptID)
	}
	var fullJournalID uint64
	db.Raw(`SELECT journal_entry_id FROM termin_payments WHERE tenant_id = ? AND id = ?`, lbTenant, full.TerminID).Scan(&fullJournalID)
	journalBalanced(fullJournalID)

	schedules = loadSchedules()
	for _, r := range schedules {
		switch r.ID {
		case landScheduleID:
			if r.PaidAmount != "2000000.0000" {
				t.Errorf("C: tanah paid_amount = %s, want 2000000.0000", r.PaidAmount)
			}
			if r.Status != "received" {
				t.Errorf("C: tanah status = %s, want received (lunas)", r.Status)
			}
		case houseScheduleID:
			if r.PaidAmount != "0.0000" {
				t.Errorf("BUG: pelunasan tanah ikut membayar cicilan RUMAH: paid_amount=%s, want 0.0000", r.PaidAmount)
			}
		}
	}

	stmt, err = svc.GetCustomerStatement(ctx, lbTenant, contract.ID, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetCustomerStatement (C): %v", err)
	}
	if stmt.Exposure.LandOutstanding != "0" {
		t.Errorf("C: LandOutstanding = %s, want 0 (lunas)", stmt.Exposure.LandOutstanding)
	}
	if stmt.Exposure.HouseOutstanding != "185000000" {
		t.Errorf("C: HouseOutstanding harus TETAP 185000000 (rumah belum dibayar sama sekali), got %s", stmt.Exposure.HouseOutstanding)
	}

	// ── Skenario D: lunasi RUMAH via jalur kontrak (waterfall, tanpa
	//    schedule_id) — total statement harus 185jt rumah + 2jt tanah = 187jt,
	//    tidak bercampur dengan pembayaran tanah yang sudah dilakukan di atas ──
	cid := contract.ID
	houseFull, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(housePrice), Date: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lk-house-full-1",
	})
	if err != nil {
		t.Fatalf("ReceivePayment (D, pelunasan rumah): %v", err)
	}
	if houseFull.RemainingBalance != "0" {
		t.Errorf("D: RemainingBalance kontrak = %s, want 0 (185jt rumah + 2jt tanah keduanya lunas)", houseFull.RemainingBalance)
	}

	schedules = loadSchedules()
	for _, r := range schedules {
		wantPaid := "185000000.0000"
		if r.ID == landScheduleID {
			wantPaid = "2000000.0000"
		}
		if r.PaidAmount != wantPaid {
			t.Errorf("D: schedule %d (%s) paid_amount = %s, want %s", r.ID, r.Type, r.PaidAmount, wantPaid)
		}
		if r.Status != "received" {
			t.Errorf("D: schedule %d (%s) status = %s, want received", r.ID, r.Type, r.Status)
		}
	}

	stmt, err = svc.GetCustomerStatement(ctx, lbTenant, contract.ID, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetCustomerStatement (D): %v", err)
	}
	if stmt.TotalPaid != "187000000" {
		t.Errorf("D: TotalPaid = %s, want 187000000 (185jt rumah + 2jt tanah, tidak bercampur)", stmt.TotalPaid)
	}
	if stmt.Exposure.TotalOutstanding != "0" {
		t.Errorf("D: TotalOutstanding = %s, want 0", stmt.Exposure.TotalOutstanding)
	}
	if stmt.Exposure.HouseOutstanding != "0" || stmt.Exposure.LandOutstanding != "0" {
		t.Errorf("D: house/land outstanding harus keduanya 0, got house=%s land=%s",
			stmt.Exposure.HouseOutstanding, stmt.Exposure.LandOutstanding)
	}

	// ── Neraca 1-2000 reconcile ke 0 pasca-lunas keduanya ───────────────────
	var arBalance string
	if err := db.Raw(`SELECT COALESCE(SUM(jl.debit - jl.credit), 0) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE jl.tenant_id = ? AND a.code = ? AND je.posted_at IS NOT NULL`,
		lbTenant, "1-2000").Scan(&arBalance).Error; err != nil {
		t.Fatalf("query saldo 1-2000: %v", err)
	}
	m, _ := domain.NewMoney(arBalance)
	if m.String() != "0" {
		t.Errorf("saldo 1-2000 pasca-lunas keduanya = %s, want 0 (Neraca harus reconcile)", m.String())
	}

	// ── Retry idempoten atas pembayaran schedule-targeted (C) — tidak boleh
	//    memposting jurnal/termin kedua ───────────────────────────────────────
	retry, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ScheduleID: &sid,
		Amount: domain.FromInt(1_000_000), Date: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lk-sched-full-1",
	})
	if err != nil {
		t.Fatalf("ReceivePayment (retry idempotency): %v", err)
	}
	if retry.TerminID != full.TerminID {
		t.Errorf("retry idempotency menghasilkan termin BARU: got %d, want %d", retry.TerminID, full.TerminID)
	}

	_ = landGross // dokumentasi angka skenario
}

// TestIntegration_Land_GeneralPaymentFlow_NotCappedAtHouseOnly menutup laporan
// klien (2026-09-04): "total terutang yang ditampilkan bener di awal tapi
// ketika melakukan penerimaan total yang bisa di bayar mentok maksimal di
// harga rumah saja". Ini persis jalur tombol "+ Catat Penerimaan" umum di
// Unit Detail (bukan tombol Bayar Kelebihan Tanah bertarget-schedule di atas):
// SATU pembayaran gabungan rumah+tanah lewat ContractID (tanpa ScheduleID),
// lewat PreviewCollectionPayment (persis API /collections/payment/preview yang
// dipakai FE utk validasi "Lanjut ke Pratinjau") lalu ReceivePayment.
func TestIntegration_Land_GeneralPaymentFlow_NotCappedAtHouseOnly(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "reserved")
	pool := lbMustPool(t, db, projectID, "1000", "500000")
	_ = pool

	hppStub := &lbStubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: domain.FromInt(200_000), Method: land.HPPMethodActual,
	}}
	svc := lbWire(db, hppStub, lbZeroUnitHPPResolver{})

	const housePrice = 185_000_000
	const landGross = 2_000_000
	const combined = housePrice + landGross

	qty := decimal.RequireFromString("4")
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID: unitID, BuyerName: "Buyer LB General", PaymentType: sale.PaymentTypeTunai,
		ContractDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:   domain.FromInt(housePrice), CustomerID: &cust, LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	if _, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(housePrice), BuyerRef: "Buyer LB General",
		BASTDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	// Ringkasan finansial (dipakai FE sbg hint "Sisa Tagihan") harus sudah
	// menyertakan tanah SEBELUM pembayaran apa pun.
	sum, err := svc.ContractFinancialSummaryByID(ctx, lbTenant, contract.ID)
	if err != nil {
		t.Fatalf("ContractFinancialSummaryByID: %v", err)
	}
	if sum.TotalOutstandingActual.String() != "187000000" {
		t.Fatalf("TotalOutstandingActual pasca-Akad = %s, want 187000000 (185jt rumah + 2jt tanah) — ini field yg wajib dipakai FE (bukan total_outstanding proyeksi pra-Akad)", sum.TotalOutstandingActual.String())
	}

	// Pratinjau (persis GET /collections/payment/preview yang dipanggil tombol
	// "Lanjut ke Pratinjau →") dgn nominal GABUNGAN rumah+tanah, TANPA scheduleId.
	preview, err := svc.PreviewCollectionPayment(ctx, lbTenant, contract.ID, domain.FromInt(combined), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection)
	if err != nil {
		t.Fatalf("PreviewCollectionPayment: %v", err)
	}
	if !preview.Valid {
		t.Fatalf("BUG REPRO: preview menolak pembayaran gabungan rumah+tanah (%d) sbg melebihi piutang — reason=%q, outstanding=%s — ini persis keluhan klien 'mentok maksimal di harga rumah saja'",
			combined, preview.Reason, preview.Outstanding)
	}
	if preview.Outstanding != "187000000" {
		t.Errorf("preview.Outstanding = %s, want 187000000", preview.Outstanding)
	}

	// Submit — harus melunasi KEDUANYA dalam satu transaksi, tanpa tombol
	// terpisah utk tanah.
	cid := contract.ID
	res, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(combined), Date: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lb-general-combined-1",
	})
	if err != nil {
		t.Fatalf("ReceivePayment (gabungan rumah+tanah, satu pintu umum): %v", err)
	}
	if res.RemainingBalance != "0" {
		t.Errorf("RemainingBalance = %s, want 0 (rumah+tanah lunas dalam satu pembayaran)", res.RemainingBalance)
	}
	if isPositive := res.OverpaymentUnapplied != "" && res.OverpaymentUnapplied != "0"; isPositive {
		t.Errorf("OverpaymentUnapplied = %s, want 0 (seluruh nominal harus teralokasi ke rumah+tanah, bukan nyasar jadi saldo kredit)", res.OverpaymentUnapplied)
	}

	var rows []struct {
		Type       string
		Amount     string
		PaidAmount string `gorm:"column:paid_amount"`
		Status     string
	}
	if err := db.Table("payment_schedules").
		Select("type, amount, paid_amount, status").
		Where("tenant_id = ? AND sale_contract_id = ?", lbTenant, contract.ID).
		Scan(&rows).Error; err != nil {
		t.Fatalf("baca payment_schedules: %v", err)
	}
	for _, r := range rows {
		if r.Status != "received" {
			t.Errorf("schedule type=%s status=%s, want received (amount=%s paid=%s)", r.Type, r.Status, r.Amount, r.PaidAmount)
		}
	}

	stmt, err := svc.GetCustomerStatement(ctx, lbTenant, contract.ID, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetCustomerStatement: %v", err)
	}
	if stmt.Exposure.HouseOutstanding != "0" || stmt.Exposure.LandOutstanding != "0" {
		t.Errorf("pasca pembayaran gabungan: house=%s land=%s, want keduanya 0", stmt.Exposure.HouseOutstanding, stmt.Exposure.LandOutstanding)
	}
}

// TestIntegration_Land_PreAkadCombinedAdvance_NettedAtAkad menutup bug
// 2026-09-04 yang dikeluhkan klien: kontrak Tunai lump-sum yang menerima
// SATU pembayaran gabungan rumah+tanah SEBELUM Akad (SumTerminsByUnit
// menjumlahkan seluruhnya jadi satu Uang Muka Penjualan, tanpa mengetahui
// pemisahan rumah/tanah) — sebelum perbaikan, RecordAkad menolak dgn
// ErrAdvanceExceedsSalePrice begitu totalAdvance > harga rumah, PADAHAL
// kelebihannya sah (milik komponen Kelebihan Tanah). Test ini memverifikasi:
//  1. RecordAkad SUKSES (bukan lagi 422) walau totalAdvance > gross rumah.
//  2. Jurnal Event 3 rumah balanced TANPA baris piutang (rumah lunas penuh
//     oleh houseAdvance yang di-cap ke gross rumah).
//  3. Jurnal netting Kelebihan Tanah (Dr UMP / Cr Piutang) balanced sebesar
//     PERSIS landAdvance (kelebihan advance gabungan di atas harga rumah).
//  4. payment_schedules jenis 'land' langsung berstatus received dgn
//     paid_amount == landAdvance (tidak lagi tampak 100% outstanding di
//     laporan piutang tanah — bug asli klien).
//  5. credit_applications TIDAK double-count: SATU baris bertaut ke jadwal
//     tanah (landAdvance) + SATU baris sweep generik utk sisa (houseAdvance,
//     tak bertaut schedule) — totalnya PERSIS sama dgn totalAdvance, saldo
//     kredit tersedia pasca-Akad = 0 (guard #1: tak ada paid_amount yang
//     berubah tanpa baris payment_allocations).
func TestIntegration_Land_PreAkadCombinedAdvance_NettedAtAkad(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "reserved")
	lbMustPool(t, db, projectID, "1000", "500000")

	hppStub := &lbStubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: domain.FromInt(200_000), Method: land.HPPMethodActual,
	}}
	svc := lbWire(db, hppStub, lbZeroUnitHPPResolver{})

	const housePrice = 185_000_000
	const landGross = 2_000_000 // 4 x 500.000
	const combined = housePrice + landGross

	qty := decimal.RequireFromString("4")
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID: unitID, BuyerName: "Buyer LB PreAkad", PaymentType: sale.PaymentTypeTunai,
		ContractDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:   domain.FromInt(housePrice), CustomerID: &cust, LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	// Satu pembayaran gabungan rumah+tanah SEBELUM Akad — seluruhnya jadi
	// buyer-credit (belum ada jadwal cicilan sama sekali pra-Akad, persis
	// kasus nyata Tunai lump-sum). CountsTowardPrice HARUS eksplisit true —
	// ini yang dibaca SumTerminsByUnit sbg totalAdvance.
	tp := &sale.TerminPayment{
		TenantID: lbTenant, UnitID: unitID, ProjectID: projectID, Amount: domain.FromInt(combined),
		BankAccountCode: "1-1300", Date: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		JournalEntryID: 1, CreditAccountCode: "2-2000", CountsTowardPrice: true,
	}
	if err := db.Create(tp).Error; err != nil {
		t.Fatalf("seed termin pra-Akad gabungan: %v", err)
	}
	if err := db.Exec(`INSERT INTO payment_allocations (tenant_id, termin_payment_id, allocation_type, amount)
		VALUES (?,?,?,?)`, lbTenant, tp.ID, "buyer_credit", domain.FromInt(combined)).Error; err != nil {
		t.Fatalf("seed buyer_credit: %v", err)
	}

	journalsBefore := int64(0)
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", lbTenant).Scan(&journalsBefore)

	rec, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(housePrice), BuyerRef: "Buyer LB PreAkad",
		BASTDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BUG REPRO: RecordAkad ditolak (%v) walau kelebihan advance (Rp%d) sah milik Kelebihan Tanah — ini persis keluhan klien 'total terutang benar di awal tapi penerimaan mentok di harga rumah'", err, landGross)
	}

	// ── (2) Event 3 rumah: balanced, TANPA baris piutang (lunas penuh) ─────
	lbJournalBalanced(t, db, rec.RevenueJournalID, "185000000")
	var housePiutangLines int64
	db.Raw(`SELECT COUNT(*) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id AND a.tenant_id = jl.tenant_id
		WHERE jl.tenant_id = ? AND jl.journal_entry_id = ? AND a.code = '1-2000'`,
		lbTenant, rec.RevenueJournalID).Scan(&housePiutangLines)
	if housePiutangLines != 0 {
		t.Errorf("Event 3 rumah punya %d baris Piutang Customer, want 0 (rumah lunas penuh oleh houseAdvance)", housePiutangLines)
	}

	// ── (3) Jurnal netting Kelebihan Tanah: balanced sebesar landAdvance ────
	var nettingJournalID uint64
	if err := db.Raw(`SELECT id FROM journal_entries WHERE tenant_id = ? AND description LIKE ?`,
		lbTenant, "Netting uang muka Kelebihan Tanah%").Scan(&nettingJournalID).Error; err != nil {
		t.Fatalf("cari jurnal netting: %v", err)
	}
	if nettingJournalID == 0 {
		t.Fatal("jurnal netting Kelebihan Tanah tidak ditemukan")
	}
	lbJournalBalanced(t, db, nettingJournalID, "2000000")

	var journalsAfter int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", lbTenant).Scan(&journalsAfter)
	// revenue rumah + revenue tanah + COGS tanah + netting tanah (unit HPP=0 → tanpa Event4 rumah).
	if journalsAfter-journalsBefore != 4 {
		t.Errorf("jurnal baru = %d, want 4 (revenue rumah + revenue tanah + COGS tanah + netting tanah)", journalsAfter-journalsBefore)
	}

	// ── (4) Jadwal tanah: lunas penuh via netting, TIDAK tampak outstanding ──
	var landSchedule struct {
		ID         uint64
		PaidAmount string `gorm:"column:paid_amount"`
		Amount     string
		Status     string
	}
	if err := db.Table("payment_schedules").
		Select("id, paid_amount, amount, status").
		Where("tenant_id = ? AND unit_id = ? AND type = ?", lbTenant, unitID, "land").
		Scan(&landSchedule).Error; err != nil {
		t.Fatalf("baca jadwal tanah: %v", err)
	}
	if landSchedule.ID == 0 {
		t.Fatal("jadwal Kelebihan Tanah tidak ditemukan")
	}
	if landSchedule.PaidAmount != "2000000.0000" || landSchedule.Amount != "2000000.0000" {
		t.Errorf("jadwal tanah amount/paid_amount = %s/%s, want 2000000.0000/2000000.0000 (BUG: sebelumnya paid_amount tetap 0 walau sudah lunas via advance gabungan)",
			landSchedule.Amount, landSchedule.PaidAmount)
	}
	if landSchedule.Status != "received" {
		t.Errorf("status jadwal tanah = %s, want received", landSchedule.Status)
	}

	// ── (5) credit_applications: tidak double-count, total = totalAdvance ──
	type caRow struct {
		PaymentScheduleID *uint64 `gorm:"column:payment_schedule_id"`
		Amount            string
	}
	var apps []caRow
	if err := db.Table("credit_applications").
		Select("payment_schedule_id, amount").
		Where("tenant_id = ? AND unit_id = ?", lbTenant, unitID).
		Scan(&apps).Error; err != nil {
		t.Fatalf("baca credit_applications: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("jumlah credit_applications = %d, want 2 (satu bertaut jadwal tanah, satu sweep generik rumah)", len(apps))
	}
	var toLand, generic string
	for _, a := range apps {
		if a.PaymentScheduleID != nil && *a.PaymentScheduleID == landSchedule.ID {
			toLand = a.Amount
		} else if a.PaymentScheduleID == nil {
			generic = a.Amount
		}
	}
	if toLand != "2000000.0000" {
		t.Errorf("credit_application bertaut jadwal tanah = %s, want 2000000.0000", toLand)
	}
	if generic != "185000000.0000" {
		t.Errorf("credit_application sweep generik = %s, want 185000000.0000 (sisa houseAdvance yang dinetkan Event 3)", generic)
	}

	repo := bcRepo(db)
	after, err := repo.GetBuyerCredit(ctx, lbTenant, unitID)
	if err != nil {
		t.Fatalf("GetBuyerCredit: %v", err)
	}
	if after.Available != "0" {
		t.Errorf("saldo kredit buyer pasca-Akad = %s, want 0 (seluruh advance gabungan sudah tercatat konsumsinya — tidak ada yg 'hilang' atau double count)", after.Available)
	}

	// ── Statement 360: rumah & tanah keduanya 0 pasca-Akad ──────────────────
	stmt, err := svc.GetCustomerStatement(ctx, lbTenant, contract.ID, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetCustomerStatement: %v", err)
	}
	if stmt.Exposure.HouseOutstanding != "0" || stmt.Exposure.LandOutstanding != "0" {
		t.Errorf("pasca netting pra-Akad: house=%s land=%s, want keduanya 0", stmt.Exposure.HouseOutstanding, stmt.Exposure.LandOutstanding)
	}
}
