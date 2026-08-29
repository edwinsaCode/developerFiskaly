package charge

// W-1 — Master Jenis Biaya Realisasi (keputusan bisnis FINAL klien 2026-08-06).
//
// Notaris, BPHTB, PDAM, dan Listrik BUKAN produk yang dijual: mereka jenis biaya
// realisasi yang ditagihkan ke customer sebagai TITIPAN. Sebelum W-1 tidak ada
// masternya sama sekali — `charge_items.label` teks bebas, dan akun titipan
// di-hardcode satu konstanta. Akibatnya perlakuan akuntansi sebuah item
// bergantung pada apa yang diketik admin.
//
// Master ini menjadikan aturannya DATA: setiap item tagihan menunjuk satu jenis
// biaya, dan jenis biaya itulah yang menentukan perlakuan (domain.BillingTreatment)
// serta akun kewajiban titipannya. Admin mengelolanya sendiri lewat CRUD —
// tanpa programmer, sesuai keputusan klien.
//
// FAIL-CLOSED, mengikuti pola project.ResolveProductPolicy: kode tak terdaftar,
// jenis nonaktif, mapping akun kosong/tidak sah, atau error DB SEMUANYA
// mengembalikan error sehingga transaksi batal SEBELUM ada jurnal.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// DefaultRealizationDepositAccount adalah akun titipan default seluruh jenis
// biaya realisasi. Owner menulis jurnalnya sebagai SATU baris "Titipan
// Realisasi" — bukan satu akun per vendor.
const DefaultRealizationDepositAccount = AccountCodeTitipanRealisasi

// RealizationChargeType adalah master satu jenis biaya realisasi tenant.
type RealizationChargeType struct {
	ID                 uint64                  `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID           uint64                  `gorm:"not null;index"                               json:"-"`
	Code               string                  `gorm:"not null;size:50"                             json:"code"`
	Name               string                  `gorm:"not null;size:200"                            json:"name"`
	Treatment          domain.BillingTreatment `gorm:"not null;size:30;default:'deposit_liability'" json:"treatment"`
	DepositAccountCode string                  `gorm:"not null;size:20;default:'2-2400'"            json:"deposit_account_code"`
	IsActive           bool                    `gorm:"not null;default:true"                        json:"is_active"`
	CreatedAt          time.Time               `                                                    json:"created_at"`
	UpdatedAt          time.Time               `                                                    json:"updated_at"`
}

func (RealizationChargeType) TableName() string { return "realization_charge_types" }

// MasterDataChange adalah audit APPEND-ONLY perubahan master data finansial
// (TD-8). Mengubah akun titipan sebuah jenis biaya mengubah ke mana uang
// customer mendarat — itu keputusan finansial, bukan sekadar edit label.
type MasterDataChange struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   uint64    `gorm:"not null;index"           json:"-"`
	Entity     string    `gorm:"not null;size:64"         json:"entity"`
	EntityCode string    `gorm:"not null;size:64"         json:"entity_code"`
	Field      string    `gorm:"not null;size:64"         json:"field"`
	OldValue   string    `gorm:"not null;size:255"        json:"old_value"`
	NewValue   string    `gorm:"not null;size:255"        json:"new_value"`
	ChangedBy  *uint64   `                                json:"changed_by,omitempty"`
	CreatedAt  time.Time `                                json:"created_at"`
	UpdatedAt  time.Time `                                json:"-"`
}

func (MasterDataChange) TableName() string { return "master_data_changes" }

// EntityRealizationChargeType adalah nilai `entity` untuk audit master di atas.
const EntityRealizationChargeType = "realization_charge_type"

// ── Normalisasi ─────────────────────────────────────────────────────────────

// normalizeChargeTypeCode menyamakan pencocokan kode dengan collation MySQL
// (utf8mb4_unicode_ci — case-insensitive) agar lookup di Go dan pencarian di DB
// tidak pernah berbeda kesimpulan. Pola sama dengan normalizeProductCode.
func normalizeChargeTypeCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// ── Store ───────────────────────────────────────────────────────────────────

func (s *Service) ListChargeTypes(ctx context.Context, tenantID uint64) ([]*RealizationChargeType, error) {
	var out []*RealizationChargeType
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("is_active DESC, code").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListChargeTypes: %w", err)
	}
	return out, nil
}

// ── Resolver KANONIK jenis biaya realisasi ──────────────────────────────────

// ResolveChargeTypePolicy mengembalikan kebijakan untuk satu kode jenis biaya.
// FAIL-CLOSED — lihat catatan paket di atas.
func resolveChargeTypePolicyDB(ctx context.Context, db *gorm.DB, tenantID uint64, code string) (domain.RealizationChargePolicy, error) {
	c := normalizeChargeTypeCode(code)
	if c == "" {
		return domain.RealizationChargePolicy{}, fmt.Errorf("%w: kode kosong", ErrChargeTypeUnknown)
	}
	var ct RealizationChargeType
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, c).
		First(&ct).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.RealizationChargePolicy{}, fmt.Errorf("%w: %q", ErrChargeTypeUnknown, c)
	}
	if err != nil {
		// Error DB TIDAK boleh menjadi fallback akun — ini jalur uang.
		return domain.RealizationChargePolicy{}, fmt.Errorf("resolve jenis biaya %q: %w", c, err)
	}
	return chargeTypePolicyOf(&ct)
}

// ResolveChargeTypePolicy adalah pintu publik resolver di atas.
func (s *Service) ResolveChargeTypePolicy(ctx context.Context, tenantID uint64, code string) (domain.RealizationChargePolicy, error) {
	return resolveChargeTypePolicyDB(ctx, s.db, tenantID, code)
}

// chargeTypePolicyOf memvalidasi baris master lalu memetakannya ke value object.
func chargeTypePolicyOf(ct *RealizationChargeType) (domain.RealizationChargePolicy, error) {
	if !ct.IsActive {
		return domain.RealizationChargePolicy{}, fmt.Errorf("%w: %q", ErrChargeTypeInactive, ct.Code)
	}
	if !ct.Treatment.Valid() {
		return domain.RealizationChargePolicy{}, fmt.Errorf("%w: %q perlakuan %q", ErrChargeTypeInvalid, ct.Code, ct.Treatment)
	}
	if strings.TrimSpace(ct.DepositAccountCode) == "" {
		return domain.RealizationChargePolicy{}, fmt.Errorf("%w: jenis %q belum punya akun titipan", ErrDepositAccountInvalid, ct.Code)
	}
	return domain.RealizationChargePolicy{
		Code:               ct.Code,
		Name:               ct.Name,
		Treatment:          ct.Treatment,
		DepositAccountCode: strings.TrimSpace(ct.DepositAccountCode),
	}, nil
}

// ── Validasi akun titipan (pola H-2 project.ValidateRevenueAccount) ─────────

// validateDepositAccountDB memastikan kode akun BOLEH dipakai sebagai akun
// titipan: ada di COA tenant, aktif, dan bertipe liability. Akun pendapatan
// ditolak keras — K-1 melarang biaya realisasi menyentuh 4-xxxx, dan mapping
// yang salah harus gagal SEBELUM ada jurnal, bukan menghasilkan jurnal balanced
// yang salah semantik.
func validateDepositAccountDB(ctx context.Context, db *gorm.DB, tenantID uint64, code string) error {
	c := strings.TrimSpace(code)
	if c == "" {
		return fmt.Errorf("%w: kode akun kosong", ErrDepositAccountInvalid)
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
		return fmt.Errorf("%w: akun %q tidak ada di COA", ErrDepositAccountInvalid, c)
	}
	if err != nil {
		return fmt.Errorf("validasi akun titipan %q: %w", c, err)
	}
	if !f.IsActive {
		return fmt.Errorf("%w: akun %q nonaktif", ErrDepositAccountInvalid, c)
	}
	if domain.AccountType(f.Type) != domain.AccountLiability {
		return fmt.Errorf("%w: akun %q bertipe %s — titipan realisasi hanya boleh dipetakan ke akun kewajiban (K-1)",
			ErrDepositAccountInvalid, c, f.Type)
	}
	return nil
}

// ── CRUD (admin mengelola sendiri — keputusan klien §5) ────────────────────

type ChargeTypeInput struct {
	Code               string
	Name               string
	DepositAccountCode string
}

func (s *Service) CreateChargeType(ctx context.Context, tenantID uint64, in ChargeTypeInput, actor *uint64) (*RealizationChargeType, error) {
	code := normalizeChargeTypeCode(in.Code)
	name := strings.TrimSpace(in.Name)
	if code == "" || name == "" {
		return nil, ErrChargeTypeInvalid
	}
	acc := strings.TrimSpace(in.DepositAccountCode)
	if acc == "" {
		acc = DefaultRealizationDepositAccount
	}
	// Mapping akun divalidasi SEBELUM disimpan — bukan saat kas diterima.
	if err := validateDepositAccountDB(ctx, s.db, tenantID, acc); err != nil {
		return nil, err
	}
	ct := &RealizationChargeType{
		TenantID:           tenantID,
		Code:               code,
		Name:               name,
		Treatment:          domain.TreatmentDepositLiability,
		DepositAccountCode: acc,
		IsActive:           true,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(ct).Error; err != nil {
			if isDuplicateChargeTypeCode(err) {
				return ErrChargeTypeDuplicate
			}
			return fmt.Errorf("simpan jenis biaya: %w", err)
		}
		return appendMasterChange(ctx, tx, tenantID, EntityRealizationChargeType, code, "created", "", acc, actor)
	})
	if err != nil {
		return nil, err
	}
	return ct, nil
}

type ChargeTypeUpdate struct {
	Name               *string
	DepositAccountCode *string
	IsActive           *bool
}

func (s *Service) UpdateChargeType(ctx context.Context, tenantID, id uint64, upd ChargeTypeUpdate, actor *uint64) (*RealizationChargeType, error) {
	var out RealizationChargeType
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ct RealizationChargeType
		if err := tx.WithContext(ctx).Clauses(gormLockingUpdate()).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			First(&ct).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrChargeTypeNotFound
			}
			return err
		}
		updates := map[string]interface{}{}
		var changes []MasterDataChange
		add := func(field, old, nw string) {
			changes = append(changes, MasterDataChange{Field: field, OldValue: old, NewValue: nw})
		}
		if upd.Name != nil && strings.TrimSpace(*upd.Name) != "" && strings.TrimSpace(*upd.Name) != ct.Name {
			updates["name"] = strings.TrimSpace(*upd.Name)
			add("name", ct.Name, strings.TrimSpace(*upd.Name))
		}
		if upd.DepositAccountCode != nil {
			acc := strings.TrimSpace(*upd.DepositAccountCode)
			if acc != "" && acc != ct.DepositAccountCode {
				// Berlaku sama untuk perubahan mapping, bukan hanya saat dibuat.
				if err := validateDepositAccountDB(ctx, tx, tenantID, acc); err != nil {
					return err
				}
				updates["deposit_account_code"] = acc
				add("deposit_account_code", ct.DepositAccountCode, acc)
			}
		}
		if upd.IsActive != nil && *upd.IsActive != ct.IsActive {
			updates["is_active"] = *upd.IsActive
			add("is_active", boolPolicyValue(ct.IsActive), boolPolicyValue(*upd.IsActive))
		}
		if len(updates) == 0 {
			out = ct
			return nil
		}
		if err := tx.WithContext(ctx).Model(&RealizationChargeType{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("ubah jenis biaya: %w", err)
		}
		for _, c := range changes {
			if err := appendMasterChange(ctx, tx, tenantID, EntityRealizationChargeType, ct.Code, c.Field, c.OldValue, c.NewValue, actor); err != nil {
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

// appendMasterChange menulis satu baris audit master (append-only).
func appendMasterChange(ctx context.Context, tx *gorm.DB, tenantID uint64, entity, code, field, oldV, newV string, actor *uint64) error {
	row := &MasterDataChange{
		TenantID: tenantID, Entity: entity, EntityCode: code,
		Field: field, OldValue: oldV, NewValue: newV, ChangedBy: actor,
	}
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("audit master data: %w", err)
	}
	return nil
}

// ListMasterChanges mengembalikan audit perubahan master (terbaru dulu).
func (s *Service) ListMasterChanges(ctx context.Context, tenantID uint64, limit int) ([]*MasterDataChange, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var out []*MasterDataChange
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListMasterChanges: %w", err)
	}
	return out, nil
}

// ── Seed ────────────────────────────────────────────────────────────────────

// defaultChargeTypes: cermin seed migration 000061. Keempatnya titipan (K-1);
// akun default sama supaya jurnal owner tetap satu baris "Titipan Realisasi".
var defaultChargeTypes = []RealizationChargeType{
	{Code: "notaris", Name: "Biaya Notaris", Treatment: domain.TreatmentDepositLiability, DepositAccountCode: DefaultRealizationDepositAccount},
	{Code: "bphtb", Name: "BPHTB", Treatment: domain.TreatmentDepositLiability, DepositAccountCode: DefaultRealizationDepositAccount},
	{Code: "pdam", Name: "Sambungan PDAM", Treatment: domain.TreatmentDepositLiability, DepositAccountCode: DefaultRealizationDepositAccount},
	{Code: "listrik", Name: "Sambungan Listrik", Treatment: domain.TreatmentDepositLiability, DepositAccountCode: DefaultRealizationDepositAccount},
}

// SeedDefaultChargeTypes menyemai master utk tenant BARU (idempoten).
func SeedDefaultChargeTypes(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	for _, ct := range defaultChargeTypes {
		row := ct
		row.TenantID = tenantID
		row.IsActive = true
		if err := db.WithContext(ctx).
			Where("tenant_id = ? AND code = ?", tenantID, row.Code).
			FirstOrCreate(&row).Error; err != nil {
			return fmt.Errorf("seed jenis biaya %s: %w", row.Code, err)
		}
	}
	return nil
}

// isDuplicateChargeTypeCode mendeteksi pelanggaran unique key MySQL (1062).
func isDuplicateChargeTypeCode(err error) bool {
	return err != nil && strings.Contains(err.Error(), "1062")
}
