package document

// Service — pengelolaan master jenis dokumen + pembacaan registry.
// Penomorannya sendiri ada di engine.go; service tidak pernah menaikkan seri.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// ── Master ──────────────────────────────────────────────────────────────────

func (s *Service) ListTypes(ctx context.Context, tenantID uint64) ([]*DocumentType, error) {
	var out []*DocumentType
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("is_active DESC, code").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListTypes: %w", err)
	}
	return out, nil
}

type TypeInput struct {
	Code         string
	Name         string
	Prefix       string
	NumberFormat string
	ResetPolicy  ResetPolicy
	Padding      uint8
}

func (s *Service) CreateType(ctx context.Context, tenantID uint64, in TypeInput, actor *uint64) (*DocumentType, error) {
	dt := &DocumentType{
		TenantID:     tenantID,
		Code:         NormalizeCode(in.Code),
		Name:         strings.TrimSpace(in.Name),
		Prefix:       strings.ToUpper(strings.TrimSpace(in.Prefix)),
		NumberFormat: strings.TrimSpace(in.NumberFormat),
		ResetPolicy:  in.ResetPolicy,
		Padding:      in.Padding,
		IsActive:     true,
		IsSystem:     false,
	}
	if dt.NumberFormat == "" {
		dt.NumberFormat = "{prefix}/{year}/{seq}"
	}
	if dt.ResetPolicy == "" {
		dt.ResetPolicy = ResetYearly
	}
	if dt.Padding == 0 {
		dt.Padding = 6
	}
	if err := validateType(dt); err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(dt).Error; err != nil {
			if isDuplicateKey(err) {
				return ErrTypeDuplicate
			}
			return fmt.Errorf("simpan jenis dokumen: %w", err)
		}
		return appendAudit(tx, tenantID, dt.Code, "created", "", dt.Prefix, actor)
	})
	if err != nil {
		return nil, err
	}
	return dt, nil
}

type TypeUpdate struct {
	Name         *string
	Prefix       *string
	NumberFormat *string
	ResetPolicy  *ResetPolicy
	Padding      *uint8
	IsActive     *bool
}

// UpdateType mengubah KONFIGURASI penomoran. Kode tidak pernah bisa diubah —
// kode adalah kunci yang dipakai modul lain saat meminta nomor, dan mengubahnya
// akan memutus penomoran seluruh dokumen jenis itu.
func (s *Service) UpdateType(ctx context.Context, tenantID, id uint64, upd TypeUpdate, actor *uint64) (*DocumentType, error) {
	var out DocumentType
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var dt DocumentType
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", id, tenantID).First(&dt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTypeNotFound
			}
			return err
		}
		next := dt
		updates := map[string]interface{}{}
		type change struct{ field, old, nw string }
		var changes []change
		set := func(field string, old, nw string) {
			updates[field] = nw
			changes = append(changes, change{field, old, nw})
		}

		if upd.Name != nil {
			if v := strings.TrimSpace(*upd.Name); v != "" && v != dt.Name {
				next.Name = v
				set("name", dt.Name, v)
			}
		}
		if upd.Prefix != nil {
			if v := strings.ToUpper(strings.TrimSpace(*upd.Prefix)); v != "" && v != dt.Prefix {
				next.Prefix = v
				set("prefix", dt.Prefix, v)
			}
		}
		if upd.NumberFormat != nil {
			if v := strings.TrimSpace(*upd.NumberFormat); v != "" && v != dt.NumberFormat {
				next.NumberFormat = v
				set("number_format", dt.NumberFormat, v)
			}
		}
		if upd.ResetPolicy != nil && *upd.ResetPolicy != "" && *upd.ResetPolicy != dt.ResetPolicy {
			next.ResetPolicy = *upd.ResetPolicy
			set("reset_policy", string(dt.ResetPolicy), string(*upd.ResetPolicy))
		}
		if upd.Padding != nil && *upd.Padding != 0 && *upd.Padding != dt.Padding {
			next.Padding = *upd.Padding
			updates["padding"] = *upd.Padding
			changes = append(changes, change{"padding", fmt.Sprint(dt.Padding), fmt.Sprint(*upd.Padding)})
		}
		if upd.IsActive != nil && *upd.IsActive != dt.IsActive {
			// Menonaktifkan jenis dokumen inti akan menggagalkan penerimaan
			// pembayaran (resolver fail-closed) — dilarang di sini, bukan
			// ditemukan admin saat kas sudah di tangan.
			if !*upd.IsActive && dt.IsSystem {
				return fmt.Errorf("%w: %q dipakai alur inti dan tidak boleh dinonaktifkan", ErrTypeInvalid, dt.Code)
			}
			next.IsActive = *upd.IsActive
			updates["is_active"] = *upd.IsActive
			changes = append(changes, change{"is_active", boolStr(dt.IsActive), boolStr(*upd.IsActive)})
		}
		if len(updates) == 0 {
			out = dt
			return nil
		}
		// Konfigurasi baru harus bisa merender nomor SEBELUM disimpan —
		// format rusak yang tersimpan baru ketahuan saat dokumen gagal terbit.
		if err := validateType(&next); err != nil {
			return err
		}
		if err := tx.Model(&DocumentType{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).Updates(updates).Error; err != nil {
			return fmt.Errorf("ubah jenis dokumen: %w", err)
		}
		for _, c := range changes {
			if err := appendAudit(tx, tenantID, dt.Code, c.field, c.old, c.nw, actor); err != nil {
				return err
			}
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).First(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// validateType memastikan konfigurasi menghasilkan nomor yang sah.
func validateType(dt *DocumentType) error {
	if dt.Code == "" || dt.Name == "" {
		return fmt.Errorf("%w: kode dan nama wajib diisi", ErrTypeInvalid)
	}
	if strings.TrimSpace(dt.Prefix) == "" {
		return fmt.Errorf("%w: prefix wajib diisi", ErrTypeInvalid)
	}
	if !dt.ResetPolicy.Valid() {
		return fmt.Errorf("%w: kebijakan reset %q", ErrTypeInvalid, dt.ResetPolicy)
	}
	if dt.Padding < 1 || dt.Padding > 12 {
		return fmt.Errorf("%w: padding harus 1..12", ErrTypeInvalid)
	}
	// Dirender sekali dengan contoh nilai — kalau format tidak sah, gagal di
	// sini, bukan saat dokumen sungguhan hendak terbit.
	if _, err := formatNumber(dt, 1, time.Now()); err != nil {
		return err
	}
	return nil
}

// ── Pratinjau ───────────────────────────────────────────────────────────────

// PreviewNext menunjukkan nomor BERIKUTNYA tanpa memakainya. Read-only —
// tidak menyentuh seri sama sekali.
type Preview struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	FiscalYear uint16 `json:"fiscal_year"`
	LastVal    uint64 `json:"last_val"`
	NextNumber string `json:"next_number"`
	Issued     int64  `json:"issued"`
}

func (s *Service) PreviewAll(ctx context.Context, tenantID uint64, at time.Time) ([]Preview, error) {
	if at.IsZero() {
		at = time.Now()
	}
	types, err := s.ListTypes(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]Preview, 0, len(types))
	for _, dt := range types {
		fy := fiscalYearFor(dt.ResetPolicy, at)
		var last uint64
		if err := s.db.WithContext(ctx).Raw(`
			SELECT COALESCE(last_val, 0) FROM document_sequences
			 WHERE tenant_id = ? AND document_type_code = ? AND fiscal_year = ?`,
			tenantID, dt.Code, fy).Scan(&last).Error; err != nil {
			return nil, fmt.Errorf("baca seri %s: %w", dt.Code, err)
		}
		var issued int64
		if err := s.db.WithContext(ctx).Model(&Document{}).
			Where("tenant_id = ? AND document_type_code = ? AND fiscal_year = ?", tenantID, dt.Code, fy).
			Count(&issued).Error; err != nil {
			return nil, fmt.Errorf("hitung dokumen %s: %w", dt.Code, err)
		}
		p := Preview{Code: dt.Code, Name: dt.Name, FiscalYear: fy, LastVal: last, Issued: issued}
		if n, ferr := formatNumber(dt, last+1, at); ferr == nil {
			p.NextNumber = n
		}
		out = append(out, p)
	}
	return out, nil
}

// ── Registry ────────────────────────────────────────────────────────────────

type ListFilter struct {
	TypeCode   string
	FiscalYear uint16
	// Number — pencarian PERSIS satu nomor dokumen (mis. "BKK/2026/000002"),
	// dipakai layar mana pun yang menampilkan nomor dokumen sebagai teks dan
	// perlu membukanya (lihat DocumentNumberLink di frontend). Nomor bersifat
	// unik per tenant dan tidak pernah dipakai ulang (append-only).
	Number string
	Limit  int
}

func (s *Service) ListDocuments(ctx context.Context, tenantID uint64, f ListFilter) ([]*Document, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if c := NormalizeCode(f.TypeCode); c != "" {
		q = q.Where("document_type_code = ?", c)
	}
	if f.FiscalYear > 0 {
		q = q.Where("fiscal_year = ?", f.FiscalYear)
	}
	if n := strings.TrimSpace(f.Number); n != "" {
		q = q.Where("number = ?", n)
	}
	var out []*Document
	if err := q.Order("id DESC").Limit(f.Limit).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListDocuments: %w", err)
	}
	return out, nil
}

// ── Audit ───────────────────────────────────────────────────────────────────

// MasterChange adalah satu baris audit master (bentuk sama dengan master lain,
// dibaca dari tabel `master_data_changes`).
type MasterChange struct {
	ID         uint64    `json:"id"`
	Entity     string    `json:"entity"`
	EntityCode string    `json:"entity_code"`
	Field      string    `json:"field"`
	OldValue   string    `json:"old_value"`
	NewValue   string    `json:"new_value"`
	ChangedBy  *uint64   `json:"changed_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListMasterChanges — riwayat perubahan konfigurasi penomoran. Mengubah prefix
// atau format berarti mengubah bentuk nomor dokumen resmi perusahaan; admin
// harus bisa menelusuri sendiri siapa mengubah apa, kapan.
func (s *Service) ListMasterChanges(ctx context.Context, tenantID uint64, limit int) ([]MasterChange, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	var out []MasterChange
	if err := s.db.WithContext(ctx).Raw(`
		SELECT id, entity, entity_code, field, old_value, new_value, changed_by, created_at
		  FROM master_data_changes
		 WHERE tenant_id = ? AND entity = ?
		 ORDER BY id DESC LIMIT ?`, tenantID, EntityDocumentType, limit).
		Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("ListMasterChanges: %w", err)
	}
	return out, nil
}

// EntityDocumentType adalah nilai `entity` pada master_data_changes.
const EntityDocumentType = "document_type"

// appendAudit menulis satu baris audit master (append-only). Memakai tabel
// `master_data_changes` yang sama dengan master lain — ditulis lewat SQL polos
// supaya package ini tidak perlu import package charge.
func appendAudit(tx *gorm.DB, tenantID uint64, code, field, oldV, newV string, actor *uint64) error {
	if err := tx.Exec(`
		INSERT INTO master_data_changes
			(tenant_id, entity, entity_code, field, old_value, new_value, changed_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NOW(3), NOW(3))`,
		tenantID, EntityDocumentType, code, field, oldV, newV, actor,
	).Error; err != nil {
		return fmt.Errorf("audit master jenis dokumen: %w", err)
	}
	return nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
