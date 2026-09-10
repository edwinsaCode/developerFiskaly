//go:build integration

package cancellation_test

// Bug fix Saldo Kredit Buyer (2026-09-04) — sisi pembatalan.
//
// Settlement pembatalan (Process, langkah 5) mendisposisi SELURUH posisi Uang
// Muka unit secara terminal ke penalti dan/atau hutang refund. Sebelum
// perbaikan ini, sub-ledger credit_applications tidak pernah tahu soal
// disposisi tsb — saldo kredit buyer yang tercatat di sana (sources −
// applications) akan TERUS terlihat "tersedia" walau dananya sudah pergi via
// settlement, sama persis dengan cacat #4407 (bedanya di sini pemicunya
// pembatalan, bukan Akad). Langkah 5b (BuyerCreditConsumer, service.go:821)
// menutup sisa itu secara simetris.
//
// Test ini juga sekaligus memvalidasi migrasi 000097 (credit_applications.
// sale_contract_id nullable): skenario PRA-BAST di sini TIDAK PERNAH punya
// sale_contract (identik TestIntegration_Cancellation_PreBAST) — Process
// harus tetap menutup sisa saldo kredit TANPA error meski FK kontrak kosong.
//
// Prasyarat: TEST_DB_DSN + migrasi ≥ 000097.

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/cancellation"
	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// cxBuyerCreditAvailable replikasi query creditBalance (internal/sale/credit_repository.go)
// murni via SQL — test di paket ini tidak boleh construct sale.GORMRepository
// langsung (unexported), representasi paling jujur atas apa yang benar-benar
// tersimpan di sub-ledger.
func cxBuyerCreditAvailable(t *testing.T, db *gorm.DB, unitID uint64) (available, sources, applied string) {
	t.Helper()
	var src, app domain.Money
	if err := db.Raw(`
		SELECT COALESCE(SUM(pa.amount), 0) FROM payment_allocations pa
		JOIN termin_payments tp ON tp.id = pa.termin_payment_id AND tp.tenant_id = pa.tenant_id
		WHERE pa.tenant_id = ? AND pa.allocation_type = 'buyer_credit' AND tp.unit_id = ?`,
		cxTenant, unitID).Scan(&src).Error; err != nil {
		t.Fatalf("sum buyer_credit: %v", err)
	}
	if err := db.Raw(`SELECT COALESCE(SUM(amount), 0) FROM credit_applications WHERE tenant_id = ? AND unit_id = ?`,
		cxTenant, unitID).Scan(&app).Error; err != nil {
		t.Fatalf("sum credit_applications: %v", err)
	}
	return src.Sub(app).String(), src.String(), app.String()
}

func TestIntegration_Cancellation_PreBAST_ClosesRemainingBuyerCredit(t *testing.T) {
	db := cxConnect(t)
	cxCleanup(t, db)
	defer cxCleanup(t, db)
	ctx := context.Background()
	_, unitID := cxSeedProjectUnit(t, db, "CXBC-01", 100, "reserved")
	cxSeedAccounts(t, db)
	saleSvc, _ := cxWireSale(db, false)
	cxSvc := cancellation.NewService(db)
	cxSvc.SetBuyerCreditConsumer(saleSvc) // wiring produksi (lihat cmd/api/main.go)

	// Termin 300jt TANPA kontrak → seluruhnya masuk sub-ledger buyer_credit
	// (pola identik TestIntegration_Cancellation_PreBAST).
	if _, err := saleSvc.ReceivePayment(ctx, cxTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceUnitTermin, UnitID: &unitID,
		Amount: domain.FromInt(300_000_000), Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300",
	}); err != nil {
		t.Fatalf("ReceivePayment: %v", err)
	}

	// Kredit tersedia SEBELUM pembatalan.
	if avail, _, _ := cxBuyerCreditAvailable(t, db, unitID); avail != "300000000" {
		t.Fatalf("available sebelum cancel = %s, want 300000000", avail)
	}

	c, err := cxSvc.Request(ctx, cxTenant, cancellation.RequestInput{
		UnitID: unitID, Reason: "buyer mundur", Penalty: domain.FromInt(50_000_000),
		EventDate: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := cxSvc.Approve(ctx, cxTenant, c.ID, nil); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := cxSvc.Process(ctx, cxTenant, c.ID, nil); err != nil {
		t.Fatalf("Process (harus sukses walau tanpa sale_contract — migrasi 000097): %v", err)
	}

	// ── Sisa saldo kredit HARUS ditutup (bukan dipulihkan) pasca-settlement ──
	avail, sources, applied := cxBuyerCreditAvailable(t, db, unitID)
	if avail != "0" {
		t.Errorf("available pasca-cancel = %s, want 0 (dana sudah didisposisi via settlement)", avail)
	}
	if sources != "300000000" || applied != "300000000" {
		t.Errorf("sources=%s applied=%s, want keduanya 300000000 (tak boleh negatif/berkurang)", sources, applied)
	}

	// SATU baris credit_applications baru, sale_contract_id NULL (unit ini
	// tidak pernah punya sale_contract formal — buktikan migrasi 000097 benar).
	var row struct {
		SaleContractID    *uint64
		PaymentScheduleID *uint64
	}
	if err := db.Raw(`SELECT sale_contract_id, payment_schedule_id FROM credit_applications
		WHERE tenant_id = ? AND unit_id = ?`, cxTenant, unitID).Scan(&row).Error; err != nil {
		t.Fatalf("baca credit_applications: %v", err)
	}
	if row.SaleContractID != nil {
		t.Errorf("sale_contract_id = %v, want nil (tidak ada sale_contract formal)", row.SaleContractID)
	}
	if row.PaymentScheduleID != nil {
		t.Errorf("payment_schedule_id = %v, want nil (konsumsi otomatis)", row.PaymentScheduleID)
	}
	var n int64
	db.Raw(`SELECT COUNT(*) FROM credit_applications WHERE tenant_id = ? AND unit_id = ?`, cxTenant, unitID).Scan(&n)
	if n != 1 {
		t.Errorf("jumlah credit_applications = %d, want 1", n)
	}
}
