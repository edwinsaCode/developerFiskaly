package fixedasset

// Master Kategori Aset Tetap — pola identik dengan cost.ExpenseType (W-10) dan
// charge.RealizationChargeType (W-1): fail-closed resolver, mapping akun
// divalidasi SEBELUM disimpan, setiap perubahan tercatat append-only di
// master_data_changes (tabel bersama, dibuat W-1 migrasi 000061).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// CategoryPolicy adalah hasil resolve master yang sudah tervalidasi. Service
// hanya pernah melihat bentuk ini — tidak pernah baris master mentah.
type CategoryPolicy struct {
	ID                                 uint64
	Code                               string
	Name                               string
	AssetAccountCode                   string
	AccumulatedDepreciationAccountCode string
	DepreciationExpenseAccountCode     string
}

// CategoryResolver menjawab kebijakan sebuah kategori aset tetap. FAIL-CLOSED:
// kategori tak dikenal, nonaktif, milik tenant lain, atau error DB semuanya
// mengembalikan error sehingga transaksi batal SEBELUM ada jurnal.
type CategoryResolver interface {
	ResolveCategory(ctx context.Context, tenantID, id uint64) (CategoryPolicy, error)
}

// masterDataChange — audit APPEND-ONLY (tabel bersama W-1 migrasi 000061).
type masterDataChange struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"`
	TenantID   uint64    `gorm:"not null;index"`
	Entity     string    `gorm:"not null;size:64"`
	EntityCode string    `gorm:"not null;size:64"`
	Field      string    `gorm:"not null;size:64"`
	OldValue   string    `gorm:"not null;size:255"`
	NewValue   string    `gorm:"not null;size:255"`
	ChangedBy  *uint64   ``
	CreatedAt  time.Time ``
	UpdatedAt  time.Time ``
}

func (masterDataChange) TableName() string { return "master_data_changes" }

// EntityFixedAssetCategory adalah nilai `entity` untuk audit master di atas.
const EntityFixedAssetCategory = "fixed_asset_category"

func normalizeCategoryCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// ── Resolver KANONIK ─────────────────────────────────────────────────────────

func (r *GORMRepository) ResolveCategory(ctx context.Context, tenantID, id uint64) (CategoryPolicy, error) {
	if id == 0 {
		return CategoryPolicy{}, fmt.Errorf("%w: id kosong", ErrCategoryNotFound)
	}
	var c Category
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CategoryPolicy{}, fmt.Errorf("%w: id %d", ErrCategoryNotFound, id)
	}
	if err != nil {
		return CategoryPolicy{}, fmt.Errorf("resolve kategori aset tetap %d: %w", id, err)
	}
	if !c.IsActive {
		return CategoryPolicy{}, fmt.Errorf("%w: %q", ErrCategoryInactive, c.Code)
	}
	return CategoryPolicy{
		ID:                                 c.ID,
		Code:                               c.Code,
		Name:                               c.Name,
		AssetAccountCode:                   c.AssetAccountCode,
		AccumulatedDepreciationAccountCode: c.AccumulatedDepreciationAccountCode,
		DepreciationExpenseAccountCode:     c.DepreciationExpenseAccountCode,
	}, nil
}

// ── Validasi akun (pola cost.validateExpenseAccountDB) ──────────────────────

// validateAccountDB memastikan sebuah kode akun ada di COA tenant, aktif, dan
// cocok dengan (tipe, normal balance) yang diharapkan perannya. Dipakai untuk
// ketiga peran akun kategori — kontra-aset dibedakan HANYA lewat NormalBalance
// (tidak ada flag IsContra terpisah di ledger.Account; 1-4900 sendiri
// Type=Asset+NormalBalance=Credit).
func validateAccountDB(ctx context.Context, db *gorm.DB, tenantID uint64, code string, wantType domain.AccountType, wantNormal domain.NormalBalance, invalidErr error) error {
	c := strings.TrimSpace(code)
	if c == "" {
		return fmt.Errorf("%w: kode akun kosong", invalidErr)
	}
	var f struct {
		Type          string `gorm:"column:type"`
		NormalBalance string `gorm:"column:normal_balance"`
		IsActive      bool   `gorm:"column:is_active"`
	}
	err := db.WithContext(ctx).
		Table("accounts").Select("type, normal_balance, is_active").
		Where("tenant_id = ? AND code = ?", tenantID, c).
		Take(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: akun %q tidak ada di COA", invalidErr, c)
	}
	if err != nil {
		return fmt.Errorf("validasi akun %q: %w", c, err)
	}
	if !f.IsActive {
		return fmt.Errorf("%w: akun %q nonaktif", invalidErr, c)
	}
	if domain.AccountType(f.Type) != wantType {
		return fmt.Errorf("%w: akun %q bertipe %s, bukan %s", invalidErr, c, f.Type, wantType)
	}
	if domain.NormalBalance(f.NormalBalance) != wantNormal {
		return fmt.Errorf("%w: akun %q normal balance %s, bukan %s", invalidErr, c, f.NormalBalance, wantNormal)
	}
	return nil
}

func validateCategoryAccounts(ctx context.Context, db *gorm.DB, tenantID uint64, assetAcc, accumDepAcc, depExpAcc string) error {
	if err := validateAccountDB(ctx, db, tenantID, assetAcc, domain.AccountAsset, domain.NormalBalanceDebit, ErrAssetAccountInvalid); err != nil {
		return err
	}
	if err := validateAccountDB(ctx, db, tenantID, accumDepAcc, domain.AccountAsset, domain.NormalBalanceCredit, ErrAccumulatedDepreciationAccountInvalid); err != nil {
		return err
	}
	if err := validateAccountDB(ctx, db, tenantID, depExpAcc, domain.AccountExpense, domain.NormalBalanceDebit, ErrDepreciationExpenseAccountInvalid); err != nil {
		return err
	}
	return nil
}

// ── CategoryService — CRUD admin ─────────────────────────────────────────────

type CategoryService struct{ db *gorm.DB }

func NewCategoryService(db *gorm.DB) *CategoryService { return &CategoryService{db: db} }

func (s *CategoryService) List(ctx context.Context, tenantID uint64) ([]*Category, error) {
	var out []*Category
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("is_active DESC, code").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListCategories: %w", err)
	}
	return out, nil
}

type CategoryInput struct {
	Code                               string
	Name                               string
	AssetAccountCode                   string
	AccumulatedDepreciationAccountCode string
	DepreciationExpenseAccountCode     string
	DefaultUsefulLifeMonths            uint
}

func (s *CategoryService) Create(ctx context.Context, tenantID uint64, in CategoryInput, actor *uint64) (*Category, error) {
	code := normalizeCategoryCode(in.Code)
	name := strings.TrimSpace(in.Name)
	assetAcc := strings.TrimSpace(in.AssetAccountCode)
	accumAcc := strings.TrimSpace(in.AccumulatedDepreciationAccountCode)
	expAcc := strings.TrimSpace(in.DepreciationExpenseAccountCode)
	if code == "" || name == "" {
		return nil, ErrCategoryInvalid
	}
	if err := validateCategoryAccounts(ctx, s.db, tenantID, assetAcc, accumAcc, expAcc); err != nil {
		return nil, err
	}
	c := &Category{
		TenantID: tenantID, Code: code, Name: name,
		AssetAccountCode: assetAcc, AccumulatedDepreciationAccountCode: accumAcc,
		DepreciationExpenseAccountCode: expAcc, DefaultUsefulLifeMonths: in.DefaultUsefulLifeMonths,
		IsActive: true,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(c).Error; err != nil {
			if isDuplicateCategoryCode(err) {
				return ErrCategoryDuplicate
			}
			return fmt.Errorf("simpan kategori aset tetap: %w", err)
		}
		return appendCategoryMasterChange(ctx, tx, tenantID, code, "created", "", assetAcc+"/"+accumAcc+"/"+expAcc, actor)
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

type CategoryUpdate struct {
	Name                               *string
	AssetAccountCode                   *string
	AccumulatedDepreciationAccountCode *string
	DepreciationExpenseAccountCode     *string
	DefaultUsefulLifeMonths            *uint
	IsActive                           *bool
}

func (s *CategoryService) Update(ctx context.Context, tenantID, id uint64, upd CategoryUpdate, actor *uint64) (*Category, error) {
	var out Category
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c Category
		if err := tx.WithContext(ctx).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			First(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCategoryNotFound
			}
			return err
		}
		updates := map[string]interface{}{}
		type change struct{ field, old, nw string }
		var changes []change

		if upd.Name != nil && strings.TrimSpace(*upd.Name) != "" && strings.TrimSpace(*upd.Name) != c.Name {
			n := strings.TrimSpace(*upd.Name)
			updates["name"] = n
			changes = append(changes, change{"name", c.Name, n})
		}
		newAssetAcc, newAccumAcc, newExpAcc := c.AssetAccountCode, c.AccumulatedDepreciationAccountCode, c.DepreciationExpenseAccountCode
		accountsChanged := false
		if upd.AssetAccountCode != nil && strings.TrimSpace(*upd.AssetAccountCode) != "" && strings.TrimSpace(*upd.AssetAccountCode) != c.AssetAccountCode {
			newAssetAcc = strings.TrimSpace(*upd.AssetAccountCode)
			accountsChanged = true
		}
		if upd.AccumulatedDepreciationAccountCode != nil && strings.TrimSpace(*upd.AccumulatedDepreciationAccountCode) != "" && strings.TrimSpace(*upd.AccumulatedDepreciationAccountCode) != c.AccumulatedDepreciationAccountCode {
			newAccumAcc = strings.TrimSpace(*upd.AccumulatedDepreciationAccountCode)
			accountsChanged = true
		}
		if upd.DepreciationExpenseAccountCode != nil && strings.TrimSpace(*upd.DepreciationExpenseAccountCode) != "" && strings.TrimSpace(*upd.DepreciationExpenseAccountCode) != c.DepreciationExpenseAccountCode {
			newExpAcc = strings.TrimSpace(*upd.DepreciationExpenseAccountCode)
			accountsChanged = true
		}
		if accountsChanged {
			if err := validateCategoryAccounts(ctx, tx, tenantID, newAssetAcc, newAccumAcc, newExpAcc); err != nil {
				return err
			}
			updates["asset_account_code"] = newAssetAcc
			updates["accumulated_depreciation_account_code"] = newAccumAcc
			updates["depreciation_expense_account_code"] = newExpAcc
			changes = append(changes, change{"accounts",
				c.AssetAccountCode + "/" + c.AccumulatedDepreciationAccountCode + "/" + c.DepreciationExpenseAccountCode,
				newAssetAcc + "/" + newAccumAcc + "/" + newExpAcc})
		}
		if upd.DefaultUsefulLifeMonths != nil && *upd.DefaultUsefulLifeMonths != c.DefaultUsefulLifeMonths {
			updates["default_useful_life_months"] = *upd.DefaultUsefulLifeMonths
			changes = append(changes, change{"default_useful_life_months", fmt.Sprint(c.DefaultUsefulLifeMonths), fmt.Sprint(*upd.DefaultUsefulLifeMonths)})
		}
		if upd.IsActive != nil && *upd.IsActive != c.IsActive {
			updates["is_active"] = *upd.IsActive
			changes = append(changes, change{"is_active", boolText(c.IsActive), boolText(*upd.IsActive)})
		}
		if len(updates) == 0 {
			out = c
			return nil
		}
		if err := tx.WithContext(ctx).Model(&Category{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("ubah kategori aset tetap: %w", err)
		}
		for _, ch := range changes {
			if err := appendCategoryMasterChange(ctx, tx, tenantID, c.Code, ch.field, ch.old, ch.nw, actor); err != nil {
				return err
			}
		}
		return tx.WithContext(ctx).
			Where("id = ? AND tenant_id = ?", id, tenantID).First(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func appendCategoryMasterChange(ctx context.Context, tx *gorm.DB, tenantID uint64, code, field, oldV, newV string, actor *uint64) error {
	row := &masterDataChange{
		TenantID: tenantID, Entity: EntityFixedAssetCategory, EntityCode: code,
		Field: field, OldValue: oldV, NewValue: newV, ChangedBy: actor,
	}
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("audit master data: %w", err)
	}
	return nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func isDuplicateCategoryCode(err error) bool {
	return err != nil && strings.Contains(err.Error(), "1062")
}

// ── Seed ─────────────────────────────────────────────────────────────────────

// defaultCategories: cermin seed migration 000086. Hanya memetakan ke akun
// yang sudah ada di COA bawaan (ledger/coa.go).
var defaultCategories = []Category{
	{Code: "peralatan-kantor", Name: "Peralatan Kantor", AssetAccountCode: "1-4000", AccumulatedDepreciationAccountCode: "1-4900", DepreciationExpenseAccountCode: "5-4500", DefaultUsefulLifeMonths: 48},
	{Code: "kendaraan", Name: "Kendaraan", AssetAccountCode: "1-4100", AccumulatedDepreciationAccountCode: "1-4900", DepreciationExpenseAccountCode: "5-4500", DefaultUsefulLifeMonths: 96},
}

// SeedDefaultCategories menyemai master untuk tenant BARU (idempoten).
func SeedDefaultCategories(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	for _, c := range defaultCategories {
		row := c
		row.TenantID = tenantID
		row.IsActive = true
		if err := db.WithContext(ctx).
			Where("tenant_id = ? AND code = ?", tenantID, row.Code).
			FirstOrCreate(&row).Error; err != nil {
			return fmt.Errorf("seed kategori aset tetap %s: %w", row.Code, err)
		}
	}
	return nil
}
