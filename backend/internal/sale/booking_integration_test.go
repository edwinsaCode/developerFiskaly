//go:build integration

package sale_test

// Increment 7 — Booking: integration (real MySQL). Prasyarat: TEST_DB_DSN +
// DB termigrasi ≥ 000044. Membuktikan siklus penuh: create (jurnal titipan +
// termin + unit booked + log, TANPA alokasi) → convert (kontrak + reklas +
// buyer_credit + unit reserved) → expire/cancel (rule klien: tanpa jurnal) +
// rekonsiliasi saldo Titipan Booking dari ledger.

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
)

const bkTenant uint64 = 9_900_007

func bkCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"payment_allocations", "receipts", "receipt_sequences", "bookings",
		"payment_schedules", "sale_contracts", "termin_payments",
		"documents", "document_sequences", "receipts",
		"journal_lines", "journal_entries",
		"unit_status_transitions", "units", "product_types", "project_phases", "projects",
		"customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", bkTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func bkSeed(t *testing.T, db *gorm.DB) (projectID, unitID, customerID uint64) {
	t.Helper()
	// Master jenis dokumen — produksi menanamnya saat tenant dibuat; resolver
	// fail-closed, jadi tanpa ini kwitansi booking tidak bisa terbit sama sekali.
	if err := slSeedDocumentTypes(db, bkTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		bkTenant, "Booking Project", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)
	// Product Catalog (fail-closed): unit_type WAJIB terdaftar di katalog.
	// Kategori 'property' + akun 4-1000 = perilaku fallback lama, jadi jurnal
	// yang dihasilkan fixture ini identik dengan sebelum hardening.
	db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, bkTenant, "villa", "villa", "property", "4-1000")
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`,
		bkTenant, projectID, "BK-01", "villa", domain.FromInt(100), domain.FromInt(0), "available").Error; err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitID)
	if err := db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`,
		bkTenant, "CUST-BK", "Buyer Booking").Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&customerID)

	accs := []ledger.Account{
		{TenantID: bkTenant, Code: "1-1300", Name: "Bank — BCA", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryBank},
		{TenantID: bkTenant, Code: "2-2000", Name: "Uang Muka Penjualan", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: bkTenant, Code: "2-2100", Name: "Titipan Booking", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: bkTenant, Code: "4-2000", Name: "Pendapatan Lain-lain", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: bkTenant, Code: "4-2100", Name: "Pendapatan Booking", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
	}
	for i := range accs {
		if err := db.Create(&accs[i]).Error; err != nil {
			t.Fatalf("seed account %s: %v", accs[i].Code, err)
		}
	}
	return
}

// bkWire: service produksi minimal utk booking (tanpa scheme flow — kontrak legacy).
func bkWire(db *gorm.DB) *sale.Service {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	repo := slWireReceipts(db, sale.NewGORMRepository(db, posting, nil))
	return sale.NewService(repo, repo, repo, repo, repo, repo,
		sale.WithContractStore(repo), sale.WithBookingStore(repo))
}

// accountBalance: saldo kredit-normal akun (Σkredit − Σdebit) dari POSTED journal lines.
func bkCreditBalance(t *testing.T, db *gorm.DB, code string) string {
	t.Helper()
	var s struct{ Bal string }
	err := db.Raw(`
		SELECT COALESCE(SUM(jl.credit - jl.debit), 0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = ?`, bkTenant, code).
		Scan(&s).Error
	if err != nil {
		t.Fatalf("saldo %s: %v", code, err)
	}
	return s.Bal
}

// bkLegacyizeBooking mengubah booking (dibuat via service kebijakan BARU)
// menjadi SIMULASI baris histori pra-rule: fee dipindah dari 4-2100 ke 2-2100
// (jurnal aux, append-only), disposisi 'held', flag counts_toward_price sesuai
// era yang disimulasikan (TRUE = pra-R4 masuk harga; FALSE = era R4).
func bkLegacyizeBooking(t *testing.T, db *gorm.DB, b *sale.Booking, countsTowardPrice, refundable bool) {
	t.Helper()
	ctx := context.Background()
	var revID, titipanID uint64
	db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = '4-2100'`, bkTenant).Scan(&revID)
	db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = '2-2100'`, bkTenant).Scan(&titipanID)
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo)
	pid, uid := b.ProjectID, b.UnitID
	entry, err := posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID: bkTenant, Date: b.BookingDate,
		Description: "SIMULASI legacy: fee kembali ke titipan (test)",
		Lines: []ledger.LineInput{
			{AccountID: revID, Debit: b.BookingFee, ProjectID: &pid, UnitID: &uid},
			{AccountID: titipanID, Credit: b.BookingFee, ProjectID: &pid, UnitID: &uid},
		},
	})
	if err != nil {
		t.Fatalf("legacyize journal: %v", err)
	}
	if _, err := posting.Post(ctx, bkTenant, entry.ID); err != nil {
		t.Fatalf("legacyize post: %v", err)
	}
	if err := db.Exec(`UPDATE bookings SET fee_disposition = 'held', refundable = ? WHERE id = ? AND tenant_id = ?`,
		refundable, b.ID, bkTenant).Error; err != nil {
		t.Fatalf("legacyize booking: %v", err)
	}
	if err := db.Exec(`UPDATE termin_payments SET counts_toward_price = ?, credit_account_code = '2-2100' WHERE id = ? AND tenant_id = ?`,
		countsTowardPrice, b.TerminPaymentID, bkTenant).Error; err != nil {
		t.Fatalf("legacyize termin: %v", err)
	}
	b.FeeDisposition = sale.FeeHeld
	b.Refundable = refundable
}

func bkBookingReq(unitID, customerID uint64, refundable bool, expiry time.Time) sale.CreateBookingRequest {
	return sale.CreateBookingRequest{
		UnitID:          unitID,
		CustomerID:      customerID,
		BookingFee:      domain.FromInt(5_000_000),
		Refundable:      refundable,
		BankAccountCode: "1-1300",
		BookingDate:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		ExpiryDate:      expiry,
	}
}

func TestIntegration_Booking_CreateLifecycle(t *testing.T) {
	db := itConnect(t)
	bkCleanup(t, db)
	defer bkCleanup(t, db)
	ctx := context.Background()
	_, unitID, customerID := bkSeed(t, db)
	svc := bkWire(db)

	b, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitID, customerID, false, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}

	// RULE KLIEN: fee = PENDAPATAN BOOKING saat diterima (Cr 4-2100);
	// 2-2100 tidak pernah tersentuh; disposisi 'recognized'.
	if bal := bkCreditBalance(t, db, "4-2100"); bal != "5000000.0000" {
		t.Errorf("saldo 4-2100 = %s, want 5000000.0000 (pendapatan diakui)", bal)
	}
	if bal := bkCreditBalance(t, db, "2-2100"); bal != "0" && bal != "0.0000" {
		t.Errorf("saldo 2-2100 = %s, want 0 (bukan liability)", bal)
	}
	if b.FeeDisposition != sale.FeeRecognized {
		t.Errorf("disposisi = %s, want recognized", b.FeeDisposition)
	}
	// Termin: source booking_fee, kredit 4-2100, DI LUAR harga (flag FALSE).
	var termin struct {
		PaymentSource     string
		CreditAccountCode string
		CountsTowardPrice bool
	}
	db.Raw("SELECT payment_source, credit_account_code, counts_toward_price FROM termin_payments WHERE id = ?", b.TerminPaymentID).Scan(&termin)
	if termin.PaymentSource != "booking_fee" || termin.CreditAccountCode != "4-2100" || termin.CountsTowardPrice {
		t.Errorf("termin = %+v, want booking_fee/4-2100/counts=false", termin)
	}
	// TANPA alokasi: fee tertahan lifecycle booking (bukan buyer credit).
	var allocs int64
	db.Raw("SELECT COUNT(*) FROM payment_allocations WHERE tenant_id = ?", bkTenant).Scan(&allocs)
	if allocs != 0 {
		t.Errorf("payment_allocations harus 0 saat booking dibuat, got %d", allocs)
	}
	// Unit booked + log booking_created ref booking.
	var st string
	db.Raw("SELECT status FROM units WHERE id = ?", unitID).Scan(&st)
	if st != "booked" {
		t.Errorf("unit status = %s, want booked", st)
	}
	var lg struct {
		Event         string
		ReferenceType string
		ReferenceID   uint64
	}
	db.Raw(`SELECT event, reference_type, reference_id FROM unit_status_transitions
		WHERE tenant_id = ? AND unit_id = ? ORDER BY id DESC LIMIT 1`, bkTenant, unitID).Scan(&lg)
	if lg.Event != "booking_created" || lg.ReferenceType != "booking" || lg.ReferenceID != b.ID {
		t.Errorf("log = %+v, want booking_created/booking/%d", lg, b.ID)
	}

	// Satu booking aktif per unit.
	if _, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitID, customerID, false, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))); !errors.Is(err, sale.ErrBookingUnitStateInvalid) {
		// Unit sudah booked → validasi dini menolak sebelum UNIQUE tersentuh.
		t.Errorf("booking kedua: want ErrBookingUnitStateInvalid, got %v", err)
	}
}

func TestIntegration_Booking_ConvertToContract(t *testing.T) {
	db := itConnect(t)
	bkCleanup(t, db)
	defer bkCleanup(t, db)
	ctx := context.Background()
	_, unitID, customerID := bkSeed(t, db)
	svc := bkWire(db)

	b, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitID, customerID, false, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}

	journalsBefore := int64(0)
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", bkTenant).Scan(&journalsBefore)

	cust := customerID
	bid := b.ID
	contract, err := svc.CreateContract(ctx, bkTenant, sale.CreateContractRequest{
		UnitID:       unitID,
		BuyerName:    "Buyer Booking",
		PaymentType:  sale.PaymentTypeTunai,
		ContractDate: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		TotalPrice:   domain.FromInt(1_000_000_000),
		CustomerID:   &cust,
		BookingID:    &bid,
	})
	if err != nil {
		t.Fatalf("CreateContract (konversi): %v", err)
	}
	if contract.ID == 0 {
		t.Fatal("kontrak tidak tersimpan")
	}

	// RULE KLIEN: booking converted → fee TETAP Pendapatan Booking
	// ('recognized'), TANPA jurnal reklas, tidak pernah menjadi uang muka.
	got, _ := svc.GetBooking(ctx, bkTenant, b.ID)
	if got.Status != sale.BookingStatusConverted || got.FeeDisposition != sale.FeeRecognized {
		t.Errorf("booking = %s/%s, want converted/recognized (fee = pendapatan final)", got.Status, got.FeeDisposition)
	}
	if got.ConvertedContractID == nil || *got.ConvertedContractID != contract.ID {
		t.Errorf("converted_contract_id = %v, want %d", got.ConvertedContractID, contract.ID)
	}
	if got.ReclassJournalID != nil {
		t.Error("reclass_journal_id harus NIHIL (tanpa jurnal reklas)")
	}

	// Ledger: Pendapatan Booking utuh; uang muka & titipan TIDAK menerima fee.
	if bal := bkCreditBalance(t, db, "4-2100"); bal != "5000000.0000" {
		t.Errorf("saldo 4-2100 pasca-konversi = %s, want 5000000.0000 (pendapatan utuh)", bal)
	}
	if bal := bkCreditBalance(t, db, "2-2000"); bal != "0" && bal != "0.0000" {
		t.Errorf("saldo 2-2000 pasca-konversi = %s, want 0 (fee di luar harga)", bal)
	}
	if bal := bkCreditBalance(t, db, "2-2100"); bal != "0" && bal != "0.0000" {
		t.Errorf("saldo 2-2100 pasca-konversi = %s, want 0 (bukan liability)", bal)
	}

	// Sub-ledger: TIDAK ada buyer_credit dari fee outside-price.
	var bc struct {
		N      int64
		Amount string
	}
	db.Raw(`SELECT COUNT(*) AS n, COALESCE(MAX(amount),'0') AS amount FROM payment_allocations
		WHERE tenant_id = ? AND allocation_type = 'buyer_credit' AND termin_payment_id = ?`,
		bkTenant, b.TerminPaymentID).Scan(&bc)
	if bc.N != 0 {
		t.Errorf("buyer_credit = %+v, want 0 baris (fee outside price)", bc)
	}

	// Outstanding kontrak = gross PENUH (fee tidak mengurangi harga).
	sum, serr := svc.ContractFinancialSummaryByID(ctx, bkTenant, contract.ID)
	if serr != nil {
		t.Fatalf("summary: %v", serr)
	}
	if sum.Outstanding.String() != "1000000000" {
		t.Errorf("outstanding = %s, want 1000000000 (gross penuh)", sum.Outstanding)
	}

	// Unit: booked → reserved (event reservation_confirmed ref booking).
	var st string
	db.Raw("SELECT status FROM units WHERE id = ?", unitID).Scan(&st)
	if st != "reserved" {
		t.Errorf("unit status = %s, want reserved (D3 reserved→ppjb tidak di-wire di env test)", st)
	}
	var events []string
	db.Raw(`SELECT event FROM unit_status_transitions WHERE tenant_id = ? AND unit_id = ? ORDER BY id`,
		bkTenant, unitID).Scan(&events)
	if len(events) != 2 || events[0] != "booking_created" || events[1] != "reservation_confirmed" {
		t.Errorf("lifecycle events = %v", events)
	}

	// Konversi ulang: booking sudah terminal → konflik; tidak ada kontrak kedua.
	if _, err := svc.CreateContract(ctx, bkTenant, sale.CreateContractRequest{
		UnitID: unitID, BuyerName: "X", PaymentType: sale.PaymentTypeTunai,
		ContractDate: time.Now(), TotalPrice: domain.FromInt(1), CustomerID: &cust, BookingID: &bid,
	}); !errors.Is(err, sale.ErrBookingNotActive) {
		t.Errorf("konversi ulang: want ErrBookingNotActive, got %v", err)
	}

	// ── Disposisi R4 (legacy) TIDAK berlaku utk booking recognized ───────────
	// Pendapatan sudah final — tidak ada yang bisa di-forfeit/refund.
	if _, err := svc.DisposeConvertedBookingFee(ctx, bkTenant, b.ID,
		sale.FeeDispositionActionForfeit, "", time.Time{}, nil); !errors.Is(err, sale.ErrFeeNotDisposable) {
		t.Errorf("disposisi booking recognized: want ErrFeeNotDisposable, got %v", err)
	}
}

// TestIntegration_Booking_ConvertCutoverLegacy: booking IN-FLIGHT pra-R4
// (fee termin counts_toward_price=TRUE) dikonversi via JALUR LAMA —
// reklas 2-2100→2-2000 + buyer_credit + disposisi transferred (design §9.3).
func TestIntegration_Booking_ConvertCutoverLegacy(t *testing.T) {
	db := itConnect(t)
	bkCleanup(t, db)
	defer bkCleanup(t, db)
	ctx := context.Background()
	_, unitID, customerID := bkSeed(t, db)
	svc := bkWire(db)

	b, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitID, customerID, false, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	// Simulasikan baris PRA-R4 (histori/in-flight): fee di 2-2100 (held),
	// termin flag TRUE (fee bagian harga era lama).
	bkLegacyizeBooking(t, db, b, true, false)

	cust := customerID
	bid := b.ID
	contract, err := svc.CreateContract(ctx, bkTenant, sale.CreateContractRequest{
		UnitID: unitID, BuyerName: "Buyer Legacy", PaymentType: sale.PaymentTypeTunai,
		ContractDate: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		TotalPrice:   domain.FromInt(1_000_000_000), CustomerID: &cust, BookingID: &bid,
	})
	if err != nil {
		t.Fatalf("CreateContract (cutover legacy): %v", err)
	}

	got, _ := svc.GetBooking(ctx, bkTenant, b.ID)
	if got.Status != sale.BookingStatusConverted || got.FeeDisposition != sale.FeeTransferred {
		t.Errorf("booking = %s/%s, want converted/transferred (jalur lama)", got.Status, got.FeeDisposition)
	}
	if got.ReclassJournalID == nil {
		t.Error("reclass_journal_id harus terisi (jalur lama)")
	}
	if bal := bkCreditBalance(t, db, "2-2100"); bal != "0.0000" {
		t.Errorf("saldo 2-2100 = %s, want 0.0000 (fee direklas)", bal)
	}
	if bal := bkCreditBalance(t, db, "2-2000"); bal != "5000000.0000" {
		t.Errorf("saldo 2-2000 = %s, want 5000000.0000 (fee jadi uang muka)", bal)
	}
	// Fee legacy MENGURANGI outstanding (kompatibilitas histori).
	sum, serr := svc.ContractFinancialSummaryByID(ctx, bkTenant, contract.ID)
	if serr != nil {
		t.Fatalf("summary: %v", serr)
	}
	if sum.Outstanding.String() != "995000000" {
		t.Errorf("outstanding legacy = %s, want 995000000 (gross − fee)", sum.Outstanding)
	}
	// Disposisi R4 TIDAK berlaku untuk jalur lama.
	if _, err := svc.DisposeConvertedBookingFee(ctx, bkTenant, b.ID,
		sale.FeeDispositionActionForfeit, "", time.Time{}, nil); !errors.Is(err, sale.ErrFeeNotDisposable) {
		t.Errorf("disposisi jalur lama: want ErrFeeNotDisposable, got %v", err)
	}
}

func TestIntegration_Booking_ExpireForfeitAndCancelRefundable(t *testing.T) {
	db := itConnect(t)
	bkCleanup(t, db)
	defer bkCleanup(t, db)
	ctx := context.Background()
	projectID, unitA, customerID := bkSeed(t, db)
	// Unit kedua untuk kasus refundable.
	var unitB uint64
	db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`, bkTenant, projectID, "BK-02", "villa", domain.FromInt(100), domain.FromInt(0), "available")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitB)
	svc := bkWire(db)

	// A: default non-refundable, expiry 10 Jul (recognized, 4-2100).
	// B: Item 3 — ditandai Refundable SAAT DIBUAT → jalur held (2-2100 Titipan
	// Booking), dibatalkan manual nanti (bukan diabaikan lagi sejak Item 3).
	bA, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitA, customerID, false, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("booking A: %v", err)
	}
	bB, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitB, customerID, true, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("booking B: %v", err)
	}
	if !bB.Refundable || bB.FeeDisposition != sale.FeeHeld {
		t.Errorf("B = refundable=%v disposisi=%s, want refundable=true/held (Item 3)", bB.Refundable, bB.FeeDisposition)
	}
	// 4-2100 hanya A (fee B belum diakui pendapatan); 2-2100 = fee B (titipan).
	if bal := bkCreditBalance(t, db, "4-2100"); bal != "5000000.0000" {
		t.Errorf("saldo 4-2100 = %s, want 5000000.0000 (hanya A)", bal)
	}
	if bal := bkCreditBalance(t, db, "2-2100"); bal != "5000000.0000" {
		t.Errorf("saldo 2-2100 = %s, want 5000000.0000 (titipan B)", bal)
	}
	journalsBefore := int64(0)
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", bkTenant).Scan(&journalsBefore)

	// ── Sweep expiry per 12 Jul: hanya A yang tutup — TANPA jurnal apa pun ────
	// (pendapatan booking sudah final saat diterima; tidak ada forfeit/reversal).
	n, err := svc.MarkExpiredBookings(ctx, bkTenant, time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))
	if err != nil || n != 1 {
		t.Fatalf("sweep: n=%d err=%v (want 1, nil)", n, err)
	}
	gA, _ := svc.GetBooking(ctx, bkTenant, bA.ID)
	if gA.Status != sale.BookingStatusExpired || gA.FeeDisposition != sale.FeeRecognized || gA.ForfeitJournalID != nil {
		t.Errorf("A = %s/%s forfeit=%v, want expired/recognized/nil (tanpa jurnal)", gA.Status, gA.FeeDisposition, gA.ForfeitJournalID)
	}
	if bal := bkCreditBalance(t, db, "4-2000"); bal != "0" && bal != "0.0000" {
		t.Errorf("saldo 4-2000 = %s, want 0 (tidak ada forfeit di rule baru)", bal)
	}
	var stA string
	db.Raw("SELECT status FROM units WHERE id = ?", unitA).Scan(&stA)
	if stA != "available" {
		t.Errorf("unit A = %s, want available", stA)
	}
	// Sweep idempoten.
	if n, _ := svc.MarkExpiredBookings(ctx, bkTenant, time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)); n != 0 {
		t.Errorf("sweep kedua = %d, want 0", n)
	}

	// ── Cancel B (held+refundable): TANPA jurnal saat cancel — reklas/refund
	// baru terjadi lewat CreateBookingRefund/PayRefund terpisah (belum dipicu
	// di sini) ─────────────────────────────────────────────────────────────
	gB, err := svc.CancelBooking(ctx, bkTenant, bB.ID, "buyer mundur", time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatalf("CancelBooking B: %v", err)
	}
	if gB.Status != sale.BookingStatusCancelled || gB.FeeDisposition != sale.FeePendingRefund || gB.ForfeitJournalID != nil {
		t.Errorf("B = %s/%s, want cancelled/pending_refund tanpa jurnal forfeit", gB.Status, gB.FeeDisposition)
	}
	var journalsAfter int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", bkTenant).Scan(&journalsAfter)
	if journalsAfter != journalsBefore {
		t.Errorf("cancel booking held→pending_refund tidak boleh menulis jurnal (reklas baru terjadi di CreateBookingRefund): %d → %d", journalsBefore, journalsAfter)
	}
	// Pendapatan A utuh (recognized, final); titipan B TETAP di 2-2100 sampai
	// diproses refund (CreateBookingRefund/PayRefund) — belum dipicu di sini.
	if bal := bkCreditBalance(t, db, "4-2100"); bal != "5000000.0000" {
		t.Errorf("saldo 4-2100 = %s, want 5000000.0000 (hanya A, final)", bal)
	}
	if bal := bkCreditBalance(t, db, "2-2100"); bal != "5000000.0000" {
		t.Errorf("saldo 2-2100 = %s, want 5000000.0000 (titipan B menunggu refund)", bal)
	}

	// ── JALUR LEGACY (baris histori 'held'): forfeit lama tetap jalan ─────────
	var unitC uint64
	db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`, bkTenant, projectID, "BK-03", "villa", domain.FromInt(100), domain.FromInt(0), "available")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitC)
	bC, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitC, customerID, false, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("booking C: %v", err)
	}
	bkLegacyizeBooking(t, db, bC, false, false) // simulasi baris era-R4: held, non-refundable
	gC, err := svc.CancelBooking(ctx, bkTenant, bC.ID, "legacy batal", time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatalf("CancelBooking C (legacy): %v", err)
	}
	if gC.FeeDisposition != sale.FeeForfeited || gC.ForfeitJournalID == nil {
		t.Errorf("C legacy = %s forfeit=%v, want forfeited + jurnal", gC.FeeDisposition, gC.ForfeitJournalID)
	}
	if bal := bkCreditBalance(t, db, "4-2000"); bal != "5000000.0000" {
		t.Errorf("saldo 4-2000 = %s, want 5000000.0000 (forfeit legacy C)", bal)
	}
	// 2-2100 = titipan B yang masih pending_refund (Item 3, belum diproses
	// CreateBookingRefund/PayRefund) — titipan legacy C sudah keluar (forfeit).
	if bal := bkCreditBalance(t, db, "2-2100"); bal != "5000000.0000" {
		t.Errorf("saldo 2-2100 = %s, want 5000000.0000 (titipan B pending_refund, titipan legacy C sudah keluar)", bal)
	}
}
