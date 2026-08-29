package fixedasset

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── GORMRepository implements Store, Accounts, CategoryResolver ─────────────

type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ── Accounts ─────────────────────────────────────────────────────────────────

func (r *GORMRepository) ResolveAccountID(ctx context.Context, tenantID uint64, code string) (uint64, error) {
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Select("id").
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrAccountNotFound, code)
	}
	return acc.ID, nil
}

// ValidatePaymentAccountCode membuktikan sebuah kode akun sah sebagai sumber
// pembayaran. Aturan "apa itu akun kas/bank" DIPINJAM dari ledger
// (ledger.ValidatePaymentAccount) — satu-satunya pemilik definisi itu,
// mengikuti pola cost.GORMRepository.ValidatePaymentAccountCode persis.
func (r *GORMRepository) ValidatePaymentAccountCode(ctx context.Context, tenantID uint64, code string) (uint64, error) {
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrNotCashBankAccount, code)
	}
	if err := ledger.ValidatePaymentAccount(&acc); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrNotCashBankAccount, err)
	}
	return acc.ID, nil
}

// ── Store ────────────────────────────────────────────────────────────────────

// CreateAsset menyimpan baris Register lalu mengisi AssetCode berdasarkan ID
// yang baru di-generate (mis. "FA-000042") — asset_code BUKAN dokumen legal
// (beda dengan BKK/invoice W-2), jadi tidak lewat document numbering engine.
func (r *GORMRepository) CreateAsset(ctx context.Context, asset *FixedAsset) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(asset).Error; err != nil {
			return fmt.Errorf("simpan aset tetap: %w", err)
		}
		asset.AssetCode = fmt.Sprintf("FA-%06d", asset.ID)
		if err := tx.WithContext(ctx).Model(&FixedAsset{}).
			Where("id = ? AND tenant_id = ?", asset.ID, asset.TenantID).
			Update("asset_code", asset.AssetCode).Error; err != nil {
			return fmt.Errorf("isi kode aset tetap: %w", err)
		}
		return nil
	})
}

func (r *GORMRepository) GetAsset(ctx context.Context, tenantID, assetID uint64) (*FixedAsset, error) {
	var a FixedAsset
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", assetID, tenantID).
		First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAssetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("GetAsset: %w", err)
	}
	return &a, nil
}

func (r *GORMRepository) ListAssets(ctx context.Context, tenantID uint64) ([]*FixedAsset, error) {
	var out []*FixedAsset
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("acquisition_date DESC, id DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListAssets: %w", err)
	}
	return out, nil
}

func (r *GORMRepository) ListActiveAssets(ctx context.Context, tenantID uint64) ([]*FixedAsset, error) {
	var out []*FixedAsset
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", tenantID, AssetStatusActive).
		Order("id ASC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListActiveAssets: %w", err)
	}
	return out, nil
}

func (r *GORMRepository) HasDepreciationLine(ctx context.Context, tenantID, assetID uint64, year uint16, month uint8) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&DepreciationLine{}).
		Where("tenant_id = ? AND fixed_asset_id = ? AND period_year = ? AND period_month = ?", tenantID, assetID, year, month).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("HasDepreciationLine: %w", err)
	}
	return count > 0, nil
}

func (r *GORMRepository) CreateDepreciationLine(ctx context.Context, line *DepreciationLine) error {
	if err := r.db.WithContext(ctx).Create(line).Error; err != nil {
		return fmt.Errorf("CreateDepreciationLine: %w", err)
	}
	return nil
}

// SumDepreciation mengembalikan akumulasi penyusutan terposting suatu aset —
// diturunkan dari SUM garis penyusutan, bukan kolom yang bisa basi (lihat
// catatan di migrasi 000087 dan model.go).
func (r *GORMRepository) SumDepreciation(ctx context.Context, tenantID, assetID uint64) (domain.Money, error) {
	var sum struct {
		Total string
	}
	err := r.db.WithContext(ctx).Model(&DepreciationLine{}).
		Select("COALESCE(SUM(amount), 0) AS total").
		Where("tenant_id = ? AND fixed_asset_id = ?", tenantID, assetID).
		Take(&sum).Error
	if err != nil {
		return domain.Money{}, fmt.Errorf("SumDepreciation: %w", err)
	}
	if sum.Total == "" {
		return domain.FromInt(0), nil
	}
	m, err := domain.NewMoney(sum.Total)
	if err != nil {
		return domain.Money{}, fmt.Errorf("SumDepreciation: parse %q: %w", sum.Total, err)
	}
	return m, nil
}

func (r *GORMRepository) ListDepreciationLines(ctx context.Context, tenantID, assetID uint64) ([]*DepreciationLine, error) {
	var out []*DepreciationLine
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND fixed_asset_id = ?", tenantID, assetID).
		Order("period_year ASC, period_month ASC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListDepreciationLines: %w", err)
	}
	return out, nil
}

// ── GORMTxRunner ─────────────────────────────────────────────────────────────

// GORMTxRunner menjalankan operasi fixed asset dalam satu transaksi database.
// Kolaborator dibangun ulang di atas transaksi — mengikuti idiom yang sama
// dengan cost.GORMTxRunner persis (tidak ada mesin transaksi kedua).
type GORMTxRunner struct {
	db *gorm.DB
}

func NewGORMTxRunner(db *gorm.DB) *GORMTxRunner {
	return &GORMTxRunner{db: db}
}

func (t *GORMTxRunner) InTx(ctx context.Context, fn func(JournalWriter, Store) error) error {
	return t.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
		return fn(NewLedgerJournalAdapter(txPosting), NewGORMRepository(tx))
	})
}

// ── LedgerJournalAdapter wraps *ledger.PostingService as fixedasset.JournalWriter ──

type LedgerJournalAdapter struct {
	svc *ledger.PostingService
}

func NewLedgerJournalAdapter(svc *ledger.PostingService) *LedgerJournalAdapter {
	return &LedgerJournalAdapter{svc: svc}
}

func (a *LedgerJournalAdapter) CreateJournal(ctx context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (uint64, error) {
	llines := make([]ledger.LineInput, len(lines))
	for i, l := range lines {
		llines[i] = ledger.LineInput{
			AccountID:   l.AccountID,
			Debit:       l.Debit,
			Credit:      l.Credit,
			ProjectID:   l.ProjectID,
			Description: l.Description,
		}
	}
	entry, err := a.svc.Create(ctx, ledger.CreateJournalRequest{
		TenantID:    tenantID,
		Date:        date,
		Description: description,
		Lines:       llines,
		Source:      "fixedasset",
	})
	if err != nil {
		return 0, err
	}
	return entry.ID, nil
}

// PostJournal memposting jurnal fixed asset. cashOut=true (perolehan aset)
// menerbitkan dokumen BKK di transaksi yang sama lewat PostDraft (INV-DOC-1);
// cashOut=false (penyusutan bulanan, tidak ada pergerakan kas) memposting
// biasa tanpa nomor dokumen kas.
func (a *LedgerJournalAdapter) PostJournal(ctx context.Context, tenantID, journalID uint64, cashOut bool) error {
	if !cashOut {
		_, err := a.svc.Post(ctx, tenantID, journalID)
		return err
	}
	_, err := a.svc.PostDraft(ctx, tenantID, journalID, ledger.DocumentSpec{TypeCode: ledger.DocCashOut})
	return err
}
