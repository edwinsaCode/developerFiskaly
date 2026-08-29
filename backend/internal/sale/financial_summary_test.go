package sale_test

// Hardening Receipt/Invoice — ContractFinancialSummary adalah SATU-SATUNYA
// rumus Terutang. Test memverifikasi: derivasi diskon dari snapshot, fallback
// kontrak lama, seam pencairan KPR (termin kpr_disbursement ikut terhitung),
// dan konsistensi BuyerRemainingBalance == summary.Outstanding.

import (
	"context"
	"testing"

	"esaproperti/internal/sale"
)

// unitWithPrice: unit fixture dengan harga list (dasar snapshot kontrak).
func unitWithPrice(id uint64, status string, price int64) *sale.UnitSaleInfo {
	u := defaultUnit(id, status)
	u.ListPrice = rupiah(price)
	return u
}

func TestContractFinancialSummary_SnapshotAndDiscount(t *testing.T) {
	// Harga unit 500jt, kontrak (DPP) 430jt → diskon derived 70jt.
	cs := newMockContractStore()
	units := map[uint64]*sale.UnitSaleInfo{5: unitWithPrice(5, "reserved", 500_000_000)}
	existingTermins := []*sale.TerminPayment{
		{UnitID: 5, Amount: rupiah(30_000_000)}, // DP
	}
	svc, _, _ := buildServiceWithContracts(units, existingTermins, cs)

	contract, err := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(430_000_000),
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	if contract.UnitPriceSnapshot == nil || !contract.UnitPriceSnapshot.Equal(rupiah(500_000_000)) {
		t.Fatalf("UnitPriceSnapshot: got %v, want 500jt (dibekukan saat kontrak dibuat)", contract.UnitPriceSnapshot)
	}

	sum, err := svc.ContractFinancialSummaryByID(context.Background(), 1, contract.ID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !sum.PriceIsSnapshot {
		t.Error("PriceIsSnapshot: want true")
	}
	if !sum.UnitPrice.Equal(rupiah(500_000_000)) {
		t.Errorf("UnitPrice: got %s, want 500jt", sum.UnitPrice)
	}
	if !sum.Discount.Equal(rupiah(70_000_000)) {
		t.Errorf("Discount: got %s, want 70jt (500jt − 430jt, derived)", sum.Discount)
	}
	if !sum.NetContract.Equal(rupiah(430_000_000)) {
		t.Errorf("NetContract: got %s, want 430jt", sum.NetContract)
	}
	if !sum.TotalPaid.Equal(rupiah(30_000_000)) {
		t.Errorf("TotalPaid: got %s, want 30jt", sum.TotalPaid)
	}
	if !sum.Outstanding.Equal(rupiah(400_000_000)) {
		t.Errorf("Outstanding: got %s, want 400jt (430 − 30)", sum.Outstanding)
	}
}

func TestContractFinancialSummary_LegacyContract_NoSnapshot(t *testing.T) {
	// Unit tanpa list price (0) → snapshot nil → harga fallback DPP, diskon 0.
	cs := newMockContractStore()
	units := map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}
	svc, _, _ := buildServiceWithContracts(units, nil, cs)

	contract, err := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(300_000_000),
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	if contract.UnitPriceSnapshot != nil {
		t.Fatalf("UnitPriceSnapshot: got %v, want nil (list price 0)", contract.UnitPriceSnapshot)
	}

	sum, err := svc.ContractFinancialSummaryByID(context.Background(), 1, contract.ID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.PriceIsSnapshot {
		t.Error("PriceIsSnapshot: want false (kontrak lama)")
	}
	if !sum.UnitPrice.Equal(rupiah(300_000_000)) {
		t.Errorf("UnitPrice fallback: got %s, want DPP 300jt", sum.UnitPrice)
	}
	if !sum.Discount.IsZero() {
		t.Errorf("Discount: got %s, want 0 (tidak mengarang diskon)", sum.Discount)
	}
}

func TestContractFinancialSummary_KPRDisbursementSeam(t *testing.T) {
	// Seam Task 4: pencairan bank = termin ber-source kpr_disbursement.
	// Harga 500jt, bank cair 430jt, buyer bayar 50jt → Terutang 20jt.
	cs := newMockContractStore()
	units := map[uint64]*sale.UnitSaleInfo{5: unitWithPrice(5, "reserved", 500_000_000)}
	existingTermins := []*sale.TerminPayment{
		{UnitID: 5, Amount: rupiah(50_000_000), PaymentSource: sale.PaymentSourceUnitTermin},
		{UnitID: 5, Amount: rupiah(430_000_000), PaymentSource: sale.PaymentSourceKPRDisbursement},
	}
	svc, _, _ := buildServiceWithContracts(units, existingTermins, cs)

	contract, err := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeKPR, BankKPR: ptrStr("BTN"),
		TotalPrice: rupiah(500_000_000),
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	sum, err := svc.ContractFinancialSummaryByID(context.Background(), 1, contract.ID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !sum.TotalPaid.Equal(rupiah(480_000_000)) {
		t.Errorf("TotalPaid: got %s, want 480jt (50 buyer + 430 pencairan bank)", sum.TotalPaid)
	}
	if !sum.Outstanding.Equal(rupiah(20_000_000)) {
		t.Errorf("Outstanding: got %s, want 20jt — kekurangan yang masih ditagih ke customer", sum.Outstanding)
	}

	// SATU rumus: BuyerRemainingBalance harus identik dengan summary.
	remaining, err := svc.BuyerRemainingBalance(context.Background(), 1, contract.ID)
	if err != nil {
		t.Fatalf("BuyerRemainingBalance: %v", err)
	}
	if !remaining.Equal(sum.Outstanding) {
		t.Errorf("BuyerRemainingBalance %s != summary.Outstanding %s (dua jalur hitung!)", remaining, sum.Outstanding)
	}
}

func ptrStr(s string) *string { return &s }
