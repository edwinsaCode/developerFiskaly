package allocation

import (
	"context"
	"fmt"
	"time"

	"esaproperti/internal/domain"
)

// ConfigStore mengelola konfigurasi basis alokasi per proyek.
type ConfigStore interface {
	GetConfig(ctx context.Context, tenantID, projectID uint64) (*AllocationConfig, error)
	SaveConfig(ctx context.Context, cfg *AllocationConfig) error
}

// DirectCostProvider menyediakan biaya terakumulasi dari jurnal.
// Implementasi: GORMRepository.GetProjectWideCosts dan GetUnitDirectCosts
// yang query journal_lines (sumber kebenaran — lihat CLAUDE.md invariant #4).
type DirectCostProvider interface {
	GetProjectWideCosts(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error)
	GetUnitDirectCosts(ctx context.Context, tenantID, unitID uint64) (domain.UnitCostBreakdown, error)
}

// UnitAttributeSource menyediakan atribut unit (area, list_price) yang
// dibutuhkan sebagai bobot dalam Compute.
type UnitAttributeSource interface {
	GetUnitInputs(ctx context.Context, tenantID, projectID uint64) ([]UnitInput, error)
}

// ExecutionStore menyimpan dan membaca riwayat eksekusi alokasi.
type ExecutionStore interface {
	SaveExecution(ctx context.Context, e *AllocationExecution) error
	ListExecutions(ctx context.Context, tenantID, projectID uint64) ([]*AllocationExecution, error)
}

// UserEmailFinder mengambil email user berdasarkan ID (untuk snapshot audit).
type UserEmailFinder interface {
	FindUserEmail(ctx context.Context, tenantID, userID uint64) (string, error)
}

// Service mengorkestrasi alokasi biaya HPP.
// Tidak ada posting jurnal di sini — itu tugas Phase 6.
type Service struct {
	configs     ConfigStore
	costs       DirectCostProvider
	unitAttribs UnitAttributeSource
	executions  ExecutionStore  // opsional; diperlukan untuk Execute/ListExecutions
	userEmails  UserEmailFinder // opsional; diperlukan untuk Execute
	versions    VersionStore    // opsional (P0-4 D1); nil = SetBasis legacy
	landPool    LandPoolSource  // opsional (LT-5); nil = Land tetap saleable_area/sales_value-weighted

	// hardPool/unitTaxCategories (UAT 2026-09-07): opsional, dipasang bersama
	// via WithHardPoolSource. nil = Hard tetap satu pool diratakan ke semua
	// unit (perilaku sebelum fitur ini) — lihat applyHardPoolOverride.
	hardPool          HardPoolSource
	unitTaxCategories UnitTaxCategorySource
}

// ServiceOption adalah functional option untuk konfigurasi opsional Service.
type ServiceOption func(*Service)

func WithExecutionStore(es ExecutionStore) ServiceOption {
	return func(s *Service) { s.executions = es }
}

func WithUserEmailFinder(uf UserEmailFinder) ServiceOption {
	return func(s *Service) { s.userEmails = uf }
}

// WithLandPoolSource memasang sumber peserta pool biaya Land berbasis
// land_area (LT-5). Tanpa opsi ini, Land tetap dialokasikan seperti sebelum
// LT-5 (saleable_area/sales_value) untuk SEMUA proyek — perilaku default tidak
// berubah. Dengan opsi ini terpasang, override HANYA aktif untuk proyek yang
// benar-benar punya land_stock (opt-in Kelebihan Tanah, LT-3); lihat
// applyLandPoolOverride.
func WithLandPoolSource(lp LandPoolSource) ServiceOption {
	return func(s *Service) { s.landPool = lp }
}

// WithHardPoolSource memasang sumber pecahan biaya Konstruksi/Hard Cost
// per subkategori (Produksi Subsidi/Komersial/General) berikut sumber
// TaxCategory unit (UAT 2026-09-07). Tanpa opsi ini, ComputeAllocation tetap
// berperilaku seperti sebelum fitur ini: satu pool Hard diratakan ke semua
// unit HPP-eligible sesuai basis alokasi proyek. Dengan opsi ini terpasang,
// override HANYA memecah pool bila proyek benar-benar punya cost entry Hard
// yang sudah diklasifikasikan Subsidi/Komersial — lihat applyHardPoolOverride.
func WithHardPoolSource(hp HardPoolSource, utc UnitTaxCategorySource) ServiceOption {
	return func(s *Service) { s.hardPool = hp; s.unitTaxCategories = utc }
}

// NewService membuat Service dengan dependency yang diinjeksikan.
func NewService(configs ConfigStore, costs DirectCostProvider, unitAttribs UnitAttributeSource, opts ...ServiceOption) *Service {
	s := &Service{configs: configs, costs: costs, unitAttribs: unitAttribs}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SetBasis menetapkan atau memperbarui basis alokasi untuk sebuah proyek.
// Jika konfigurasi sudah ada, ID-nya dipertahankan (update, bukan insert baru).
//
// P0-4 D1 (dengan VersionStore terpasang): setelah snapshot BAST pertama ada,
// basis version aktif IMMUTABLE — mengganti basis membuat VERSION BARU
// (supersede, audited). Sebelum snapshot pertama: update in-place.
func (s *Service) SetBasis(ctx context.Context, tenantID, projectID uint64, basis AllocationBasis) error {
	if projectID == 0 {
		return ErrProjectRequired
	}
	if !basis.Valid() {
		return ErrInvalidBasis
	}

	existing, err := s.configs.GetConfig(ctx, tenantID, projectID)
	if err != nil && err != ErrConfigNotFound {
		return fmt.Errorf("baca konfigurasi: %w", err)
	}

	basisChanged := existing == nil || existing.Basis != basis

	// Legacy row (allocation_configs) tetap menyimpan basis aktif saat ini.
	if existing != nil {
		if basisChanged {
			existing.Basis = basis
			if err := s.configs.SaveConfig(ctx, existing); err != nil {
				return err
			}
		}
	} else {
		if err := s.configs.SaveConfig(ctx, &AllocationConfig{
			TenantID: tenantID, ProjectID: projectID, Basis: basis,
		}); err != nil {
			return err
		}
	}

	// Versioning D1 (opsional).
	if s.versions == nil {
		return nil
	}
	if !basisChanged {
		// Idempoten: pastikan version row ada (proyek pra-000029 tanpa backfill).
		_, err := s.versions.UpsertActiveVersion(ctx, tenantID, projectID, basis)
		return err
	}
	frozen, err := s.versions.HasSnapshots(ctx, tenantID, projectID)
	if err != nil {
		return err
	}
	if frozen {
		// Snapshot sudah ada → version aktif beku; ganti = version baru.
		_, err := s.versions.CreateNextVersion(ctx, tenantID, projectID, basis)
		return err
	}
	_, err = s.versions.UpsertActiveVersion(ctx, tenantID, projectID, basis)
	return err
}

// GetBasis mengambil konfigurasi basis alokasi untuk sebuah proyek.
func (s *Service) GetBasis(ctx context.Context, tenantID, projectID uint64) (*AllocationConfig, error) {
	return s.configs.GetConfig(ctx, tenantID, projectID)
}

// ComputeAllocation menghitung biaya akumulasi per unit (direct + share project-wide).
//
// Urutan:
//  1. Ambil konfigurasi basis — error jika belum diset.
//  2. Ambil biaya project-wide dari jurnal (sumber kebenaran, Invariant #4).
//  3. Ambil atribut unit (area/list_price) sekaligus direct cost per unit.
//  4. Panggil Compute — rekonsiliasi terjamin oleh Money.Allocate (Invariant #3).
func (s *Service) ComputeAllocation(ctx context.Context, tenantID, projectID uint64) ([]AllocationResult, error) {
	cfg, err := s.configs.GetConfig(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	projectWide, err := s.costs.GetProjectWideCosts(ctx, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("ambil biaya project-wide: %w", err)
	}

	units, err := s.unitAttribs.GetUnitInputs(ctx, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("ambil atribut unit: %w", err)
	}

	results, err := Compute(projectWide, units, cfg.Basis)
	if err != nil {
		return nil, err
	}
	results, err = s.applyLandPoolOverride(ctx, tenantID, projectID, projectWide.Land, results)
	if err != nil {
		return nil, err
	}
	return s.applyHardPoolOverride(ctx, tenantID, projectID, results)
}

// ComputeBudgeted mengalokasikan pool biaya TERANGGARKAN (RAB) — bukan biaya
// aktual dari jurnal — ke setiap unit berdasarkan basis proyek. Ini adalah
// jalur Budgeted Cost Allocation (docs/budgeted-cost-allocation-spec.md):
// pool berasal dari RAB aktif (budget.GetBudgetedHPPBasis), bukan journal_lines.
//
// Berbeda dari ComputeAllocation, HPP budgeted = HANYA porsi teralokasi
// (AllocationResult.Allocated) — biaya direct per-unit dari jurnal TIDAK
// dijumlahkan (menghindari double-count antara RAB dan realisasi). Rekonsiliasi
// BCA-1 (Σ Allocated per class == pool per class) dijamin Money.Allocate.
//
// Mengembalikan ErrConfigNotFound bila basis alokasi belum diatur (gate BCA-2 —
// caller sale menerjemahkannya ke ErrAllocationBasisMissing).
//
// TODO(uat-2026-09-07): jalur budgeted/RAB-estimate ini SENGAJA belum
// menerapkan applyHardPoolOverride — pool.Hard di sini berasal dari RAB
// (budget.GetBudgetedHPPBasis, satu SUM per BudgetCategory, belum per
// subkategori Produksi Subsidi/Komersial/Sarana/Perizinan). Acceptance
// criteria fitur pemisahan Subsidi/Komersial (UAT 2026-09-07) seluruhnya
// actual-cost-based (ComputeAllocation) — jalur budgeted ini dipakai untuk
// estimasi pra-BAST dan akan di-true-up oleh ComputeAllocation (actual) saat
// penjualan. Memisah Hard budgeted per subkategori adalah pekerjaan susulan
// bila klien butuh estimasi pra-BAST yang sudah presisi per Subsidi/Komersial.
func (s *Service) ComputeBudgeted(ctx context.Context, tenantID, projectID uint64, pool domain.UnitCostBreakdown) ([]AllocationResult, AllocationBasis, error) {
	cfg, err := s.configs.GetConfig(ctx, tenantID, projectID)
	if err != nil {
		return nil, "", err // ErrConfigNotFound diteruskan apa adanya
	}
	units, err := s.unitAttribs.GetUnitInputs(ctx, tenantID, projectID)
	if err != nil {
		return nil, "", fmt.Errorf("ambil atribut unit: %w", err)
	}
	results, err := Compute(pool, units, cfg.Basis)
	if err != nil {
		return nil, "", err
	}
	results, err = s.applyLandPoolOverride(ctx, tenantID, projectID, pool.Land, results)
	if err != nil {
		return nil, "", err
	}
	return results, cfg.Basis, nil
}

// applyLandPoolOverride menimpa porsi Land pada results dengan alokasi
// land_area-weighted yang menyertakan peserta land_stock (LT-5) — sehingga
// pool biaya Land dibagi antara unit properti DAN tanah kelebihan yang belum
// terjual, mencegah double-count saat land_stock itu sendiri dijual (land_sales)
// dan butuh porsi pool Land-nya sendiri.
//
// NO-OP (results dikembalikan apa adanya) kecuali DUA syarat terpenuhi:
//  1. WithLandPoolSource terpasang, DAN
//  2. proyek ini benar-benar punya land_stock (opt-in Kelebihan Tanah, LT-3).
//
// Syarat #2 krusial: units.land_area BELUM di-backfill untuk unit historis
// (LT-2, by design — lihat migration 000078). Tanpa gating ini, override
// tanpa syarat akan meng-nol-kan porsi Land HPP untuk SETIAP unit di SETIAP
// proyek system-wide begitu WithLandPoolSource dipasang — sebuah blast radius
// yang sama sekali tidak berkaitan dengan Kelebihan Tanah. Gating pada
// eksistensi land_stock membuat override hanya aktif tepat di proyek yang
// benar-benar butuh dan sudah mem-backfill land_area unit-unitnya.
func (s *Service) applyLandPoolOverride(ctx context.Context, tenantID, projectID uint64, landPoolCost domain.Money, results []AllocationResult) ([]AllocationResult, error) {
	if s.landPool == nil {
		return results, nil
	}

	participants, err := s.landPool.GetLandPoolParticipants(ctx, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("ambil peserta pool land: %w", err)
	}

	hasLandStock := false
	for _, p := range participants {
		if p.Kind == LandPoolParticipantLandStock {
			hasLandStock = true
			break
		}
	}
	if !hasLandStock {
		return results, nil
	}

	landResults, err := ComputeLandPool(landPoolCost, participants)
	if err != nil {
		return nil, err
	}
	landByUnit := make(map[uint64]domain.Money, len(landResults))
	for _, lr := range landResults {
		if lr.Kind == LandPoolParticipantUnit {
			landByUnit[lr.UnitID] = lr.Allocated
		}
	}

	overridden := make([]AllocationResult, len(results))
	for i, r := range results {
		newLand, ok := landByUnit[r.UnitID]
		if !ok {
			overridden[i] = r
			continue
		}
		r.Allocated.Land = newLand
		r.Total.Land = r.Direct.Land.Add(newLand)
		overridden[i] = r
	}
	return overridden, nil
}

// applyHardPoolOverride menimpa porsi Hard pada results dengan alokasi 3-pool
// (Produksi Subsidi/Komersial/General — UAT 2026-09-07), sehingga biaya
// Produksi Subsidi hanya membentuk HPP unit ber-TaxCategory Subsidi, dan
// sebaliknya untuk Komersial; Sarana & Prasarana/Perizinan/legacy tetap ke
// semua unit HPP-eligible.
//
// NO-OP (results dikembalikan apa adanya) bila WithHardPoolSource tidak
// terpasang — perilaku sebelum fitur ini (satu pool Hard diratakan ke semua
// unit) berlaku untuk semua proyek yang belum memakai hard_subcategory sama
// sekali. TaxCategory unit HANYA diambil bila proyek benar-benar punya cost
// entry Hard yang sudah diklasifikasikan Subsidi/Komersial — General-only
// (pool Subsidi dan Komersial sama-sama nol) mereproduksi hasil identik
// dengan Compute() tanpa perlu tahu TaxCategory unit sama sekali.
func (s *Service) applyHardPoolOverride(ctx context.Context, tenantID, projectID uint64, results []AllocationResult) ([]AllocationResult, error) {
	if s.hardPool == nil {
		return results, nil
	}

	pools, err := s.hardPool.GetHardSubpools(ctx, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("ambil hard subpools: %w", err)
	}

	var unitTax map[uint64]domain.TaxCategory
	if !pools.ProduksiSubsidi.IsZero() || !pools.ProduksiKomersial.IsZero() {
		if s.unitTaxCategories == nil {
			return nil, fmt.Errorf("ada biaya Produksi Subsidi/Komersial tapi UnitTaxCategorySource tidak terpasang")
		}
		unitTax, err = s.unitTaxCategories.GetUnitTaxCategories(ctx, tenantID, projectID)
		if err != nil {
			return nil, fmt.Errorf("ambil tax category unit: %w", err)
		}
	}

	return ComputeHardPool(pools, results, unitTax)
}

// ComputeLandStockShare mengembalikan porsi pool biaya Land yang teralokasi ke
// peserta land_stock proyek itu sendiri — pasangan dari applyLandPoolOverride,
// yang menghitung porsi ini secara internal tapi hanya mengekspos porsi per-unit.
// Ini adalah pembilang untuk hpp_rate_per_m2 = share / land_stock.total_quantity_m2
// (LT-5 §E.3/§E.4).
//
// ok=false bila WithLandPoolSource tidak terpasang, atau proyek ini tidak
// punya land_stock (override Land-pool tidak aktif untuk proyek ini) — dalam
// kedua kasus caller wajib fallback ke metode HPP lain (mis. actual).
func (s *Service) ComputeLandStockShare(ctx context.Context, tenantID, projectID uint64, landPoolCost domain.Money) (domain.Money, bool, error) {
	if s.landPool == nil {
		return domain.Money{}, false, nil
	}

	participants, err := s.landPool.GetLandPoolParticipants(ctx, tenantID, projectID)
	if err != nil {
		return domain.Money{}, false, fmt.Errorf("ambil peserta pool land: %w", err)
	}

	landResults, err := ComputeLandPool(landPoolCost, participants)
	if err != nil {
		return domain.Money{}, false, err
	}
	for _, lr := range landResults {
		if lr.Kind == LandPoolParticipantLandStock {
			return lr.Allocated, true, nil
		}
	}
	return domain.Money{}, false, nil
}

// ComputeLandStockShareActual menghitung porsi pool biaya Land AKTUAL (dari
// jurnal project-wide, bukan RAB) yang jadi hak land_stock proyek — dipakai
// LT-5 metode actual (fallback saat proyek tidak punya RAB aktif). Pola
// identik ComputeAllocation (self-fetching project-wide costs), tapi hanya
// mengembalikan porsi land_stock, bukan hasil per-unit.
func (s *Service) ComputeLandStockShareActual(ctx context.Context, tenantID, projectID uint64) (domain.Money, bool, error) {
	projectWide, err := s.costs.GetProjectWideCosts(ctx, tenantID, projectID)
	if err != nil {
		return domain.Money{}, false, fmt.Errorf("ambil biaya project-wide: %w", err)
	}
	return s.ComputeLandStockShare(ctx, tenantID, projectID, projectWide.Land)
}

// Execute menjalankan ComputeAllocation dan menyimpan audit record eksekusi.
// Tidak mengubah engine — hanya memanggil ComputeAllocation lalu menyimpan snapshot.
func (s *Service) Execute(ctx context.Context, tenantID, projectID, userID uint64) (*AllocationExecution, error) {
	if s.executions == nil {
		return nil, fmt.Errorf("execution store tidak dikonfigurasi")
	}

	// Ambil konfigurasi saat ini (untuk basis snapshot)
	cfg, err := s.configs.GetConfig(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	// Jalankan compute (tidak memodifikasi engine)
	results, err := s.ComputeAllocation(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	// Jumlahkan total HPP semua unit
	var totalCost domain.Money
	for _, r := range results {
		totalCost = totalCost.Add(r.Total.Total())
	}

	// Ambil email user untuk snapshot (graceful fallback jika finder tidak tersedia)
	email := fmt.Sprintf("user#%d", userID)
	if s.userEmails != nil {
		if found, err := s.userEmails.FindUserEmail(ctx, tenantID, userID); err == nil && found != "" {
			email = found
		}
	}

	exec := &AllocationExecution{
		TenantID:   tenantID,
		ProjectID:  projectID,
		Basis:      cfg.Basis,
		ExecutedBy: userID,
		UserEmail:  email,
		TotalCost:  totalCost,
		ExecutedAt: time.Now(),
	}

	if err := s.executions.SaveExecution(ctx, exec); err != nil {
		return nil, fmt.Errorf("simpan eksekusi: %w", err)
	}

	return exec, nil
}

// ListExecutions mengembalikan riwayat eksekusi alokasi untuk sebuah proyek, terbaru pertama.
func (s *Service) ListExecutions(ctx context.Context, tenantID, projectID uint64) ([]*AllocationExecution, error) {
	if s.executions == nil {
		return nil, fmt.Errorf("execution store tidak dikonfigurasi")
	}
	return s.executions.ListExecutions(ctx, tenantID, projectID)
}
