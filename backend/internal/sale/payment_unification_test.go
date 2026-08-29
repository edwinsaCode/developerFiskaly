package sale_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// Scenario A–G: membuktikan SATU workflow penerimaan (ReceivePayment) — setiap
// pembayaran menghasilkan tepat satu payment, satu journal, satu receipt; dan
// idempotency mencegah duplikasi.

// countingReceiptGen menghitung pemanggilan GenerateReceipt (verifikasi "satu receipt").
type countingReceiptGen struct {
	calls    int
	byTermin map[uint64]int
}

func (g *countingReceiptGen) GenerateReceipt(_ context.Context, _, _, terminID uint64, _ string) (string, uint64, error) {
	g.calls++
	if g.byTermin == nil {
		g.byTermin = map[uint64]int{}
	}
	g.byTermin[terminID]++
	return fmt.Sprintf("KWT/2026/%06d", terminID), terminID, nil
}

func buildUnifiedService(units map[uint64]*sale.UnitSaleInfo, cs *mockContractStore) (*sale.Service, *mockJournalWriter, *mockTerminStore, *countingReceiptGen) {
	af := &mockAccountFinder{accounts: standardAccounts}
	jw := &mockJournalWriter{}
	ur := &mockUnitReader{units: units}
	ts := &mockTerminStore{}
	cp := &mockUnitCostProvider{}
	bw := &mockBASTWriter{}
	rg := &countingReceiptGen{}
	svc := sale.NewService(af, jw, ur, ts, cp, bw, sale.WithContractStore(cs))
	svc.SetReceiptGenerator(rg)
	return svc, jw, ts, rg
}

func uschedule(id uint64, num int, amount int64) *sale.PaymentSchedule {
	return &sale.PaymentSchedule{
		ID: id, InstallmentNumber: num,
		DueDate: time.Date(2026, time.Month(num+2), 1, 0, 0, 0, 0, time.UTC),
		Amount:  domain.FromInt(amount), Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled,
	}
}

func assertExactlyOne(t *testing.T, jw *mockJournalWriter, ts *mockTerminStore, rg *countingReceiptGen) {
	t.Helper()
	if got := len(jw.allLines); got != 1 {
		t.Errorf("harus tepat 1 jurnal, got %d", got)
	}
	if got := ts.TerminCount(); got != 1 {
		t.Errorf("harus tepat 1 payment/termin, got %d", got)
	}
	if rg.calls != 1 {
		t.Errorf("harus tepat 1 receipt, got %d", rg.calls)
	}
}

// ── A. Old endpoint /units/{id}/termins → 1 payment, 1 journal ───────────────

func TestUnification_A_UnitTermin_ExactlyOne(t *testing.T) {
	const tenant, unit = uint64(1), uint64(100)
	cs := newMockContractStore()
	svc, jw, ts, rg := buildUnifiedService(map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "reserved"}}, cs)

	uid := unit
	res, err := svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceUnitTermin, UnitID: &uid,
		Amount: domain.FromInt(250_000_000), Date: time.Now(), BankAccountCode: "1-1300",
	})
	if err != nil {
		t.Fatalf("ReceivePayment: %v", err)
	}
	if res.Source != sale.PaymentSourceUnitTermin {
		t.Errorf("source: got %s, want unit_termin", res.Source)
	}
	assertExactlyOne(t, jw, ts, rg)
}

// ── B. Old endpoint /schedules/{id}/received → 1 payment, 1 journal ──────────

func TestUnification_B_ScheduleReceived_ExactlyOne(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenant, contract, unit, domain.FromInt(1_000_000_000),
		[]*sale.PaymentSchedule{uschedule(1, 1, 1_000_000_000)})
	svc, jw, ts, rg := buildUnifiedService(map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "reserved"}}, cs)

	// Endpoint lama → adapter RecordInstallmentPaid → ReceivePayment.
	updated, err := svc.RecordInstallmentPaid(context.Background(), tenant, sale.RecordInstallmentPaidRequest{
		ScheduleID: 1, BankAccountCode: "1-1300", ReceivedAt: time.Now(), Description: "cicilan",
	})
	if err != nil {
		t.Fatalf("RecordInstallmentPaid: %v", err)
	}
	if updated.Status != sale.ScheduleStatusReceived {
		t.Errorf("cicilan harus received, got %s", updated.Status)
	}
	assertExactlyOne(t, jw, ts, rg)
}

// ── C. Collection endpoint → 1 payment, 1 journal ────────────────────────────

func TestUnification_C_Collection_ExactlyOne(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenant, contract, unit, domain.FromInt(1_000_000_000),
		[]*sale.PaymentSchedule{uschedule(1, 1, 1_000_000_000)})
	svc, jw, ts, rg := buildUnifiedService(map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "reserved"}}, cs)

	cid := uint64(contract)
	res, err := svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(400_000_000), Date: time.Now(), BankAccountCode: "1-1300",
	})
	if err != nil {
		t.Fatalf("ReceivePayment: %v", err)
	}
	if res.ReceiptNumber == "" {
		t.Error("kwitansi harus terbuat")
	}
	assertExactlyOne(t, jw, ts, rg)
}

// ── D. Same idempotency key across calls → only one succeeds ─────────────────

func TestUnification_D_IdempotencyKey_OnlyOne(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenant, contract, unit, domain.FromInt(1_000_000_000),
		[]*sale.PaymentSchedule{uschedule(1, 1, 1_000_000_000)})
	svc, jw, ts, rg := buildUnifiedService(map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "reserved"}}, cs)

	cid := uint64(contract)
	mk := func() sale.ReceivePaymentRequest {
		return sale.ReceivePaymentRequest{
			Source: sale.PaymentSourceCollection, ContractID: &cid,
			Amount: domain.FromInt(400_000_000), Date: time.Now(), BankAccountCode: "1-1300",
			IdempotencyKey: "dup-key-1",
		}
	}
	r1, err := svc.ReceivePayment(context.Background(), tenant, mk())
	if err != nil {
		t.Fatalf("call #1: %v", err)
	}
	r2, err := svc.ReceivePayment(context.Background(), tenant, mk())
	if err != nil {
		t.Fatalf("call #2: %v", err)
	}
	if !r2.AlreadyExisted || r1.TerminID != r2.TerminID {
		t.Errorf("call #2 harus idempotent-hit (sama): r1=%d r2=%d existed=%v", r1.TerminID, r2.TerminID, r2.AlreadyExisted)
	}
	// Tidak boleh ada dua termin / dua jurnal / dua receipt.
	assertExactlyOne(t, jw, ts, rg)
}

// ── E. Tenant isolation ──────────────────────────────────────────────────────

func TestUnification_E_TenantIsolation(t *testing.T) {
	const tenantA, tenantB, contract, unit = uint64(1), uint64(2), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenantA, contract, unit, domain.FromInt(1_000_000_000),
		[]*sale.PaymentSchedule{uschedule(1, 1, 1_000_000_000)})
	svc, _, _, _ := buildUnifiedService(map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "reserved"}}, cs)

	cid := uint64(contract)
	_, err := svc.ReceivePayment(context.Background(), tenantB, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(100_000_000), Date: time.Now(), BankAccountCode: "1-1300",
	})
	if err == nil {
		t.Fatal("tenant B tidak boleh membayar kontrak tenant A")
	}
}

// ── F. Before BAST accounting (Cr Uang Muka) ─────────────────────────────────

func TestUnification_F_BeforeBAST(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenant, contract, unit, domain.FromInt(1_000_000_000),
		[]*sale.PaymentSchedule{uschedule(1, 1, 1_000_000_000)})
	svc, _, _, _ := buildUnifiedService(map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "reserved"}}, cs)

	cid := uint64(contract)
	res, err := svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(400_000_000), Date: time.Now(), BankAccountCode: "1-1300",
	})
	if err != nil {
		t.Fatalf("ReceivePayment: %v", err)
	}
	if res.CreditAccount != "2-2000" {
		t.Errorf("sebelum BAST harus Cr 2-2000 (Uang Muka), got %s", res.CreditAccount)
	}
}

// ── G. After BAST accounting (Cr Piutang) ────────────────────────────────────

func TestUnification_G_AfterBAST(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenant, contract, unit, domain.FromInt(1_000_000_000),
		[]*sale.PaymentSchedule{uschedule(1, 1, 1_000_000_000)})
	svc, _, ts, _ := buildUnifiedService(map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "sold"}}, cs)
	ts.SetSaleRecord(&sale.SaleRecord{ID: 1, TenantID: tenant, UnitID: unit, SalePrice: domain.FromInt(1_000_000_000)}, tenant)

	cid := uint64(contract)
	res, err := svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(400_000_000), Date: time.Now(), BankAccountCode: "1-1300",
	})
	if err != nil {
		t.Fatalf("ReceivePayment: %v", err)
	}
	if res.CreditAccount != "1-2000" {
		t.Errorf("setelah BAST harus Cr 1-2000 (Piutang), got %s", res.CreditAccount)
	}
}
