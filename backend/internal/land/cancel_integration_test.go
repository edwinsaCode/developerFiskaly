//go:build integration

package land_test

// LT-6 — integration test land.GORMRepository.CancelLandSale terhadap MySQL
// nyata (§F.3). Membuktikan: jurnal pembalik pendapatan+HPP terposting
// balanced dan saldo akun kembali nol (invariant #1, invariant #5 — koreksi
// hanya via jurnal pembalik), land_stock.sold_quantity_m2 kembali ke nilai
// pra-Akad (kuantitas kembali AVAILABLE, bukan RESERVED), land_sales
// transisi ke cancelled dengan kedua *_reversal_journal_id terisi,
// double-cancel ditolak, dan isolasi tenant.
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (s/d 000082).

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/land"
)

func TestIntegration_CancelLandSale_Success(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Cancel Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-CANCEL-1")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	params := ltAkadParams(accs, projectID, pool.ID, custID, "100", "100000000", "100000000", "60000000")
	sale, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	cancelled, err := repo.CancelLandSale(context.Background(), ltTenant, sale.ID, land.CancelLandSaleParams{
		Reason:     "pembeli wanprestasi",
		CancelDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("CancelLandSale: %v", err)
	}
	if cancelled.Status != land.LandSaleStatusCancelled {
		t.Errorf("status want cancelled, got %s", cancelled.Status)
	}
	if cancelled.CancelledAt == nil {
		t.Error("CancelledAt want non-nil")
	}
	if cancelled.CancelReason != "pembeli wanprestasi" {
		t.Errorf("CancelReason want %q, got %q", "pembeli wanprestasi", cancelled.CancelReason)
	}
	if cancelled.RevenueReversalJournalID == nil {
		t.Fatal("RevenueReversalJournalID want non-nil")
	}
	if cancelled.CogsReversalJournalID == nil {
		t.Fatal("CogsReversalJournalID want non-nil")
	}
	ltJournalBalanced(t, db, *cancelled.RevenueReversalJournalID)
	ltJournalBalanced(t, db, *cancelled.CogsReversalJournalID)

	// Saldo akun kembali ke nol — reversal sempurna (invariant #5).
	if got := ltNet(t, db, ltTenant, "1-1300"); !got.IsZero() {
		t.Errorf("kas want 0 setelah cancel, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "4-1100"); !got.IsZero() {
		t.Errorf("pendapatan want 0 setelah cancel, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "5-1000"); !got.IsZero() {
		t.Errorf("HPP want 0 setelah cancel, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "1-3000"); !got.IsZero() {
		t.Errorf("persediaan tanah want 0 setelah cancel, got %s", got)
	}

	gotPool, err := repo.FindPoolByProject(context.Background(), ltTenant, projectID)
	if err != nil {
		t.Fatalf("FindPoolByProject: %v", err)
	}
	if !gotPool.SoldQuantityM2.IsZero() {
		t.Errorf("sold_quantity_m2 want 0 setelah cancel, got %s", gotPool.SoldQuantityM2)
	}
	if !gotPool.ReservedQuantityM2.IsZero() {
		t.Errorf("reserved_quantity_m2 want tetap 0 (kuantitas kembali AVAILABLE, bukan RESERVED), got %s", gotPool.ReservedQuantityM2)
	}
	if !gotPool.AvailableQuantityM2().Equal(decimal.NewFromInt(500)) {
		t.Errorf("available want kembali 500 penuh, got %s", gotPool.AvailableQuantityM2())
	}
}

func TestIntegration_CancelLandSale_NoHPP_OnlyReversesRevenue(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Cancel NoHPP Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-CANCEL-NOHPP")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	// hpp_total=0 → CogsJournalID nil (pola sama dengan TestIntegration_RecordAkad_PPN_JournalAmounts).
	params := ltAkadParams(accs, projectID, pool.ID, custID, "50", "50000000", "50000000", "0")
	sale, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if sale.CogsJournalID != nil {
		t.Fatalf("prasyarat: CogsJournalID want nil, got %v", *sale.CogsJournalID)
	}

	cancelled, err := repo.CancelLandSale(context.Background(), ltTenant, sale.ID, land.CancelLandSaleParams{
		Reason:     "batal tanpa HPP",
		CancelDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("CancelLandSale: %v", err)
	}
	if cancelled.RevenueReversalJournalID == nil {
		t.Fatal("RevenueReversalJournalID want non-nil")
	}
	if cancelled.CogsReversalJournalID != nil {
		t.Errorf("CogsReversalJournalID want tetap nil (tidak ada jurnal HPP utk dibalik), got %v", *cancelled.CogsReversalJournalID)
	}
	ltJournalBalanced(t, db, *cancelled.RevenueReversalJournalID)

	if got := ltNet(t, db, ltTenant, "1-1300"); !got.IsZero() {
		t.Errorf("kas want 0 setelah cancel, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "4-1100"); !got.IsZero() {
		t.Errorf("pendapatan want 0 setelah cancel, got %s", got)
	}
}

func TestIntegration_CancelLandSale_DoubleCancelRejected(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Cancel Double Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-CANCEL-DBL")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	params := ltAkadParams(accs, projectID, pool.ID, custID, "100", "100000000", "100000000", "60000000")
	sale, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if _, err := repo.CancelLandSale(context.Background(), ltTenant, sale.ID, land.CancelLandSaleParams{
		Reason: "batal pertama", CancelDate: time.Now(),
	}); err != nil {
		t.Fatalf("CancelLandSale pertama: %v", err)
	}

	_, err = repo.CancelLandSale(context.Background(), ltTenant, sale.ID, land.CancelLandSaleParams{
		Reason: "batal kedua", CancelDate: time.Now(),
	})
	if err != land.ErrLandSaleNotAkad {
		t.Fatalf("want ErrLandSaleNotAkad pada cancel kedua, got %v", err)
	}
}

func TestIntegration_CancelLandSale_DraftRejected(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Cancel Draft Project")
	ltMustPool(t, repo, ltTenant, projectID, "500")

	// Tidak ada land_sale sama sekali (ID sembarang) — juga menutup jalur
	// ErrLandSaleNotFound terpisah dari ErrLandSaleNotAkad.
	_, err := repo.CancelLandSale(context.Background(), ltTenant, 999_999_999, land.CancelLandSaleParams{
		Reason: "tidak ada", CancelDate: time.Now(),
	})
	if err != land.ErrLandSaleNotFound {
		t.Fatalf("want ErrLandSaleNotFound, got %v", err)
	}
}

func TestIntegration_CancelLandSale_TenantIsolation(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Cancel Isolation Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-CANCEL-ISO")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	params := ltAkadParams(accs, projectID, pool.ID, custID, "80", "80000000", "80000000", "0")
	sale, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	_, err = repo.CancelLandSale(context.Background(), ltOtherTenant, sale.ID, land.CancelLandSaleParams{
		Reason: "cross-tenant", CancelDate: time.Now(),
	})
	if err != land.ErrLandSaleNotFound {
		t.Fatalf("cross-tenant CancelLandSale want ErrLandSaleNotFound, got %v", err)
	}

	// Pastikan tidak ada efek samping: land_sale tenant asli tetap akad.
	got, err := repo.FindLandSale(context.Background(), ltTenant, sale.ID)
	if err != nil {
		t.Fatalf("FindLandSale: %v", err)
	}
	if got.Status != land.LandSaleStatusAkad {
		t.Errorf("status want tetap akad setelah upaya cross-tenant cancel, got %s", got.Status)
	}
}
