//go:build integration

package fixedasset_test

// Integration tests terhadap MySQL nyata — memverifikasi hal yang tidak bisa
// dibuktikan oleh mock: isolasi tenant via GORM (CLAUDE.md Invariant #6, MySQL
// tidak punya RLS), fail-closed kategori lintas tenant, dan unique constraint
// DB INV-FA-1 sebagai penjaga terakhir idempotensi penyusutan.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (mengikuti pola
// internal/cost/atomicity_integration_test.go persis).

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/fixedasset"
	"esaproperti/internal/ledger"
)

const (
	faTenantA uint64 = 9_900_087
	faTenantB uint64 = 9_900_088
)

func faConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func faCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"fixed_asset_depreciation_lines", "fixed_assets", "fixed_asset_categories",
		"master_data_changes", "documents", "document_sequences",
		"journal_lines", "journal_entries", "accounts",
	} {
		for _, tenant := range []uint64{faTenantA, faTenantB} {
			if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", tenant).Error; err != nil {
				t.Fatalf("cleanup %s (tenant %d): %v", tbl, tenant, err)
			}
		}
	}
	for _, tenant := range []uint64{faTenantA, faTenantB} {
		if err := document.SeedDefaultDocumentTypes(context.Background(), db, tenant); err != nil {
			t.Fatalf("seed jenis dokumen tenant %d: %v", tenant, err)
		}
	}
}

// faSeed menanam akun 1-4000 (aset), 1-4900 (akumulasi, Asset+Credit — contra
// via NormalBalance override), 5-4500 (beban penyusutan), dan 1-1300 (bank)
// untuk satu tenant, lalu satu kategori aktif memetakan ke ketiganya.
func faSeed(t *testing.T, db *gorm.DB, tenant uint64) {
	t.Helper()
	accs := []*ledger.Account{
		{TenantID: tenant, Code: "1-4000", Name: "Peralatan Kantor", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: tenant, Code: "1-4900", Name: "Akumulasi Penyusutan", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: tenant, Code: "5-4500", Name: "Beban Penyusutan", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: tenant, Code: "1-1300", Name: "Bank BCA", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryBank},
	}
	for _, a := range accs {
		if err := db.Create(a).Error; err != nil {
			t.Fatalf("seed akun %s (tenant %d): %v", a.Code, tenant, err)
		}
	}
	cat := &fixedasset.Category{
		TenantID: tenant, Code: "peralatan-kantor", Name: "Peralatan Kantor",
		AssetAccountCode: "1-4000", AccumulatedDepreciationAccountCode: "1-4900",
		DepreciationExpenseAccountCode: "5-4500", DefaultUsefulLifeMonths: 48, IsActive: true,
	}
	if err := db.Create(cat).Error; err != nil {
		t.Fatalf("seed kategori (tenant %d): %v", tenant, err)
	}
}

func faCategoryID(t *testing.T, db *gorm.DB, tenant uint64) uint64 {
	t.Helper()
	var cat fixedasset.Category
	if err := db.Where("tenant_id = ? AND code = ?", tenant, "peralatan-kantor").First(&cat).Error; err != nil {
		t.Fatalf("baca kategori (tenant %d): %v", tenant, err)
	}
	return cat.ID
}

func faService(db *gorm.DB) (*fixedasset.Service, *fixedasset.GORMRepository) {
	repo := fixedasset.NewGORMRepository(db)
	svc := fixedasset.NewService(fixedasset.NewGORMTxRunner(db), repo, repo)
	return svc, repo
}

func faAcquisition(categoryID uint64) fixedasset.AcquisitionInput {
	return fixedasset.AcquisitionInput{
		CategoryID:         categoryID,
		AssetName:          "Laptop Dell W-FA1",
		AcquisitionDate:    time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		AcquisitionCost:    domain.FromInt(24_000_000),
		ResidualValue:      domain.FromInt(0),
		UsefulLifeMonths:   48,
		DepreciationMethod: fixedasset.DepreciationMethodStraightLine,
		PaymentMethod:      fixedasset.PaymentMethodBank,
		BankAccountCode:    "1-1300",
		Vendor:             "PT Sumber Komputer",
		Description:        "Laptop kerja tim finance",
	}
}

func faCount(t *testing.T, db *gorm.DB, table string, tenant uint64) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Where("tenant_id = ?", tenant).Count(&n).Error; err != nil {
		t.Fatalf("hitung %s: %v", table, err)
	}
	return n
}

// ── Perolehan: jurnal balanced + posted, terhadap MySQL nyata ────────────────

func TestFA_AcquireAsset_PostsBalancedJournal_RealDB(t *testing.T) {
	db := faConnect(t)
	faCleanup(t, db)
	t.Cleanup(func() { faCleanup(t, db) })
	faSeed(t, db, faTenantA)

	svc, _ := faService(db)
	catID := faCategoryID(t, db, faTenantA)

	asset, err := svc.AcquireAsset(context.Background(), faTenantA, faAcquisition(catID))
	if err != nil {
		t.Fatalf("AcquireAsset: %v", err)
	}
	if asset.AssetCode == "" {
		t.Error("asset_code harus terisi setelah create (FA-NNNNNN)")
	}

	var je ledger.JournalEntry
	if err := db.First(&je, "id = ? AND tenant_id = ?", asset.AcquisitionJournalID, faTenantA).Error; err != nil {
		t.Fatalf("baca jurnal perolehan: %v", err)
	}
	if je.PostedAt == nil {
		t.Fatal("jurnal perolehan harus terposting")
	}

	var lines []ledger.JournalLine
	if err := db.Where("journal_entry_id = ?", asset.AcquisitionJournalID).Find(&lines).Error; err != nil {
		t.Fatalf("baca baris jurnal: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 baris jurnal, got %d", len(lines))
	}
	var debitSum, creditSum domain.Money = domain.FromInt(0), domain.FromInt(0)
	for _, l := range lines {
		debitSum = debitSum.Add(l.Debit)
		creditSum = creditSum.Add(l.Credit)
	}
	if !debitSum.Equal(creditSum) {
		t.Errorf("jurnal tidak balanced di DB nyata: debit=%s credit=%s", debitSum, creditSum)
	}
}

// ── Isolasi tenant (Invariant #6) ────────────────────────────────────────────

func TestFA_TenantIsolation_RealDB(t *testing.T) {
	db := faConnect(t)
	faCleanup(t, db)
	t.Cleanup(func() { faCleanup(t, db) })
	faSeed(t, db, faTenantA)
	faSeed(t, db, faTenantB)

	svcA, repoA := faService(db)
	catA := faCategoryID(t, db, faTenantA)
	assetA, err := svcA.AcquireAsset(context.Background(), faTenantA, faAcquisition(catA))
	if err != nil {
		t.Fatalf("AcquireAsset tenant A: %v", err)
	}

	// Tenant B tidak bisa membaca aset tenant A.
	if _, err := repoA.GetAsset(context.Background(), faTenantB, assetA.ID); err == nil {
		t.Error("tenant B seharusnya tidak bisa membaca aset tenant A")
	}

	// Listing tenant B kosong meski tenant A punya aset.
	listB, err := repoA.ListActiveAssets(context.Background(), faTenantB)
	if err != nil {
		t.Fatalf("ListActiveAssets tenant B: %v", err)
	}
	if len(listB) != 0 {
		t.Errorf("listing lintas-tenant harus 0 hasil, got %d", len(listB))
	}

	// Kategori tenant A tidak bisa dipakai oleh tenant B (fail-closed).
	svcB, _ := faService(db)
	_, err = svcB.AcquireAsset(context.Background(), faTenantB, faAcquisition(catA))
	if err == nil {
		t.Error("tenant B memakai category_id tenant A seharusnya gagal (fail-closed lintas tenant)")
	}
}

// ── INV-FA-1: unique constraint sebagai penjaga terakhir ─────────────────────

func TestFA_DepreciationUniqueConstraint_RealDB(t *testing.T) {
	db := faConnect(t)
	faCleanup(t, db)
	t.Cleanup(func() { faCleanup(t, db) })
	faSeed(t, db, faTenantA)

	svc, _ := faService(db)
	catID := faCategoryID(t, db, faTenantA)
	asset, err := svc.AcquireAsset(context.Background(), faTenantA, faAcquisition(catID))
	if err != nil {
		t.Fatalf("AcquireAsset: %v", err)
	}

	// Jalur normal via Service — harus tercatat.
	result, err := svc.RunDepreciation(context.Background(), faTenantA, 2026, 8)
	if err != nil {
		t.Fatalf("RunDepreciation: %v", err)
	}
	if len(result.Posted) != 1 {
		t.Fatalf("expected 1 posted, got %d", len(result.Posted))
	}
	if n := faCount(t, db, "fixed_asset_depreciation_lines", faTenantA); n != 1 {
		t.Fatalf("expected 1 baris penyusutan di DB, got %d", n)
	}

	// Penjaga terakhir: insert langsung baris duplikat (period sama) harus
	// ditolak oleh UNIQUE(tenant_id, fixed_asset_id, period_year, period_month)
	// walau melewati (secara sengaja) pengecekan aplikasi HasDepreciationLine.
	dup := &fixedasset.DepreciationLine{
		TenantID: faTenantA, FixedAssetID: asset.ID,
		PeriodYear: 2026, PeriodMonth: 8, Amount: domain.FromInt(500_000),
		JournalEntryID: asset.AcquisitionJournalID, // journal id tak relevan di sini
	}
	if err := db.Create(dup).Error; err == nil {
		t.Fatal("INV-FA-1: insert baris penyusutan duplikat seharusnya ditolak oleh unique constraint DB")
	}

	// Menjalankan ulang periode yang sama via Service tetap idempoten dan
	// tidak menambah jurnal baru.
	before := faCount(t, db, "journal_entries", faTenantA)
	result2, err := svc.RunDepreciation(context.Background(), faTenantA, 2026, 8)
	if err != nil {
		t.Fatalf("RunDepreciation (ulang): %v", err)
	}
	if len(result2.Posted) != 0 {
		t.Errorf("run kedua periode sama tidak boleh posting, got %d", len(result2.Posted))
	}
	after := faCount(t, db, "journal_entries", faTenantA)
	if after != before {
		t.Errorf("run kedua tidak boleh menambah jurnal baru: before=%d after=%d", before, after)
	}
}

// ── Atomicity: kegagalan simpan Register membatalkan jurnal yang sudah dibuat ─

type faFailCreateAsset struct{ fixedasset.Store }

var errFAInjected = &faInjectedErr{}

type faInjectedErr struct{}

func (*faInjectedErr) Error() string { return "kegagalan disuntik saat menyimpan register aset tetap" }

func (faFailCreateAsset) CreateAsset(_ context.Context, _ *fixedasset.FixedAsset) error {
	return errFAInjected
}

type faWrapRunner struct{ inner fixedasset.TxRunner }

func (w faWrapRunner) InTx(ctx context.Context, fn func(fixedasset.JournalWriter, fixedasset.Store) error) error {
	return w.inner.InTx(ctx, func(jw fixedasset.JournalWriter, st fixedasset.Store) error {
		return fn(jw, faFailCreateAsset{st})
	})
}

func TestFA_AcquireAsset_GagalSimpanRegister_TidakMeninggalkanJurnal_RealDB(t *testing.T) {
	db := faConnect(t)
	faCleanup(t, db)
	t.Cleanup(func() { faCleanup(t, db) })
	faSeed(t, db, faTenantA)

	repo := fixedasset.NewGORMRepository(db)
	runner := faWrapRunner{inner: fixedasset.NewGORMTxRunner(db)}
	svc := fixedasset.NewService(runner, repo, repo)
	catID := faCategoryID(t, db, faTenantA)

	_, err := svc.AcquireAsset(context.Background(), faTenantA, faAcquisition(catID))
	if err == nil {
		t.Fatal("expected kegagalan disuntik")
	}
	if n := faCount(t, db, "journal_entries", faTenantA); n != 0 {
		t.Fatalf("jurnal harus ikut dibatalkan (atomicity); tersisa %d", n)
	}
	if n := faCount(t, db, "fixed_assets", faTenantA); n != 0 {
		t.Fatalf("tidak boleh ada baris register; ada %d", n)
	}
}
