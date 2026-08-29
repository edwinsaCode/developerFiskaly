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
	var b domain.UnitCostBreakdown
	for _, c := range domain.AllCostCategories {
		if m, ok := byCode[c.InventoryAccountCode()]; ok {
			switch c {
			case domain.CostCategoryLand:
				b.Land = m
			case domain.CostCategoryHard:
				b.Hard = m
			case domain.CostCategorySoft:
				b.Soft = m
			case domain.CostCategoryFinancing:
				b.Financing = m
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
		Select("u.id, u.code, u.unit_type, pt.category, u.saleable_area, u.list_price").
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
	}
	err = r.db.WithContext(ctx).
		Table("land_stock").
		Select("id, total_quantity_m2").
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Take(&stock).Error
	switch {
	case err == nil:
		participants = append(participants, LandPoolParticipant{
			Kind:        LandPoolParticipantLandStock,
			LandStockID: stock.ID,
			LandAreaM2:  stock.TotalQuantityM2,
		})
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Proyek tanpa land_stock: tidak opt-in Kelebihan Tanah — tidak ada
		// peserta land_stock, applyLandPoolOverride akan NO-OP.
	default:
		return nil, fmt.Errorf("GetLandPoolParticipants: baca land_stock: %w", err)
	}

	return participants, nil
}
