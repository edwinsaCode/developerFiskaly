package sale_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/allocation"
	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// TestGhostInventory_SiklusPenuh_TidakAdaPersediaanHantu membuktikan invariant akun:
//
//	Σ saldo Persediaan (1-3000…3300) == Σ biaya akumulasi unit yang BELUM terjual.
//	Saat SEMUA unit terjual → saldo Persediaan == 0 per kategori (tidak ada persediaan hantu).
//
// Fixture memiliki DUA jenis biaya:
//
//	Biaya project-wide (unit_id=NULL): Land 200 jt  ← biaya yang dialokasikan
//	Biaya langsung U1  (unit_id=1)  : Hard 100 jt  ← biaya langsung ber-unit_id
//	Basis: saleable_area (U1=80m², U2=120m²)
//
// Expected allocation engine Phase 5:
//
//	U1.Total = Land 80M (200M × 80/200) + Hard 100M (direct)
//	U2.Total = Land 120M (200M × 120/200)
//
// Saldo Persediaan AWAL dari kapitalisasi biaya:
//
//	Dr 1-3000 (Land) : 200 jt   (project-wide)
//	Dr 1-3100 (Hard) : 100 jt   (direct U1)
func TestGhostInventory_SiklusPenuh_TidakAdaPersediaanHantu(t *testing.T) {
	ctx := context.Background()

	// ── 1. Fixture biaya ──────────────────────────────────────────────────────

	// Biaya project-wide: ada cost entry ber-unit_id=NULL di jurnal
	projectWideCosts := domain.UnitCostBreakdown{
		Land: rupiah(200_000_000),
	}

	// Biaya langsung unit 1: ada cost entry ber-unit_id=1
	u1Direct := domain.UnitCostBreakdown{Hard: rupiah(100_000_000)}
	u2Direct := domain.UnitCostBreakdown{} // unit 2 tidak ada biaya langsung

	// ── 2. Engine Phase 5 ─────────────────────────────────────────────────────
	// Merepresentasikan allocation.Service.ComputeAllocation pada production.
	// sale.repository.GetUnitCost memanggil ComputeAllocation lalu mengembalikan res.Total.
	unitInputs := []allocation.UnitInput{
		{
			UnitID:       1,
			SaleableArea: decimal.NewFromInt(80),
			SalesValue:   rupiah(1_000_000_000),
			Direct:       u1Direct,
		},
		{
			UnitID:       2,
			SaleableArea: decimal.NewFromInt(120),
			SalesValue:   rupiah(1_500_000_000),
			Direct:       u2Direct,
		},
	}

	engineResults, err := allocation.Compute(projectWideCosts, unitInputs, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatalf("allocation.Compute: %v", err)
	}

	// Bangun costMap[unitID] = Total dari engine (Direct + Allocated)
	// Ini adalah apa yang GORMRepository.GetUnitCost kembalikan (res.Total).
	costMap := make(map[uint64]domain.UnitCostBreakdown)
	for _, r := range engineResults {
		costMap[r.UnitID] = r.Total
	}
	u1Cost := costMap[1]
	u2Cost := costMap[2]

	// ── 3. Verifikasi angka alokasi engine ────────────────────────────────────
	// U1: Land = 200M × (80/200) = 80M
	if !u1Cost.Land.Equal(rupiah(80_000_000)) {
		t.Errorf("U1 Land allocated=%s, want 80_000_000", u1Cost.Land)
	}
	// U1: Hard = 100M (direct, tidak ada alokasi project-wide Hard)
	if !u1Cost.Hard.Equal(rupiah(100_000_000)) {
		t.Errorf("U1 Hard (direct)=%s, want 100_000_000", u1Cost.Hard)
	}
	// U2: Land = 200M × (120/200) = 120M
	if !u2Cost.Land.Equal(rupiah(120_000_000)) {
		t.Errorf("U2 Land allocated=%s, want 120_000_000", u2Cost.Land)
	}
	// U2: Hard = 0 (tidak ada direct, tidak ada project-wide Hard)
	if !u2Cost.Hard.IsZero() {
		t.Errorf("U2 Hard=%s, want 0", u2Cost.Hard)
	}

	// Invariant #3: Σ alokasi Land == projectWideCosts.Land
	sigmaLand := u1Cost.Land.Add(u2Cost.Land)
	if !sigmaLand.Equal(projectWideCosts.Land) {
		t.Errorf("Invariant #3 violated: Σ allocated Land=%s != project-wide=%s",
			sigmaLand, projectWideCosts.Land)
	}

	// ── 4. Saldo Persediaan dari kapitalisasi biaya ───────────────────────────
	// Ini merepresentasikan jurnal yang sudah diposting dari Cost Phase (3/4).
	// Dr 1-3000 Land = 200M; Dr 1-3100 Hard = 100M; yang lain = 0.
	initialInventory := domain.UnitCostBreakdown{
		Land: projectWideCosts.Land, // 200M dari project-wide
		Hard: u1Direct.Hard,         // 100M dari direct U1
	}

	// ── 5. Setup sale service dengan cost dari engine ──────────────────────────
	// buildService dengan mockUnitCostProvider yang mengembalikan res.Total (Direct + Allocated).
	// Ini adalah data yang sama dengan yang GORMRepository.GetUnitCost kembalikan pada production.
	u1Info := &sale.UnitSaleInfo{ID: 1, ProjectID: 10, Status: "reserved"}
	u2Info := &sale.UnitSaleInfo{ID: 2, ProjectID: 10, Status: "reserved"}
	units := map[uint64]*sale.UnitSaleInfo{1: u1Info, 2: u2Info}

	bw := &mockBASTWriter{}
	svc, bast, _ := buildService(standardAccounts, units, nil, costMap, bw)

	// Lacak kumulatif Cr Persediaan dari semua BAST
	cumCr := domain.UnitCostBreakdown{}

	// ── 6. JUAL U1 ────────────────────────────────────────────────────────────
	_, err = svc.RecordAkad(ctx, 1, sale.RecordBASTRequest{
		UnitID:    1,
		SalePrice: rupiah(1_000_000_000),
		BASTDate:  time.Now(),
	})
	if err != nil {
		t.Fatalf("BAST U1: %v", err)
	}

	// Event 4 harus balanced
	assertBalanced(t, "BAST-U1-Event4", bast.captured.COGSLines)

	// Kumpulkan Cr per kategori dari COGSLines U1
	crU1 := creditsByCategory(bast.captured.COGSLines)
	cumCr.Land = cumCr.Land.Add(crU1.Land)
	cumCr.Hard = cumCr.Hard.Add(crU1.Hard)
	cumCr.Soft = cumCr.Soft.Add(crU1.Soft)
	cumCr.Financing = cumCr.Financing.Add(crU1.Financing)

	// HPP yang di-credit untuk U1 harus == U1.Total
	if !crU1.Land.Equal(u1Cost.Land) {
		t.Errorf("BAST U1: Cr Land=%s, want %s (HPP U1 Land)", crU1.Land, u1Cost.Land)
	}
	if !crU1.Hard.Equal(u1Cost.Hard) {
		t.Errorf("BAST U1: Cr Hard=%s, want %s (HPP U1 Hard)", crU1.Hard, u1Cost.Hard)
	}

	// ── INVARIANT SETELAH U1 TERJUAL ─────────────────────────────────────────
	// Saldo Persediaan = Initial - Σ Cr sampai sekarang == biaya U2 (belum terjual).
	balAfterU1 := domain.UnitCostBreakdown{
		Land: initialInventory.Land.Sub(cumCr.Land),
		Hard: initialInventory.Hard.Sub(cumCr.Hard),
	}

	if !balAfterU1.Land.Equal(u2Cost.Land) {
		t.Errorf(
			"[setelah U1 terjual] saldo Land=%s, want %s (biaya akumulasi U2 belum terjual)",
			balAfterU1.Land, u2Cost.Land,
		)
	}
	if !balAfterU1.Hard.Equal(u2Cost.Hard) {
		t.Errorf(
			"[setelah U1 terjual] saldo Hard=%s, want %s (biaya akumulasi U2 belum terjual)",
			balAfterU1.Hard, u2Cost.Hard,
		)
	}

	// ── 7. JUAL U2 ────────────────────────────────────────────────────────────
	_, err = svc.RecordAkad(ctx, 1, sale.RecordBASTRequest{
		UnitID:    2,
		SalePrice: rupiah(1_500_000_000),
		BASTDate:  time.Now(),
	})
	if err != nil {
		t.Fatalf("BAST U2: %v", err)
	}

	assertBalanced(t, "BAST-U2-Event4", bast.captured.COGSLines)

	crU2 := creditsByCategory(bast.captured.COGSLines)
	cumCr.Land = cumCr.Land.Add(crU2.Land)
	cumCr.Hard = cumCr.Hard.Add(crU2.Hard)
	cumCr.Soft = cumCr.Soft.Add(crU2.Soft)
	cumCr.Financing = cumCr.Financing.Add(crU2.Financing)

	// HPP yang di-credit untuk U2 harus == U2.Total
	if !crU2.Land.Equal(u2Cost.Land) {
		t.Errorf("BAST U2: Cr Land=%s, want %s (HPP U2 Land)", crU2.Land, u2Cost.Land)
	}
	if !crU2.Hard.Equal(u2Cost.Hard) { // U2 Hard = 0
		t.Errorf("BAST U2: Cr Hard=%s, want 0", crU2.Hard)
	}

	// ── INVARIANT SEMUA UNIT TERJUAL: saldo == 0 ─────────────────────────────
	finalBal := domain.UnitCostBreakdown{
		Land:      initialInventory.Land.Sub(cumCr.Land),
		Hard:      initialInventory.Hard.Sub(cumCr.Hard),
		Soft:      domain.Zero.Sub(cumCr.Soft),      // initial Soft = 0
		Financing: domain.Zero.Sub(cumCr.Financing), // initial Financing = 0
	}

	if !finalBal.Land.IsZero() {
		t.Errorf(
			"[SEMUA terjual] PERSEDIAAN HANTU: saldo Land=%s, harus 0",
			finalBal.Land,
		)
	}
	if !finalBal.Hard.IsZero() {
		t.Errorf(
			"[SEMUA terjual] PERSEDIAAN HANTU: saldo Hard=%s, harus 0",
			finalBal.Hard,
		)
	}
	if !finalBal.Soft.IsZero() {
		t.Errorf(
			"[SEMUA terjual] PERSEDIAAN HANTU: saldo Soft=%s, harus 0",
			finalBal.Soft,
		)
	}
	if !finalBal.Financing.IsZero() {
		t.Errorf(
			"[SEMUA terjual] PERSEDIAAN HANTU: saldo Financing=%s, harus 0",
			finalBal.Financing,
		)
	}

	// ── 8. Σ Cr == Σ biaya terakumulasi semua unit ────────────────────────────
	// Ini adalah verifikasi akhir: seluruh kredit yang dihasilkan BAST harus
	// sama persis dengan seluruh debit yang dicatat saat kapitalisasi biaya.
	totalInitialInventory := initialInventory.Total()
	totalCrBAST := cumCr.Total()
	if !totalCrBAST.Equal(totalInitialInventory) {
		t.Errorf(
			"Σ Cr BAST=%s != Σ initial inventory=%s (persediaan hantu tersembunyi)",
			totalCrBAST, totalInitialInventory,
		)
	}
}

// TestGhostInventory_AlokasiBesar_200Unit membuktikan tidak ada hantu meski ada 200 unit
// dengan pembulatan largest-remainder — setiap unit mendapat share yang persis.
func TestGhostInventory_AlokasiBesar_TidakAdaSelisDariPembulatan(t *testing.T) {
	const nUnits = 200
	projectWideLand := rupiah(10_000_000_000) // 10 miliar — tidak habis dibagi 200

	unitInputs := make([]allocation.UnitInput, nUnits)
	for i := range unitInputs {
		unitInputs[i] = allocation.UnitInput{
			UnitID:       uint64(i + 1),
			SaleableArea: decimal.NewFromInt(int64(50 + i%10)), // variasi 50–59m²
			SalesValue:   rupiah(int64((i + 1) * 500_000_000)),
			Direct:       domain.UnitCostBreakdown{},
		}
	}

	projectWideCosts := domain.UnitCostBreakdown{Land: projectWideLand}
	results, err := allocation.Compute(projectWideCosts, unitInputs, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// Invariant #3: Σ alokasi Land harus == projectWideLand (persis, tanpa sisa)
	var sigmaLand domain.Money
	for _, r := range results {
		sigmaLand = sigmaLand.Add(r.Total.Land)
	}
	if !sigmaLand.Equal(projectWideLand) {
		t.Errorf(
			"Invariant #3 violated (200 units): Σ Land=%s != projectWide=%s (sisa pembulatan hilang!)",
			sigmaLand, projectWideLand,
		)
	}

	// Jual SEMUA unit dan pastikan total Cr Land == projectWideLand
	svc, bast, _ := buildService(
		standardAccounts,
		buildUnitInfoMap(nUnits),
		nil,
		buildCostMapFromResults(results),
		nil,
	)

	var totalCrLand domain.Money
	for i := 1; i <= nUnits; i++ {
		_, err = svc.RecordAkad(context.Background(), 1, sale.RecordBASTRequest{
			UnitID:    uint64(i),
			SalePrice: rupiah(int64(i) * 500_000_000),
			BASTDate:  time.Now(),
		})
		if err != nil {
			t.Fatalf("BAST unit %d: %v", i, err)
		}
		cr := creditsByCategory(bast.captured.COGSLines)
		totalCrLand = totalCrLand.Add(cr.Land)
	}

	// Σ Cr Land dari semua BAST harus == total project-wide Land
	if !totalCrLand.Equal(projectWideLand) {
		t.Errorf(
			"[200 unit, semua terjual] Σ Cr Land=%s != projectWide=%s (persediaan hantu!)",
			totalCrLand, projectWideLand,
		)
	}
}

// ── helpers khusus ghost inventory test ──────────────────────────────────────

// creditsByCategory memetakan Cr COGSLines ke domain.UnitCostBreakdown.
func creditsByCategory(lines []sale.JournalLineInput) domain.UnitCostBreakdown {
	var b domain.UnitCostBreakdown
	for _, l := range lines {
		switch l.AccountID {
		case accLand:
			b.Land = b.Land.Add(l.Credit)
		case accHard:
			b.Hard = b.Hard.Add(l.Credit)
		case accSoft:
			b.Soft = b.Soft.Add(l.Credit)
		case accFinancing:
			b.Financing = b.Financing.Add(l.Credit)
		}
	}
	return b
}

func buildUnitInfoMap(n int) map[uint64]*sale.UnitSaleInfo {
	m := make(map[uint64]*sale.UnitSaleInfo, n)
	for i := 1; i <= n; i++ {
		uid := uint64(i)
		m[uid] = &sale.UnitSaleInfo{ID: uid, ProjectID: 10, Status: "reserved"}
	}
	return m
}

func buildCostMapFromResults(results []allocation.AllocationResult) map[uint64]domain.UnitCostBreakdown {
	m := make(map[uint64]domain.UnitCostBreakdown, len(results))
	for _, r := range results {
		m[r.UnitID] = r.Total
	}
	return m
}
