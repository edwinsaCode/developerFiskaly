package allocation

// P0-4 (D1) — Versioning basis alokasi.
//
// Basis alokasi IMMUTABLE untuk sebuah proyek setelah snapshot BAST pertama
// ada; perubahan basis = VERSION baru (deliberate, audited). Snapshot BAST
// mem-pin version yang dipakai (migration 000030); true-up membaca basis dari
// version yang di-pin — bukan config saat ini.
//
// Tabel `allocation_config_versions` (migration 000029). `allocation_configs`
// lama tetap menyimpan "basis aktif saat ini" (kompatibilitas reader lama).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AllocationConfigVersion adalah satu versi basis alokasi proyek (append-only;
// supersede via active_key, pola budget_plans).
type AllocationConfigVersion struct {
	ID        uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint64          `gorm:"not null;index"           json:"-"`
	ProjectID uint64          `gorm:"not null"                 json:"project_id"`
	Version   int             `gorm:"not null;default:1"       json:"version"`
	Basis     AllocationBasis `gorm:"not null;size:20"         json:"basis"`
	ActiveKey *string         `gorm:"size:1"                   json:"-"` // 'Y' aktif, NULL superseded
	CreatedBy *uint64         `                                json:"created_by,omitempty"`
	CreatedAt time.Time       `                                json:"created_at"`
	UpdatedAt time.Time       `                                json:"updated_at"`
}

func (AllocationConfigVersion) TableName() string { return "allocation_config_versions" }

// VersionStore adalah kontrak persistence versioning (opsional — WithVersionStore).
type VersionStore interface {
	GetActiveVersion(ctx context.Context, tenantID, projectID uint64) (*AllocationConfigVersion, error)
	// HasSnapshots: apakah proyek sudah punya allocation snapshot (BAST budgeted)
	// — penentu freeze D1.
	HasSnapshots(ctx context.Context, tenantID, projectID uint64) (bool, error)
	// UpsertActiveVersion: update basis version aktif IN-PLACE (pra-freeze), atau
	// buat version 1 bila belum ada.
	UpsertActiveVersion(ctx context.Context, tenantID, projectID uint64, basis AllocationBasis) (*AllocationConfigVersion, error)
	// CreateNextVersion: supersede version aktif + buat version max+1 (pasca-freeze).
	CreateNextVersion(ctx context.Context, tenantID, projectID uint64, basis AllocationBasis) (*AllocationConfigVersion, error)
}

// WithVersionStore mengaktifkan versioning D1 pada Service (wiring produksi).
// Tanpa opsi ini SetBasis berperilaku legacy (unit test lama tidak terpengaruh).
func WithVersionStore(vs VersionStore) ServiceOption {
	return func(s *Service) { s.versions = vs }
}

// ActiveConfigVersion mengembalikan version basis aktif proyek — dipakai
// resolver BAST untuk mem-pin snapshot (D1) dan closing untuk true-up.
// Self-heal: proyek lama yang punya config tapi belum punya version row
// (dibuat sebelum 000029 backfill mencakupnya) mendapat version 1 otomatis.
func (s *Service) ActiveConfigVersion(ctx context.Context, tenantID, projectID uint64) (*AllocationConfigVersion, error) {
	if s.versions == nil {
		return nil, fmt.Errorf("version store tidak dikonfigurasi")
	}
	v, err := s.versions.GetActiveVersion(ctx, tenantID, projectID)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, ErrConfigNotFound) {
		return nil, err
	}
	cfg, cerr := s.configs.GetConfig(ctx, tenantID, projectID)
	if cerr != nil {
		return nil, cerr // termasuk ErrConfigNotFound: basis memang belum diatur
	}
	return s.versions.UpsertActiveVersion(ctx, tenantID, projectID, cfg.Basis)
}

// ── GORM implementation ───────────────────────────────────────────────────────

func (r *GORMRepository) GetActiveVersion(ctx context.Context, tenantID, projectID uint64) (*AllocationConfigVersion, error) {
	var v AllocationConfigVersion
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ? AND active_key = 'Y'", tenantID, projectID).
		First(&v).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConfigNotFound
		}
		return nil, fmt.Errorf("GetActiveVersion: %w", err)
	}
	return &v, nil
}

// HasSnapshots membaca tabel allocation_snapshots via SQL (tanpa import package
// sale — hindari siklus; nama tabel = kontrak schema, bukan kontrak Go).
func (r *GORMRepository) HasSnapshots(ctx context.Context, tenantID, projectID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Table("allocation_snapshots").
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("HasSnapshots: %w", err)
	}
	return n > 0, nil
}

func (r *GORMRepository) UpsertActiveVersion(ctx context.Context, tenantID, projectID uint64, basis AllocationBasis) (*AllocationConfigVersion, error) {
	existing, err := r.GetActiveVersion(ctx, tenantID, projectID)
	if err != nil && !errors.Is(err, ErrConfigNotFound) {
		return nil, err
	}
	if existing != nil {
		if existing.Basis != basis {
			existing.Basis = basis
			if err := r.db.WithContext(ctx).Model(&AllocationConfigVersion{}).
				Where("id = ?", existing.ID).
				Update("basis", string(basis)).Error; err != nil {
				return nil, fmt.Errorf("UpsertActiveVersion update: %w", err)
			}
		}
		return existing, nil
	}
	y := "Y"
	v := &AllocationConfigVersion{TenantID: tenantID, ProjectID: projectID, Version: 1, Basis: basis, ActiveKey: &y}
	if err := r.db.WithContext(ctx).Create(v).Error; err != nil {
		return nil, fmt.Errorf("UpsertActiveVersion create: %w", err)
	}
	return v, nil
}

func (r *GORMRepository) CreateNextVersion(ctx context.Context, tenantID, projectID uint64, basis AllocationBasis) (*AllocationConfigVersion, error) {
	var out *AllocationConfigVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var maxVer int
		if err := tx.Model(&AllocationConfigVersion{}).
			Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
			Select("COALESCE(MAX(version), 0)").Scan(&maxVer).Error; err != nil {
			return fmt.Errorf("max version: %w", err)
		}
		if err := tx.Model(&AllocationConfigVersion{}).
			Where("tenant_id = ? AND project_id = ? AND active_key = 'Y'", tenantID, projectID).
			Update("active_key", nil).Error; err != nil {
			return fmt.Errorf("supersede version aktif: %w", err)
		}
		y := "Y"
		v := &AllocationConfigVersion{TenantID: tenantID, ProjectID: projectID, Version: maxVer + 1, Basis: basis, ActiveKey: &y}
		if err := tx.Create(v).Error; err != nil {
			return fmt.Errorf("buat version baru: %w", err)
		}
		out = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
