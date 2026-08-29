package sale_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// ── mockPPhFinalAccruer ───────────────────────────────────────────────────────

// mockPPhFinalAccruer implements sale.PPhFinalAccruer for testing.
// It records whether it was called and can be configured to return an error.
type mockPPhFinalAccruer struct {
	called        bool
	capturedValue domain.Money
	err           error
}

func (m *mockPPhFinalAccruer) AccruePPhFinalInTx(
	_ context.Context, _ *gorm.DB,
	_, _, _ uint64,
	transferValue domain.Money, _ time.Time,
) error {
	m.called = true
	m.capturedValue = transferValue
	return m.err
}

// ── bastWriterWithPPh ────────────────────────────────────────────────────────
//
// bastWriterWithPPh simulates GORMRepository.Execute() at unit-test level.
// It faithfully models the atomicity contract:
//
//   Event 3 → revenue journal "created" (tracked)
//   Event 4 → HPP journal "created" (tracked, only if COGSLines non-empty)
//   Event 5 → taxAccruer.AccruePPhFinalInTx() called (same simulated tx)
//   error   → simulated rollback: tracked state cleared, no SaleRecord returned

type bastWriterWithPPh struct {
	taxAccruer      sale.PPhFinalAccruer
	revenueCreated  bool
	hppCreated      bool
	saleRecordSaved bool
}

func (w *bastWriterWithPPh) Execute(ctx context.Context, p sale.BASTAtomicParams) (*sale.SaleRecord, error) {
	// Event 3: Revenue
	if len(p.RevenueLines) > 0 {
		w.revenueCreated = true
	}

	// Event 4: HPP (only if cost lines exist)
	if len(p.COGSLines) > 0 {
		w.hppCreated = true
	}

	// Event 5: PPh Final accrual (inside the same simulated transaction)
	if w.taxAccruer != nil {
		if err := w.taxAccruer.AccruePPhFinalInTx(ctx, nil,
			p.TenantID, p.UnitID, p.ProjectID, p.SalePrice, p.BASTDate); err != nil {
			// Simulate GORM transaction rollback: undo tracked state
			w.revenueCreated = false
			w.hppCreated = false
			return nil, fmt.Errorf("akrual PPh Final: %w", err)
		}
	}

	// SaleRecord persisted only if all events succeeded
	w.saleRecordSaved = true
	return &sale.SaleRecord{
		ID:        1,
		TenantID:  p.TenantID,
		UnitID:    p.UnitID,
		ProjectID: p.ProjectID,
		SalePrice: p.SalePrice,
	}, nil
}

// ── Helper ────────────────────────────────────────────────────────────────────

// buildServicePPh constructs a sale.Service with a custom BASTAtomicWriter.
// Reuses mocks already declared in service_test.go (same package).
func buildServicePPh(
	accounts map[string]uint64,
	units map[uint64]*sale.UnitSaleInfo,
	termins []*sale.TerminPayment,
	costs map[uint64]domain.UnitCostBreakdown,
	bastWriter sale.BASTAtomicWriter,
) *sale.Service {
	af := &mockAccountFinder{accounts: accounts}
	jw := &mockJournalWriter{}
	ur := &mockUnitReader{units: units}
	ts := &mockTerminStore{termins: termins}
	cp := &mockUnitCostProvider{costs: costs}
	return sale.NewService(af, jw, ur, ts, cp, bastWriter)
}

// ── Sprint 1 QA #2: Auto PPh Final — 3 events ────────────────────────────────

// TestBASTExecute_ThreeJournals_Created (Sprint 1 QA #2):
// BAST normal harus menghasilkan 3 event:
//   Event 3 — Jurnal pengakuan pendapatan (revenue)
//   Event 4 — Jurnal HPP (COGS release)
//   Event 5 — PPh Final accrual (otomatis, dalam transaksi yang sama)
func TestBASTExecute_ThreeJournals_Created(t *testing.T) {
	taxAccruer := &mockPPhFinalAccruer{}
	bastWriter := &bastWriterWithPPh{taxAccruer: taxAccruer}

	hpp := domain.UnitCostBreakdown{Hard: rupiah(500_000_000)}
	svc := buildServicePPh(
		standardAccounts,
		map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil,
		map[uint64]domain.UnitCostBreakdown{5: hpp},
		bastWriter,
	)

	req := sale.RecordBASTRequest{
		UnitID:    5,
		SalePrice: rupiah(1_000_000_000),
		BASTDate:  time.Now(),
	}
	rec, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordBAST unexpected error: %v", err)
	}
	if rec == nil {
		t.Fatal("SaleRecord nil")
	}

	// Event 3: revenue journal
	if !bastWriter.revenueCreated {
		t.Error("Event 3: revenue journal tidak dibuat")
	}
	// Event 4: HPP journal (HPP > 0)
	if !bastWriter.hppCreated {
		t.Error("Event 4: HPP journal tidak dibuat padahal HPP > 0")
	}
	// Event 5: PPh Final called
	if !taxAccruer.called {
		t.Error("Event 5: PPh Final accruer tidak dipanggil")
	}
	// PPh Final transfer value == SalePrice (DPP)
	if !taxAccruer.capturedValue.Equal(rupiah(1_000_000_000)) {
		t.Errorf("PPh Final transfer value = %s, want 1000000000", taxAccruer.capturedValue)
	}
	// SaleRecord saved last (setelah semua event sukses)
	if !bastWriter.saleRecordSaved {
		t.Error("SaleRecord tidak disimpan setelah semua event sukses")
	}
}

// TestBASTExecute_ThreeJournals_ZeroHPP_OnlyRevenueAndPPh:
// Jika HPP = 0, Event 4 tidak ada — tapi Event 3 dan Event 5 tetap harus ada.
func TestBASTExecute_ThreeJournals_ZeroHPP_OnlyRevenueAndPPh(t *testing.T) {
	taxAccruer := &mockPPhFinalAccruer{}
	bastWriter := &bastWriterWithPPh{taxAccruer: taxAccruer}

	// HPP = 0 (unit tanpa biaya terakumulasi)
	svc := buildServicePPh(
		standardAccounts,
		map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil,
		map[uint64]domain.UnitCostBreakdown{5: {}},
		bastWriter,
	)

	req := sale.RecordBASTRequest{
		UnitID:    5,
		SalePrice: rupiah(800_000_000),
		BASTDate:  time.Now(),
	}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordBAST: %v", err)
	}

	if !bastWriter.revenueCreated {
		t.Error("Event 3: revenue journal tidak dibuat")
	}
	if bastWriter.hppCreated {
		t.Error("Event 4: HPP journal seharusnya tidak ada saat HPP = 0")
	}
	if !taxAccruer.called {
		t.Error("Event 5: PPh Final harus tetap dipanggil meski HPP = 0")
	}
}

// ── Sprint 1 QA #3: Rollback saat PPh Final gagal ────────────────────────────

// TestBASTExecute_PPh_Fails_RollsBackAll (Sprint 1 QA #3):
// Jika PPh Final accrual gagal, SEMUA yang sudah diproses harus di-rollback:
//   - sale_record tidak tersimpan
//   - revenue journal tidak tersimpan
//   - HPP journal tidak tersimpan
//   - tax obligation tidak dibuat
func TestBASTExecute_PPh_Fails_RollsBackAll(t *testing.T) {
	taxErr := errors.New("koneksi DB terputus saat akrual PPh Final")
	taxAccruer := &mockPPhFinalAccruer{err: taxErr}
	bastWriter := &bastWriterWithPPh{taxAccruer: taxAccruer}

	hpp := domain.UnitCostBreakdown{Hard: rupiah(500_000_000)}
	svc := buildServicePPh(
		standardAccounts,
		map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil,
		map[uint64]domain.UnitCostBreakdown{5: hpp},
		bastWriter,
	)

	req := sale.RecordBASTRequest{
		UnitID:    5,
		SalePrice: rupiah(1_000_000_000),
		BASTDate:  time.Now(),
	}
	rec, err := svc.RecordAkad(context.Background(), 1, req)

	// Harus mengembalikan error
	if err == nil {
		t.Fatal("diharapkan error saat PPh Final gagal, tapi RecordBAST sukses")
	}
	// SaleRecord harus nil (tidak tersimpan)
	if rec != nil {
		t.Error("sale_record seharusnya nil — rollback karena PPh Final gagal")
	}

	// Revenue journal: harus di-rollback
	if bastWriter.revenueCreated {
		t.Error("revenue journal seharusnya di-rollback saat PPh Final gagal")
	}
	// HPP journal: harus di-rollback
	if bastWriter.hppCreated {
		t.Error("HPP journal seharusnya di-rollback saat PPh Final gagal")
	}
	// SaleRecord: tidak boleh tersimpan
	if bastWriter.saleRecordSaved {
		t.Error("sale_record seharusnya tidak tersimpan — rollback karena PPh Final gagal")
	}
	// PPh Final HARUS dipanggil (konfirmasi ini adalah titik kegagalannya)
	if !taxAccruer.called {
		t.Error("PPh Final accruer seharusnya dipanggil sebelum gagal")
	}
}

// TestBASTExecute_PPh_Fails_ErrorPropagates:
// Error dari PPh Final harus dibungkus dan dapat di-unwrap dari error RecordBAST.
func TestBASTExecute_PPh_Fails_ErrorPropagates(t *testing.T) {
	taxErr := errors.New("pajak gagal")
	taxAccruer := &mockPPhFinalAccruer{err: taxErr}
	bastWriter := &bastWriterWithPPh{taxAccruer: taxAccruer}

	svc := buildServicePPh(
		standardAccounts,
		map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil,
		map[uint64]domain.UnitCostBreakdown{5: {Hard: rupiah(100_000_000)}},
		bastWriter,
	)

	_, err := svc.RecordAkad(context.Background(), 1, sale.RecordBASTRequest{
		UnitID: 5, SalePrice: rupiah(500_000_000), BASTDate: time.Now(),
	})
	if !errors.Is(err, taxErr) {
		t.Errorf("expected taxErr to be in error chain, got %v", err)
	}
}
