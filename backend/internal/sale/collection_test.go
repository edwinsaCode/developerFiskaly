package sale_test

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// QA Collection Transaction Flow: partial, full, overpayment, before/after BAST,
// tenant isolation, duplicate-submit protection.

func collectionDate() time.Time { return time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) }

// setupCollection menyiapkan service + store dengan satu kontrak + jadwal.
func setupCollection(
	tenantID, contractID, unitID uint64,
	gross int64,
	unitStatus string,
	schedules []*sale.PaymentSchedule,
) (*sale.Service, *mockContractStore, *mockTerminStore) {
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenantID, contractID, unitID, domain.FromInt(gross), schedules)
	units := map[uint64]*sale.UnitSaleInfo{unitID: {ID: unitID, ProjectID: 9, Status: unitStatus}}
	svc, _, ts := buildServiceWithContracts(units, nil, cs)
	return svc, cs, ts
}

func sched(id uint64, num int, amount int64) *sale.PaymentSchedule {
	return &sale.PaymentSchedule{
		ID: id, InstallmentNumber: num,
		DueDate: time.Date(2026, time.Month(num+2), 1, 0, 0, 0, 0, time.UTC),
		Amount:  domain.FromInt(amount), Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled,
	}
}

func collReq(contractID uint64, amount int64, key string) sale.CollectionPaymentRequest {
	uid := uint64(7)
	return sale.CollectionPaymentRequest{
		ContractID:      contractID,
		Amount:          domain.FromInt(amount),
		Date:            collectionDate(),
		BankAccountCode: "1-1300",
		Reference:       "TRX-001",
		Notes:           "transfer",
		CreatedBy:       &uid,
		IdempotencyKey:  key,
	}
}

// ── Full payment (before BAST) ───────────────────────────────────────────────

func TestCollection_FullPayment_BeforeBAST(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, cs, _ := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})

	res, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 1_000_000_000, ""))
	if err != nil {
		t.Fatalf("RecordCollectionPayment: %v", err)
	}
	if res.CreditAccount != "2-2000" {
		t.Errorf("CreditAccount: got %s, want 2-2000 (pre-BAST)", res.CreditAccount)
	}
	if res.RemainingBalance != "0" {
		t.Errorf("RemainingBalance: got %s, want 0", res.RemainingBalance)
	}
	if len(res.AppliedSchedules) != 1 || !res.AppliedSchedules[0].FullyPaid {
		t.Errorf("cicilan harus lunas penuh: %+v", res.AppliedSchedules)
	}
	if cs.SchedulePaidAmount(1) != "1000000000" {
		t.Errorf("schedule paid_amount: got %s, want 1000000000", cs.SchedulePaidAmount(1))
	}
}

// ── Partial payment (before BAST) ────────────────────────────────────────────

func TestCollection_PartialPayment_BeforeBAST(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, cs, _ := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 500_000_000), sched(2, 2, 500_000_000)})

	res, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 300_000_000, ""))
	if err != nil {
		t.Fatalf("RecordCollectionPayment: %v", err)
	}
	if res.RemainingBalance != "700000000" {
		t.Errorf("RemainingBalance: got %s, want 700000000", res.RemainingBalance)
	}
	if len(res.AppliedSchedules) != 1 || res.AppliedSchedules[0].FullyPaid {
		t.Errorf("cicilan #1 harus partial (belum lunas): %+v", res.AppliedSchedules)
	}
	if cs.SchedulePaidAmount(1) != "300000000" {
		t.Errorf("schedule #1 paid_amount: got %s, want 300000000", cs.SchedulePaidAmount(1))
	}
	if cs.SchedulePaidAmount(2) != "0" {
		t.Errorf("schedule #2 paid_amount harus 0, got %s", cs.SchedulePaidAmount(2))
	}
}

// ── Overpayment sebelum BAST → saldo kredit buyer (bukan ditolak) ────────────

func TestCollection_Overpayment_BeforeBAST_BecomesBuyerCredit(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, cs, _ := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})

	res, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 1_200_000_000, ""))
	if err != nil {
		t.Fatalf("kelebihan bayar pra-BAST harus diterima: %v", err)
	}
	// Cicilan 1M lunas; sisa 200jt jadi saldo kredit buyer.
	// S8: buyer_credit = saldo KANONIK; overpayment_unapplied = sisa transaksi ini.
	if res.BuyerCredit != "200000000" {
		t.Errorf("BuyerCredit: got %s, want 200000000", res.BuyerCredit)
	}
	if res.OverpaymentUnapplied != "200000000" {
		t.Errorf("OverpaymentUnapplied: got %s, want 200000000", res.OverpaymentUnapplied)
	}
	if len(res.AppliedSchedules) != 1 || !res.AppliedSchedules[0].FullyPaid {
		t.Errorf("cicilan harus lunas penuh: %+v", res.AppliedSchedules)
	}
	if cs.SchedulePaidAmount(1) != "1000000000" {
		t.Errorf("schedule paid_amount: got %s, want 1000000000", cs.SchedulePaidAmount(1))
	}
}

// Kelebihan bayar SETELAH BAST tetap ditolak (split-credit = fase berikutnya).
func TestCollection_Overpayment_AfterBAST_Rejected(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, _, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "sold",
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})
	ts.SetSaleRecord(&sale.SaleRecord{ID: 1, TenantID: tenant, UnitID: unit, SalePrice: domain.FromInt(1_000_000_000)}, tenant)

	_, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 1_200_000_000, ""))
	if err != sale.ErrPaymentExceedsReceivable {
		t.Errorf("kelebihan bayar pasca-BAST harus ditolak: got %v", err)
	}
}

// ── After BAST → credit Piutang ──────────────────────────────────────────────

func TestCollection_Payment_AfterBAST_CreditsPiutang(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, _, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "sold",
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})
	// Simulasikan BAST: SaleRecord ada (DPP 1M).
	ts.SetSaleRecord(&sale.SaleRecord{ID: 1, TenantID: tenant, UnitID: unit, SalePrice: domain.FromInt(1_000_000_000)}, tenant)

	res, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 400_000_000, ""))
	if err != nil {
		t.Fatalf("RecordCollectionPayment: %v", err)
	}
	if res.CreditAccount != "1-2000" {
		t.Errorf("CreditAccount: got %s, want 1-2000 (post-BAST)", res.CreditAccount)
	}
	if res.RemainingBalance != "600000000" {
		t.Errorf("RemainingBalance: got %s, want 600000000", res.RemainingBalance)
	}
}

// ── Tenant isolation ─────────────────────────────────────────────────────────

func TestCollection_TenantIsolation(t *testing.T) {
	const tenantA, tenantB, contract, unit = uint64(1), uint64(2), uint64(10), uint64(100)
	svc, _, _ := setupCollection(tenantA, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})

	// Tenant B mencoba membayar kontrak milik tenant A → kontrak tidak ditemukan.
	_, err := svc.RecordCollectionPayment(context.Background(), tenantB, collReq(contract, 100_000_000, ""))
	if err == nil {
		t.Fatal("tenant B tidak boleh membayar kontrak tenant A")
	}
}

// ── Duplicate submit protection ──────────────────────────────────────────────

func TestCollection_DuplicateSubmit_Idempotent(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, _, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})

	key := "idem-key-123"
	r1, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 400_000_000, key))
	if err != nil {
		t.Fatalf("submit #1: %v", err)
	}
	r2, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 400_000_000, key))
	if err != nil {
		t.Fatalf("submit #2: %v", err)
	}

	if r2.AlreadyExisted != true {
		t.Error("submit #2 harus dikenali sebagai duplikat (AlreadyExisted)")
	}
	if r1.TerminID != r2.TerminID {
		t.Errorf("idempoten: TerminID berbeda %d vs %d", r1.TerminID, r2.TerminID)
	}
	// Hanya SATU termin tercatat → tidak ada jurnal/penerimaan ganda.
	if got := ts.TerminCount(); got != 1 {
		t.Errorf("harus hanya 1 termin tercatat, got %d", got)
	}
}

// ── W-13: Jenis Penerimaan terstruktur (Kind) ────────────────────────────────

// Kontrak TANPA jadwal sama sekali — memenuhi requirement B/F: pengguna tetap
// bisa mencatat "DP" terstruktur tanpa membuat Jadwal Cicilan terlebih dahulu.
func TestCollection_Kind_ExplicitNoSchedule(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, _, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved", nil)

	req := collReq(contract, 300_000_000, "")
	req.Kind = sale.TerminKindDP
	res, err := svc.RecordCollectionPayment(context.Background(), tenant, req)
	if err != nil {
		t.Fatalf("RecordCollectionPayment: %v", err)
	}
	got := ts.termins[len(ts.termins)-1]
	if got.ID != res.TerminID {
		t.Fatalf("termin tersimpan tidak cocok dengan hasil: %d vs %d", got.ID, res.TerminID)
	}
	if got.Kind != sale.TerminKindDP {
		t.Errorf("Kind: got %q, want %q", got.Kind, sale.TerminKindDP)
	}
	if got.InstallmentNo != nil {
		t.Errorf("InstallmentNo harus nil utk DP, got %v", *got.InstallmentNo)
	}
}

// Kind tidak dikirim sama sekali (zero value) → jatuh ke default "other", bukan
// ditolak — konsisten dengan T (aturan stop): tidak ada gate wajib jenis.
func TestCollection_Kind_DefaultsToOther(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, _, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved", nil)

	_, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 300_000_000, ""))
	if err != nil {
		t.Fatalf("RecordCollectionPayment: %v", err)
	}
	got := ts.termins[len(ts.termins)-1]
	if got.Kind != sale.TerminKindOther {
		t.Errorf("Kind default: got %q, want %q", got.Kind, sale.TerminKindOther)
	}
}

// Kind yang tidak dikenal (bukan salah satu dari 5 nilai sah) tidak diloloskan
// mentah-mentah — fail-closed ringan ke "other", bukan menyimpan string bebas.
func TestCollection_Kind_UnknownRejectedToOther(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, _, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved", nil)

	req := collReq(contract, 300_000_000, "")
	req.Kind = sale.TerminKind("not-a-real-kind")
	_, err := svc.RecordCollectionPayment(context.Background(), tenant, req)
	if err != nil {
		t.Fatalf("RecordCollectionPayment: %v", err)
	}
	got := ts.termins[len(ts.termins)-1]
	if got.Kind != sale.TerminKindOther {
		t.Errorf("Kind tak dikenal harus jatuh ke other: got %q", got.Kind)
	}
}

// Pembayaran cicilan spesifik (ScheduleID) — Kind DITURUNKAN dari
// PaymentSchedule.Type/InstallmentNumber, mengabaikan apa pun yang dikirim
// pemanggil. Ini jalur yang dipakai tombol "Terima" di ScheduleRow
// (RecordInstallmentPaid) — otomatis benar tanpa perubahan frontend.
func TestCollection_Kind_ScheduleDerived(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	dp := &sale.PaymentSchedule{ID: 1, InstallmentNumber: 0, Amount: domain.FromInt(200_000_000), Type: sale.ScheduleTypeDP, Status: sale.ScheduleStatusScheduled, DueDate: collectionDate()}
	inst2 := sched(2, 2, 300_000_000)
	final := &sale.PaymentSchedule{ID: 3, InstallmentNumber: 99, Amount: domain.FromInt(500_000_000), Type: sale.ScheduleTypeFinal, Status: sale.ScheduleStatusScheduled, DueDate: collectionDate()}
	svc, _, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{dp, inst2, final})

	pay := func(scheduleID uint64, sentKind sale.TerminKind) *sale.TerminPayment {
		id := scheduleID
		_, err := svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
			Source:          sale.PaymentSourceScheduleReceived,
			ScheduleID:      &id,
			Amount:          domain.FromInt(1), // nominal kecil, tidak relevan utk assersi Kind
			Date:            collectionDate(),
			BankAccountCode: "1-1300",
			Kind:            sentKind, // harus DIABAIKAN — schedule menang
		})
		if err != nil {
			t.Fatalf("ReceivePayment schedule #%d: %v", scheduleID, err)
		}
		return ts.termins[len(ts.termins)-1]
	}

	if got := pay(1, sale.TerminKindOther); got.Kind != sale.TerminKindDP {
		t.Errorf("schedule DP: Kind got %q, want dp", got.Kind)
	}
	if got := pay(2, sale.TerminKindDP); got.Kind != sale.TerminKindInstallment || got.InstallmentNo == nil || *got.InstallmentNo != 2 {
		t.Errorf("schedule installment #2: Kind=%q InstallmentNo=%v, want installment/2", got.Kind, got.InstallmentNo)
	}
	if got := pay(3, sale.TerminKindOther); got.Kind != sale.TerminKindFinalPayment {
		t.Errorf("schedule final: Kind got %q, want final_payment", got.Kind)
	}
}

// Pencairan dana bank KPR — Kind DIPAKSA "bank_disbursement" apa pun yang
// dikirim pemanggil, mengikuti SoT payment_source=kpr_disbursement (D/H).
// Mendukung pencairan bertahap: dua transaksi terpisah, masing-masing dengan
// dokumen/kwitansi sendiri (tidak digabung).
func TestCollection_Kind_BankDisbursementForced(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, _, ts := setupCollection(tenant, contract, unit, 500_000_000, "sold", nil)
	ts.SetSaleRecord(&sale.SaleRecord{ID: 1, TenantID: tenant, UnitID: unit, SalePrice: domain.FromInt(500_000_000)}, tenant)

	disburse := func(amount int64, idem string) {
		_, err := svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
			Source:          sale.PaymentSourceKPRDisbursement,
			ContractID:      ptrU64(contract),
			Amount:          domain.FromInt(amount),
			Date:            collectionDate(),
			BankAccountCode: "1-1300",
			Kind:            sale.TerminKindDP, // pemanggil kirim keliru — harus diabaikan
			IdempotencyKey:  idem,
		})
		if err != nil {
			t.Fatalf("ReceivePayment disbursement: %v", err)
		}
	}
	disburse(250_000_000, "disb-1")
	disburse(150_000_000, "disb-2")

	if got := ts.TerminCount(); got != 2 {
		t.Fatalf("pencairan bertahap harus tercatat sbg 2 transaksi terpisah, got %d", got)
	}
	for _, term := range ts.termins {
		if term.Kind != sale.TerminKindBankDisbursement {
			t.Errorf("termin #%d Kind: got %q, want bank_disbursement", term.ID, term.Kind)
		}
		if term.PaymentSource != sale.PaymentSourceKPRDisbursement {
			t.Errorf("termin #%d PaymentSource: got %q, want kpr_disbursement", term.ID, term.PaymentSource)
		}
	}
}

func ptrU64(v uint64) *uint64 { return &v }
