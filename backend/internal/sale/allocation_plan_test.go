package sale_test

import (
	"context"
	"testing"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// Auto-allocation preview: rencana "dialokasikan ke tagihan" + saldo kredit,
// diuji lewat PreviewCollectionPayment (read-only, tanpa posting).

func previewSetup(gross int64, unitStatus string, schedules []*sale.PaymentSchedule) (*sale.Service, uint64) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenant, contract, unit, domain.FromInt(gross), schedules)
	units := map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: unitStatus}}
	svc, _, _ := buildServiceWithContracts(units, nil, cs)
	return svc, contract
}

// Scenario C: bayar 500jt menutup beberapa termin + sisa partial.
func TestPreview_AutoAllocation_MultiSchedule(t *testing.T) {
	svc, contract := previewSetup(1_000_000_000, "reserved", []*sale.PaymentSchedule{
		sched(1, 1, 200_000_000), // DP
		sched(2, 2, 200_000_000), // Termin 2
		sched(3, 3, 300_000_000), // Termin 3
		sched(4, 4, 300_000_000), // Termin 4
	})

	p, err := svc.PreviewCollectionPayment(context.Background(), 1, contract, domain.FromInt(500_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !p.Valid {
		t.Fatalf("preview harus valid, reason=%s", p.Reason)
	}
	// 200 (DP lunas) + 200 (T2 lunas) + 100 (T3 sebagian) = 500; saldo kredit 0.
	if len(p.Applied) != 3 {
		t.Fatalf("harus 3 baris alokasi, got %d: %+v", len(p.Applied), p.Applied)
	}
	if !p.Applied[0].FullyPaid || !p.Applied[1].FullyPaid || p.Applied[2].FullyPaid {
		t.Errorf("status alokasi salah: %+v", p.Applied)
	}
	if p.Applied[2].Amount != "100000000" {
		t.Errorf("alokasi T3: got %s, want 100000000", p.Applied[2].Amount)
	}
	if p.BuyerCredit != "0" {
		t.Errorf("BuyerCredit: got %s, want 0", p.BuyerCredit)
	}
}

// Overpay sebelum BAST → preview valid + saldo kredit > 0.
func TestPreview_OverpayBeforeBAST_ShowsBuyerCredit(t *testing.T) {
	svc, contract := previewSetup(1_000_000_000, "reserved", []*sale.PaymentSchedule{
		sched(1, 1, 1_000_000_000),
	})
	p, err := svc.PreviewCollectionPayment(context.Background(), 1, contract, domain.FromInt(1_200_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !p.Valid {
		t.Errorf("overpay pra-BAST harus valid (jadi saldo kredit), reason=%s", p.Reason)
	}
	if p.BuyerCredit != "200000000" {
		t.Errorf("BuyerCredit: got %s, want 200000000", p.BuyerCredit)
	}
}

// Overpay setelah BAST → preview invalid (jujur: submit akan ditolak).
func TestPreview_OverpayAfterBAST_Invalid(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenant, contract, unit, domain.FromInt(1_000_000_000),
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})
	units := map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "sold"}}
	svc, _, ts := buildServiceWithContracts(units, nil, cs)
	ts.SetSaleRecord(&sale.SaleRecord{ID: 1, TenantID: tenant, UnitID: unit, SalePrice: domain.FromInt(1_000_000_000)}, tenant)

	p, err := svc.PreviewCollectionPayment(context.Background(), tenant, contract, domain.FromInt(1_200_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if p.Valid {
		t.Error("overpay pasca-BAST harus invalid di preview (tidak boleh berbohong ke user)")
	}
}

// Preview menampilkan baris jurnal Dr Bank / Cr Uang Muka (pra-BAST).
func TestPreview_JournalLines_PreBAST(t *testing.T) {
	svc, contract := previewSetup(1_000_000_000, "reserved", []*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})
	p, err := svc.PreviewCollectionPayment(context.Background(), 1, contract, domain.FromInt(400_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(p.Lines) != 2 {
		t.Fatalf("harus 2 baris jurnal, got %d", len(p.Lines))
	}
	if p.Lines[1].AccountCode != "2-2000" {
		t.Errorf("kredit pra-BAST harus 2-2000, got %s", p.Lines[1].AccountCode)
	}
}
