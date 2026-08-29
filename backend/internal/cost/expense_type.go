package cost

// W-10 — Master Jenis Pengeluaran.
//
// Sebelum W-10 akun beban sebuah biaya ditentukan enum TERTUTUP dua nilai
// (domain.CostCategory: marketing → 5-3000, other → 5-4000), sehingga seluruh
// biaya kantor — gaji, sewa, listrik, internet, transport — menggumpal di
// 5-4000 meski COA sudah menyediakan akunnya masing-masing.
//
// Master ini menjadikan pemetaannya DATA: satu jenis pengeluaran menunjuk satu
// akun beban, dan admin mengelolanya sendiri tanpa programmer. Polanya sengaja
// dijiplak dari W-1 charge.RealizationChargeType (fail-closed resolver, mapping
// akun divalidasi SEBELUM disimpan, setiap perubahan mapping tercatat di
// master_data_changes) — dua master, satu cara kerja.
//
// Master ini TIDAK menyentuh jalur biaya proyek: kapitalisasi tetap memakai
// taksonomi domain.CostCategory → Persediaan 1-3xxx.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// ExpenseType adalah master satu jenis pengeluaran operasional tenant.
type ExpenseType struct {
	ID                 uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID           uint64    `gorm:"not null;index"           json:"-"`
	Code               string    `gorm:"not null;size:50"         json:"code"`
	Name               string    `gorm:"not null;size:200"        json:"name"`
	ExpenseAccountCode string    `gorm:"not null;size:20"         json:"expense_account_code"`
	IsActive           bool      `gorm:"not null;default:true"    json:"is_active"`
	CreatedAt          time.Time `                                json:"created_at"`
	UpdatedAt          time.Time `                                json:"updated_at"`
}

func (ExpenseType) TableName() string { return "expense_types" }

// ExpenseTypePolicy adalah hasil resolve master yang sudah tervalidasi.
// Service hanya pernah melihat bentuk ini — tidak pernah baris master mentah.
type ExpenseTypePolicy struct {
	ID                 uint64
	Code               string
	Name               string
	ExpenseAccountCode string
}

// ExpenseTypeResolver menjawab kebijakan sebuah jenis pengeluaran.
// FAIL-CLOSED: jenis tak dikenal, nonaktif, milik tenant lain, mapping akun
// kosong, atau error DB semuanya mengembalikan error sehingga transaksi batal
// SEBELUM ada jurnal.
type ExpenseTypeResolver interface {
	ResolveExpenseType(ctx context.Context, tenantID, id uint64) (ExpenseTypePolicy, error)
}

// masterDataChange adalah audit APPEND-ONLY perubahan master data finansial
// (tabel bersama, dibuat W-1 migrasi 000061). Mengubah akun sebuah jenis
// pengeluaran mengubah ke baris laba rugi mana biaya perusahaan mendarat — itu
// keputusan finansial, bukan sekadar edit label.
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

// EntityExpenseType adalah nilai `entity` untuk audit master di atas.
const EntityExpenseType = "expense_type"

// normalizeExpenseTypeCode menyamakan pencocokan kode dengan collation MySQL
// (utf8mb4_unicode_ci — case-insensitive) agar lookup di Go dan pencarian di DB
// tidak pernah berbeda kesimpulan. Pola sama dengan normalizeChargeTypeCode.
func normalizeExpenseTypeCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// ── Resolver KANONIK ────────────────────────────────────────────────────────

// ResolveExpenseType mengimplementasikan ExpenseTypeResolver di atas GORM.
// Tenant-scoped: baris milik tenant lain tidak pernah terlihat, sehingga
// expense_type_id yang "dipinjam" dari tenant lain gagal resolve (bukan
// diam-diam memakai akun default).
func (r *GORMRepository) ResolveExpenseType(ctx context.Context, tenantID, id uint64) (ExpenseTypePolicy, error) {
	if id == 0 {
		return ExpenseTypePolicy{}, fmt.Errorf("%w: id kosong", ErrExpenseTypeUnknown)
	}
	var et ExpenseType
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&et).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ExpenseTypePolicy{}, fmt.Errorf("%w: id %d", ErrExpenseTypeUnknown, id)
	}
	if err != nil {
		// Error DB TIDAK boleh menjadi fallback akun — ini jalur uang.
		return ExpenseTypePolicy{}, fmt.Errorf("resolve jenis pengeluaran %d: %w", id, err)
	}
	return expenseTypePolicyOf(&et)
}

// expenseTypePolicyOf memvalidasi baris master lalu memetakannya ke value object.
func expenseTypePolicyOf(et *ExpenseType) (ExpenseTypePolicy, error) {
	if !et.IsActive {
		return ExpenseTypePolicy{}, fmt.Errorf("%w: %q", ErrExpenseTypeInactive, et.Code)
	}
	acc := strings.TrimSpace(et.ExpenseAccountCode)
	if acc == "" {
		return ExpenseTypePolicy{}, fmt.Errorf("%w: jenis %q belum punya akun beban", ErrExpenseAccountInvalid, et.Code)
	}
	return ExpenseTypePolicy{ID: et.ID, Code: et.Code, Name: et.Name, ExpenseAccountCode: acc}, nil
}

// ── Validasi akun beban (pola W-1 validateDepositAccountDB) ─────────────────

// validateExpenseAccountDB memastikan kode akun BOLEH dipakai sebagai akun
// beban: ada di COA tenant, aktif, dan bertipe expense. Memetakan jenis
// pengeluaran ke akun aset/kewajiban/pendapatan menghasilkan jurnal yang
// balanced tapi salah semantik — harus gagal saat mapping disimpan, bukan saat
// uang sudah keluar.
func validateExpenseAccountDB(ctx context.Context, db *gorm.DB, tenantID uint64, code string) error {
	c := strings.TrimSpace(code)
	if c == "" {
		return fmt.Errorf("%w: kode akun kosong", ErrExpenseAccountInvalid)
	}
	var f struct {
		Type     string `gorm:"column:type"`
		IsActive bool   `gorm:"column:is_active"`
	}
	err := db.WithContext(ctx).
		Table("accounts").Select("type, is_active").
		Where("tenant_id = ? AND code = ?", tenantID, c).
		Take(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: akun %q tidak ada di COA", ErrExpenseAccountInvalid, c)
	}
	if err != nil {
		return fmt.Errorf("validasi akun beban %q: %w", c, err)
	}
	if !f.IsActive {
		return fmt.Errorf("%w: akun %q nonaktif", ErrExpenseAccountInvalid, c)
	}
	if domain.AccountType(f.Type) != domain.AccountExpense {
		return fmt.Errorf("%w: akun %q bertipe %s — jenis pengeluaran hanya boleh dipetakan ke akun beban",
			ErrExpenseAccountInvalid, c, f.Type)
	}
	return nil
}

// ── ExpenseTypeService — CRUD admin ─────────────────────────────────────────

// ExpenseTypeService mengelola master (admin mengelola sendiri, tanpa programmer).
type ExpenseTypeService struct{ db *gorm.DB }

func NewExpenseTypeService(db *gorm.DB) *ExpenseTypeService { return &ExpenseTypeService{db: db} }

func (s *ExpenseTypeService) List(ctx context.Context, tenantID uint64) ([]*ExpenseType, error) {
	var out []*ExpenseType
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("is_active DESC, code").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListExpenseTypes: %w", err)
	}
	return out, nil
}

type ExpenseTypeInput struct {
	Code               string
	Name               string
	ExpenseAccountCode string
}

func (s *ExpenseTypeService) Create(ctx context.Context, tenantID uint64, in ExpenseTypeInput, actor *uint64) (*ExpenseType, error) {
	code := normalizeExpenseTypeCode(in.Code)
	name := strings.TrimSpace(in.Name)
	acc := strings.TrimSpace(in.ExpenseAccountCode)
	if code == "" || name == "" {
		return nil, ErrExpenseTypeInvalid
	}
	// Mapping akun divalidasi SEBELUM disimpan — bukan saat kas keluar.
	if err := validateExpenseAccountDB(ctx, s.db, tenantID, acc); err != nil {
		return nil, err
	}
	et := &ExpenseType{TenantID: tenantID, Code: code, Name: name, ExpenseAccountCode: acc, IsActive: true}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(et).Error; err != nil {
			if isDuplicateExpenseTypeCode(err) {
				return ErrExpenseTypeDuplicate
			}
			return fmt.Errorf("simpan jenis pengeluaran: %w", err)
		}
		return appendExpenseMasterChange(ctx, tx, tenantID, code, "created", "", acc, actor)
	})
	if err != nil {
		return nil, err
	}
	return et, nil
}

type ExpenseTypeUpdate struct {
	Name               *string
	ExpenseAccountCode *string
	IsActive           *bool
}

func (s *ExpenseTypeService) Update(ctx context.Context, tenantID, id uint64, upd ExpenseTypeUpdate, actor *uint64) (*ExpenseType, error) {
	var out ExpenseType
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var et ExpenseType
		if err := tx.WithContext(ctx).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			First(&et).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrExpenseTypeNotFound
			}
			return err
		}
		updates := map[string]interface{}{}
		type change struct{ field, old, nw string }
		var changes []change

		if upd.Name != nil && strings.TrimSpace(*upd.Name) != "" && strings.TrimSpace(*upd.Name) != et.Name {
			n := strings.TrimSpace(*upd.Name)
			updates["name"] = n
			changes = append(changes, change{"name", et.Name, n})
		}
		if upd.ExpenseAccountCode != nil {
			acc := strings.TrimSpace(*upd.ExpenseAccountCode)
			if acc != "" && acc != et.ExpenseAccountCode {
				// Berlaku sama untuk perubahan mapping, bukan hanya saat dibuat.
				if err := validateExpenseAccountDB(ctx, tx, tenantID, acc); err != nil {
					return err
				}
				updates["expense_account_code"] = acc
				changes = append(changes, change{"expense_account_code", et.ExpenseAccountCode, acc})
			}
		}
		if upd.IsActive != nil && *upd.IsActive != et.IsActive {
			updates["is_active"] = *upd.IsActive
			changes = append(changes, change{"is_active", boolText(et.IsActive), boolText(*upd.IsActive)})
		}
		if len(updates) == 0 {
			out = et
			return nil
		}
		if err := tx.WithContext(ctx).Model(&ExpenseType{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("ubah jenis pengeluaran: %w", err)
		}
		for _, c := range changes {
			if err := appendExpenseMasterChange(ctx, tx, tenantID, et.Code, c.field, c.old, c.nw, actor); err != nil {
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

// ListMasterChanges mengembalikan audit perubahan master jenis pengeluaran.
func (s *ExpenseTypeService) ListMasterChanges(ctx context.Context, tenantID uint64, limit int) ([]*masterDataChange, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var out []*masterDataChange
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND entity = ?", tenantID, EntityExpenseType).
		Order("id DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListMasterChanges: %w", err)
	}
	return out, nil
}

func appendExpenseMasterChange(ctx context.Context, tx *gorm.DB, tenantID uint64, code, field, oldV, newV string, actor *uint64) error {
	row := &masterDataChange{
		TenantID: tenantID, Entity: EntityExpenseType, EntityCode: code,
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

func isDuplicateExpenseTypeCode(err error) bool {
	return err != nil && strings.Contains(err.Error(), "1062")
}

// ── Seed ────────────────────────────────────────────────────────────────────

// defaultExpenseTypes: cermin seed migration 000072. Hanya memetakan ke akun
// yang sudah ada di COA bawaan (ledger/coa.go) — seed tidak pernah menciptakan
// akun baru.
var defaultExpenseTypes = []ExpenseType{
	{Code: "gaji", Name: "Gaji & Tunjangan", ExpenseAccountCode: "5-4100"},
	{Code: "sewa-kantor", Name: "Sewa Kantor", ExpenseAccountCode: "5-4200"},
	{Code: "utilitas", Name: "Listrik, Air & Internet", ExpenseAccountCode: "5-4300"},
	{Code: "transport", Name: "Transport & Perjalanan Dinas", ExpenseAccountCode: "5-4400"},
	{Code: "penyusutan", Name: "Penyusutan", ExpenseAccountCode: "5-4500"},
	{Code: "atk", Name: "ATK & Perlengkapan Kantor", ExpenseAccountCode: "5-4000"},
	{Code: "software", Name: "Software & Langganan", ExpenseAccountCode: "5-4000"},
}

// SeedDefaultExpenseTypes menyemai master utk tenant BARU (idempoten).
func SeedDefaultExpenseTypes(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	for _, et := range defaultExpenseTypes {
		row := et
		row.TenantID = tenantID
		row.IsActive = true
		if err := db.WithContext(ctx).
			Where("tenant_id = ? AND code = ?", tenantID, row.Code).
			FirstOrCreate(&row).Error; err != nil {
			return fmt.Errorf("seed jenis pengeluaran %s: %w", row.Code, err)
		}
	}
	return nil
}
