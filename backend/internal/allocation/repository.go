package allocation

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// GORMRepository mengimplementasi ConfigStore, DirectCostProvider, dan UnitAttributeSource.
type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ── ConfigStore implementation ────────────────────────────────────────────────

func (r *GORMRepository) GetConfig(ctx context.Context, tenantID, projectID uint64) (*AllocationConfig, error) {
	var cfg AllocationConfig
	err := r.db.WithContext(ctx).
		First(&cfg, "tenant_id = ? AND project_id = ?", tenantID, projectID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConfigNotFound
		}
		return nil, fmt.Errorf("GetConfig: %w", err)
	}
	return &cfg, nil
}

func (r *GORMRepository) SaveConfig(ctx context.Context, cfg *AllocationConfig) error {
	var err error
	if cfg.ID == 0 {
		err = r.db.WithContext(ctx).Create(cfg).Error
	} else {
		err = r.db.WithContext(ctx).Save(cfg).Error
	}
	if err != nil {
		return fmt.Errorf("SaveConfig: %w", err)
	}
	return nil
}

// ── ExecutionStore implementation ─────────────────────────────────────────────

func (r *GORMRepository) SaveExecution(ctx context.Context, e *AllocationExecution) error {
	if err := r.db.WithContext(ctx).Create(e).Error; err != nil {
		return fmt.Errorf("SaveExecution: %w", err)
	}
	return nil
}

func (r *GORMRepository) ListExecutions(ctx context.Context, tenantID, projectID uint64) ([]*AllocationExecution, error) {
	var execs []*AllocationExecution
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Order("executed_at DESC").
		Find(&execs).Error
	if err != nil {
		return nil, fmt.Errorf("ListExecutions: %w", err)
	}
	return execs, nil
}

// ── UserEmailFinder implementation ────────────────────────────────────────────

func (r *GORMRepository) FindUserEmail(ctx context.Context, tenantID, userID uint64) (string, error) {
	var email string
	err := r.db.WithContext(ctx).
		Table("users").
		Select("email").
		Where("tenant_id = ? AND id = ?", tenantID, userID).
		Scan(&email).Error
	if err != nil {
		return "", fmt.Errorf("FindUserEmail: %w", err)
	}
	return email, nil
}

// ── DirectCostProvider implementation ────────────────────────────────────────
//
// Sumber kebenaran = journal_lines yang sudah diposting (Invariant #4).
// Account codes 1-3000/3100/3200/3300 = Persediaan Real Estat per kategori.

// SATU definisi via AccountRoleRegistry (S1) — dulu kembar dgn cost/repository.go.
var inventoryCodes = ledger.RoleCodeList(ledger.RoleInventory)

// GetProjectWideCosts mengambil biaya project-wide (unit_id IS NULL) dari jurnal.
func (r *GORMRepository) GetProjectWideCosts(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error) {
	return r.queryCosts(ctx, tenantID, projectID, "project-wide", 0)
}

// GetUnitDirectCosts mengambil biaya yang sudah ber-tag unit_id dari jurnal.
func (r *GORMRepository) GetUnitDirectCosts(ctx context.Context, tenantID, unitID uint64) (domain.UnitCostBreakdown, error) {
	return r.queryCosts(ctx, tenantID, 0, "unit", unitID)
}

// queryCosts (S5): delegasi ke pembaca KANONIK ledger.ActualCostByCode —
// netting tunggal (debit non-reversal − kredit reversal), bukan debit-only.
func (r *GORMRepository) queryCosts(ctx context.Context, tenantID, projectID uint64, filter string, unitID uint64) (domain.UnitCostBreakdown, error) {
	var sc ledger.ActualCostScope
	switch filter {
	case "project-wide":
		sc.ProjectID = projectID
		sc.ProjectWideOnly = true
	case "unit":
		sc.UnitID = &unitID
	}
	byCode, err := ledger.NewQueryService(r.db).ActualCostByCode(ctx, tenantID, inventoryCodes, sc)
	if err != nil {
		return domain.UnitCostBreakdown{}, fmt.Errorf("queryCosts(%s): %w", filter, err)
	}
	// AllCostCategories hanya land|hard (RULE KLIEN FREEZE 2026-09-04) — b.Soft
	// tidak pernah diisi dari sini (field itu legacy-only untuk baca data historis).
	var b domain.UnitCostBreakdown
	for _, c := range domain.AllCostCategories {
		if m, ok := byCode[c.InventoryAccountCode()]; ok {
			switch c {
			case domain.CostCategoryLand:
				b.Land = m
			case domain.CostCategoryHard:
				b.Hard = m
			}
		}
	}
	return b, nil
}

// ── UnitAttributeSource implementation ───────────────────────────────────────
//
// unitRow adalah proyeksi kolom yang diperlukan dari tabel units.
type unitRow struct {
	ID           uint64          `gorm:"column:id"`
	Code         string          `gorm:"column:code"`
	UnitType     string          `gorm:"column:unit_type"`
	Category     *string         `gorm:"column:category"` // NULL = unit_type tak terdaftar di katalog
	SaleableArea decimal.Decimal `gorm:"column:saleable_area"`
	ListPrice    domain.Money    `gorm:"column:list_price"`
	LandAreaM2   decimal.Decimal `gorm:"column:land_area"`
}

// GetUnitInputs mengambil unit sebuah proyek YANG IKUT HPP beserta area dan
// list_price. Direct cost per unit di-fetch terpisah dan di-merge di sini.
//
// H-1 — basis alokasi HPP hanya berisi produk properti:
// Unit non-properti (kelebihan tanah, sambungan PDAM, …) punya saleable_area /
// list_price > 0, sehingga bila ikut masuk basis mereka MENYEDOT bobot dan
// mendilusi HPP rumah — baik di alokasi biaya aktual maupun di snapshot
// budgeted saat BAST. Keputusan ikut/tidak diambil HANYA oleh fungsi kanonik
// domain.ProductCategory.ParticipatesInHPP(); SQL di sini cuma mengambil
// kategori mentah, tidak menafsirkan aturannya.
//
// FAIL-CLOSED: unit yang unit_type-nya tidak ada di master katalog TIDAK
// diam-diam dianggap properti maupun diam-diam dibuang — ia menggagalkan
// perhitungan dengan error yang jelas (migration 000058 sudah mendaftarkan
// seluruh unit_type historis, jadi ini hanya terjadi bila master dirusak).
func (r *GORMRepository) GetUnitInputs(ctx context.Context, tenantID, projectID uint64) ([]UnitInput, error) {
	var rows []unitRow
	err := r.db.WithContext(ctx).
		Table("units u").
		Select("u.id, u.code, u.unit_type, pt.category, u.saleable_area, u.list_price, u.land_area").
		Joins("LEFT JOIN product_types pt ON pt.tenant_id = u.tenant_id AND pt.code = u.unit_type").
		Where("u.tenant_id = ? AND u.project_id = ?", tenantID, projectID).
		Order("u.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("GetUnitInputs: %w", err)
	}

	inputs := make([]UnitInput, 0, len(rows))
	for _, row := range rows {
		if row.Category == nil {
			return nil, fmt.Errorf("%w: unit %s (id=%d) bertipe %q", ErrUnitTypeUnregistered, row.Code, row.ID, row.UnitType)
		}
		if !domain.ProductCategory(*row.Category).ParticipatesInHPP() {
			continue // non-properti: tidak pernah menjadi penerima alokasi HPP
		}
		direct, err := r.GetUnitDirectCosts(ctx, tenantID, row.ID)
		if err != nil {
			return nil, fmt.Errorf("GetUnitDirectCosts(unit=%d): %w", row.ID, err)
		}
		inputs = append(inputs, UnitInput{
			UnitID:       row.ID,
			SaleableArea: row.SaleableArea,
			SalesValue:   row.ListPrice,
			LandAreaM2:   row.LandAreaM2,
			Direct:       direct,
		})
	}
	return inputs, nil
}

// ── LandPoolSource implementation (LT-5) ────────────────────────────────────

// landUnitRow adalah proyeksi kolom land_area dari units, dengan filter
// partisipasi HPP yang SAMA dengan GetUnitInputs (H-1).
type landUnitRow struct {
	ID         uint64          `gorm:"column:id"`
	Code       string          `gorm:"column:code"`
	UnitType   string          `gorm:"column:unit_type"`
	Category   *string         `gorm:"column:category"`
	LandAreaM2 decimal.Decimal `gorm:"column:land_area"`
}

// GetLandPoolParticipants mengambil peserta pool biaya Land berbasis
// land_area untuk sebuah proyek: setiap unit properti (filter identik dengan
// GetUnitInputs — H-1, fail-closed pada unit_type tak terdaftar) PLUS satu
// peserta land_stock (jika proyek punya pool Kelebihan Tanah, LT-3).
//
// Tidak mengambil direct cost — pool Land dialokasikan sebagai satu kesatuan,
// tidak ada "direct Land cost per unit" yang relevan di sini (berbeda dari
// GetUnitInputs yang dipakai Compute() untuk SEMUA kategori biaya).
func (r *GORMRepository) GetLandPoolParticipants(ctx context.Context, tenantID, projectID uint64) ([]LandPoolParticipant, error) {
	var rows []landUnitRow
	err := r.db.WithContext(ctx).
		Table("units u").
		Select("u.id, u.code, u.unit_type, pt.category, u.land_area").
		Joins("LEFT JOIN product_types pt ON pt.tenant_id = u.tenant_id AND pt.code = u.unit_type").
		Where("u.tenant_id = ? AND u.project_id = ?", tenantID, projectID).
		Order("u.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("GetLandPoolParticipants: %w", err)
	}

	participants := make([]LandPoolParticipant, 0, len(rows)+1)
	for _, row := range rows {
		if row.Category == nil {
			return nil, fmt.Errorf("%w: unit %s (id=%d) bertipe %q", ErrUnitTypeUnregistered, row.Code, row.ID, row.UnitType)
		}
		if !domain.ProductCategory(*row.Category).ParticipatesInHPP() {
			continue
		}
		participants = append(participants, LandPoolParticipant{
			Kind:       LandPoolParticipantUnit,
			UnitID:     row.ID,
			LandAreaM2: row.LandAreaM2,
		})
	}

	var stock struct {
		ID              uint64          `gorm:"column:id"`
		TotalQuantityM2 decimal.Decimal `gorm:"column:total_quantity_m2"`
		PurchasePrice   domain.Money    `gorm:"column:purchase_price"`
	}
	err = r.db.WithContext(ctx).
		Table("land_stock").
		Select("id, total_quantity_m2, purchase_price").
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Take(&stock).Error
	switch {
	case err == nil:
		participants = append(participants, LandPoolParticipant{
			Kind:               LandPoolParticipantLandStock,
			LandStockID:        stock.ID,
			LandAreaM2:         stock.TotalQuantityM2,
			PurchasePricePerM2: stock.PurchasePrice,
		})
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Proyek tanpa land_stock: tidak opt-in Kelebihan Tanah — tidak ada
		// peserta land_stock, applyLandPoolOverride akan NO-OP.
	default:
		return nil, fmt.Errorf("GetLandPoolParticipants: baca land_stock: %w", err)
	}

	return participants, nil
}

// ── HardPoolSource / UnitTaxCategorySource implementation (UAT 2026-09-07) ──

// hardSubpoolRow adalah proyeksi hasil GROUP BY hard_subcategory dari
// cost_entries — biaya Konstruksi (Hard) project-wide (tier=shared,
// unit_id IS NULL) yang SUDAH ter-posting dan belum dibatalkan.
type hardSubpoolRow struct {
	HardSubcategory *string      `gorm:"column:hard_subcategory"`
	Total           domain.Money `gorm:"column:total"`
}

// GetHardSubpools memecah biaya Konstruksi project-wide sebuah proyek menjadi
// 3 pool (ProduksiSubsidi/ProduksiKomersial/General) berdasarkan
// cost_entries.hard_subcategory (migration 000103).
//
// Sumber kebenaran = cost_entries, BUKAN journal_lines seperti queryCosts —
// hard_subcategory adalah atribut transaksi (cost_entries), bukan atribut
// akun, sehingga tidak bisa diturunkan dari kode akun Persediaan generik.
// Filter posted (je.posted_at IS NOT NULL) dan belum dibatalkan (LEFT JOIN
// rev ... rev.id IS NULL) mereplikasi pola exact yang sama dengan
// expenseStatusOf di internal/cost/expense_query.go — satu cost_entry yang
// jurnalnya sudah dibalik tidak lagi dihitung sebagai biaya aktual (reversing
// journal menetralkan penuh, jadi exclude == netting ke nol, Invariant #5).
// unit_id IS NULL + cost_tier=shared menyaring HANYA baris "Produksi
// Unit/Blok" project-wide yang memang melalui allocation engine — baris
// tier=direct (sudah ber-unit_id) sudah 1:1 ke unit lewat GetUnitDirectCosts,
// tidak boleh dobel dihitung di sini (Invariant #3).
func (r *GORMRepository) GetHardSubpools(ctx context.Context, tenantID, projectID uint64) (HardSubpools, error) {
	var rows []hardSubpoolRow
	err := r.db.WithContext(ctx).
		Table("cost_entries ce").
		Select("ce.hard_subcategory AS hard_subcategory, SUM(ce.amount) AS total").
		Joins("JOIN journal_entries je ON je.id = ce.journal_entry_id AND je.tenant_id = ce.tenant_id").
		Joins("LEFT JOIN journal_entries rev ON rev.reverses_id = je.id AND rev.tenant_id = ce.tenant_id AND rev.posted_at IS NOT NULL").
		Where("ce.tenant_id = ? AND ce.project_id = ? AND ce.category = ? AND ce.cost_tier = ? AND ce.unit_id IS NULL",
			tenantID, projectID, domain.CostCategoryHard, domain.CostTierShared).
		Where("je.posted_at IS NOT NULL AND rev.id IS NULL").
		Group("ce.hard_subcategory").
		Scan(&rows).Error
	if err != nil {
		return HardSubpools{}, fmt.Errorf("GetHardSubpools: %w", err)
	}

	var pools HardSubpools
	for _, row := range rows {
		var sub domain.ConstructionSubcategory
		if row.HardSubcategory != nil {
			sub = domain.ConstructionSubcategory(*row.HardSubcategory)
		}
		switch sub {
		case domain.ConstructionProduksiSubsidi:
			pools.ProduksiSubsidi = row.Total
		case domain.ConstructionProduksiKomersial:
			pools.ProduksiKomersial = row.Total
		default:
			// Sarana & Prasarana, Perizinan, NULL (baris legacy pra-migration
			// 000103) — semuanya digabung General, perilaku identik dengan
			// sebelum fitur ini.
			pools.General = pools.General.Add(row.Total)
		}
	}
	return pools, nil
}

// unitTaxRow adalah proyeksi TaxCategory efektif mentah (belum diresolusi)
// untuk satu unit: TaxCategory product_type (override, boleh NULL) dan
// TaxCategory proyek (default, boleh NULL/kosong pada proyek legacy).
type unitTaxRow struct {
	UnitID            uint64  `gorm:"column:id"`
	ProductTypeTaxCat *string `gorm:"column:pt_tax_category"`
	ProjectTaxCat     *string `gorm:"column:project_tax_category"`
}

// GetUnitTaxCategories mengembalikan TaxCategory efektif setiap unit dalam
// sebuah proyek, mereplikasi algoritma resolusi tax.Service persis
// (internal/tax/service.go: product_type.tax_category MENANG atas
// projects.tax_category bila diset) via raw SQL — TIDAK mengimpor package
// tax/project untuk menghindari import cycle (allocation di-import tax? —
// tidak, tapi project meng-import allocation, jadi arah sebaliknya juga harus
// dihindari untuk konsistensi arsitektur).
//
// Unit yang keduanya (product_type dan project) tidak punya TaxCategory valid
// TIDAK dimasukkan ke map — ComputeHardPool memperlakukan unit yang tidak ada
// di map sebagai "tidak ikut pool Subsidi maupun Komersial" (hanya General).
func (r *GORMRepository) GetUnitTaxCategories(ctx context.Context, tenantID, projectID uint64) (map[uint64]domain.TaxCategory, error) {
	var rows []unitTaxRow
	err := r.db.WithContext(ctx).
		Table("units u").
		Select("u.id, pt.tax_category AS pt_tax_category, p.tax_category AS project_tax_category").
		Joins("LEFT JOIN product_types pt ON pt.tenant_id = u.tenant_id AND pt.code = u.unit_type").
		Joins("JOIN projects p ON p.tenant_id = u.tenant_id AND p.id = u.project_id").
		Where("u.tenant_id = ? AND u.project_id = ?", tenantID, projectID).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("GetUnitTaxCategories: %w", err)
	}

	out := make(map[uint64]domain.TaxCategory, len(rows))
	for _, row := range rows {
		cat := domain.TaxCategory("")
		if row.ProjectTaxCat != nil {
			cat = domain.TaxCategory(*row.ProjectTaxCat)
		}
		if row.ProductTypeTaxCat != nil && *row.ProductTypeTaxCat != "" {
			cat = domain.TaxCategory(*row.ProductTypeTaxCat)
		}
		if cat.Valid() {
			out[row.UnitID] = cat
		}
	}
	return out, nil
}
