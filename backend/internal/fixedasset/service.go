package fixedasset

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// errDuplicateDepreciation adalah sinyal internal (bukan sentinel publik)
// untuk membatalkan transaksi saat unique constraint INV-FA-1 kalah race
// terhadap pengecekan HasDepreciationLine di atasnya.
var errDuplicateDepreciation = errors.New("duplicate depreciation period (internal)")

// ── Kolaborator (diimplementasikan oleh repository.go) ──────────────────────

// Store adalah sisi tulis/baca Register yang dipakai Service. Setiap
// instance yang diberikan ke TxRunner.InTx SUDAH terikat ke transaksi yang
// sama dengan JournalWriter pada pemanggilan itu — satu commit/rollback utk
// jurnal + register + garis penyusutan sekaligus (atomicity).
type Store interface {
	// CreateAsset menyimpan baris Register baru DAN mengisi AssetCode
	// (mis. "FA-000001") berdasarkan ID yang baru di-generate — lihat
	// catatan di migrasi 000087.
	CreateAsset(ctx context.Context, asset *FixedAsset) error
	GetAsset(ctx context.Context, tenantID, assetID uint64) (*FixedAsset, error)
	// ListActiveAssets mengembalikan aset berstatus active milik tenant,
	// dipakai oleh RunDepreciation untuk menentukan kandidat penyusutan.
	ListActiveAssets(ctx context.Context, tenantID uint64) ([]*FixedAsset, error)
	// HasDepreciationLine — penjaga aplikasi utk INV-FA-1, DI DEPAN unique
	// constraint DB (bukan pengganti; constraint tetap penjaga terakhir).
	HasDepreciationLine(ctx context.Context, tenantID, assetID uint64, year uint16, month uint8) (bool, error)
	CreateDepreciationLine(ctx context.Context, line *DepreciationLine) error
}

// JournalWriter membungkus ledger.PostingService — SATU-SATUNYA mesin
// posting di sistem ini (tidak ada mesin kedua di paket ini). Pola dua fase
// (Create lalu Post) mengikuti cost.JournalWriter persis.
type JournalWriter interface {
	CreateJournal(ctx context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (journalID uint64, err error)
	// PostJournal memposting jurnal draft. cashOut=true menandai pergerakan
	// kas keluar (perolehan aset) sehingga mendapat nomor dokumen BKK
	// (INV-DOC-1) via PostDraft; cashOut=false untuk jurnal non-kas
	// (penyusutan bulanan) yang memposting biasa tanpa nomor dokumen kas.
	PostJournal(ctx context.Context, tenantID, journalID uint64, cashOut bool) error
}

// TxRunner menjalankan fn secara atomic: journals dan store yang diberikan
// ke fn SAMA-SAMA terikat pada satu transaksi DB — commit/rollback bersama.
type TxRunner interface {
	InTx(ctx context.Context, fn func(journals JournalWriter, store Store) error) error
}

// Accounts menerjemahkan kode akun COA → account ID, dan memvalidasi bahwa
// sebuah kode adalah akun kas/bank aktif yang boleh menerima pembayaran
// (dipakai ulang dari ledger.ValidatePaymentAccount — bukan aturan baru).
type Accounts interface {
	ResolveAccountID(ctx context.Context, tenantID uint64, code string) (uint64, error)
	ValidatePaymentAccountCode(ctx context.Context, tenantID uint64, code string) (uint64, error)
}

// ── Service ──────────────────────────────────────────────────────────────────

type Service struct {
	store      TxRunner
	categories CategoryResolver
	accounts   Accounts
}

func NewService(store TxRunner, categories CategoryResolver, accounts Accounts) *Service {
	return &Service{store: store, categories: categories, accounts: accounts}
}

// AcquisitionInput — input satu-satunya pintu perolehan aset tetap, dipanggil
// dari toggle "Jenis Pembelian: Fixed Asset" di form Pengeluaran (W-10).
type AcquisitionInput struct {
	CategoryID         uint64
	ProjectID          *uint64
	AssetName          string
	AcquisitionDate    time.Time
	AcquisitionCost    domain.Money
	ResidualValue      domain.Money
	UsefulLifeMonths   uint
	DepreciationMethod DepreciationMethod
	PaymentMethod      PaymentMethod
	BankAccountCode    string
	Vendor             string
	Description        string
}

// resolvedAcquisition adalah hasil validasi+resolve SEBELUM transaksi
// dibuka — pola "plan lalu commit" yang sama dengan cost.costPlan.
type resolvedAcquisition struct {
	in             AcquisitionInput
	category       CategoryPolicy
	assetAccountID uint64
	accumDepAcctID uint64
	depExpenseAcct uint64
	paymentAcctID  uint64
	depreciable    domain.Money
}

func (s *Service) plan(ctx context.Context, tenantID uint64, in AcquisitionInput) (*resolvedAcquisition, error) {
	if err := validateAcquisitionInput(in); err != nil {
		return nil, err
	}
	cat, err := s.categories.ResolveCategory(ctx, tenantID, in.CategoryID)
	if err != nil {
		return nil, err
	}
	assetAcctID, err := s.accounts.ResolveAccountID(ctx, tenantID, cat.AssetAccountCode)
	if err != nil {
		return nil, err
	}
	accumID, err := s.accounts.ResolveAccountID(ctx, tenantID, cat.AccumulatedDepreciationAccountCode)
	if err != nil {
		return nil, err
	}
	expID, err := s.accounts.ResolveAccountID(ctx, tenantID, cat.DepreciationExpenseAccountCode)
	if err != nil {
		return nil, err
	}
	payAcctID, err := s.accounts.ValidatePaymentAccountCode(ctx, tenantID, in.BankAccountCode)
	if err != nil {
		return nil, err
	}
	return &resolvedAcquisition{
		in: in, category: cat,
		assetAccountID: assetAcctID, accumDepAcctID: accumID, depExpenseAcct: expID,
		paymentAcctID: payAcctID,
		depreciable:   in.AcquisitionCost.Sub(in.ResidualValue),
	}, nil
}

func validateAcquisitionInput(in AcquisitionInput) error {
	if trimEmpty(in.AssetName) {
		return ErrAssetNameRequired
	}
	if !in.AcquisitionCost.IsWholeRupiah() {
		return ErrAmountFractional
	}
	if in.AcquisitionCost.IsZero() || in.AcquisitionCost.IsNeg() {
		return ErrAmountZeroOrNeg
	}
	if !in.ResidualValue.IsWholeRupiah() {
		return ErrAmountFractional
	}
	if in.ResidualValue.IsNeg() {
		return ErrResidualNegative
	}
	if in.ResidualValue.GreaterThan(in.AcquisitionCost) {
		return ErrResidualExceedsCost
	}
	if in.UsefulLifeMonths == 0 {
		return ErrInvalidUsefulLife
	}
	if !in.DepreciationMethod.Valid() {
		return ErrUnsupportedDepreciationMethod
	}
	if !in.PaymentMethod.Valid() {
		return ErrInvalidPaymentMethod
	}
	if in.PaymentMethod == PaymentMethodPayable {
		return ErrPayableNotSupported
	}
	if trimEmpty(in.BankAccountCode) {
		return ErrBankAccountCodeRequired
	}
	return nil
}

func trimEmpty(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' {
			return false
		}
	}
	return true
}

// AcquisitionPreview — pratinjau tanpa efek samping (tidak menulis apa pun),
// dipakai FE utk menampilkan jurnal & jadwal penyusutan sebelum submit.
type AcquisitionPreview struct {
	DebitAccountCode     string         `json:"debit_account_code"`
	CreditAccountCode    string         `json:"credit_account_code"`
	Amount               domain.Money   `json:"amount"`
	DepreciableAmount    domain.Money   `json:"depreciable_amount"`
	MonthlyDepreciation  domain.Money   `json:"monthly_depreciation_indicative"`
	DepreciationSchedule []domain.Money `json:"depreciation_schedule"`
}

func (s *Service) PreviewAcquisition(ctx context.Context, tenantID uint64, in AcquisitionInput) (*AcquisitionPreview, error) {
	p, err := s.plan(ctx, tenantID, in)
	if err != nil {
		return nil, err
	}
	schedule := depreciationSchedule(p.depreciable, in.UsefulLifeMonths)
	monthly := domain.FromInt(0)
	if len(schedule) > 0 {
		monthly = schedule[0]
	}
	return &AcquisitionPreview{
		DebitAccountCode:     p.category.AssetAccountCode,
		CreditAccountCode:    in.BankAccountCode,
		Amount:               in.AcquisitionCost,
		DepreciableAmount:    p.depreciable,
		MonthlyDepreciation:  monthly,
		DepreciationSchedule: schedule,
	}, nil
}

// AcquireAsset — SATU jalur perolehan aset tetap: memvalidasi, memposting
// jurnal (Dr Aset / Cr Kas-Bank) via ledger.PostingService, lalu mendaftarkan
// baris Register — semuanya dalam SATU transaksi (atomicity).
func (s *Service) AcquireAsset(ctx context.Context, tenantID uint64, in AcquisitionInput) (*FixedAsset, error) {
	p, err := s.plan(ctx, tenantID, in)
	if err != nil {
		return nil, err
	}

	var created *FixedAsset
	err = s.store.InTx(ctx, func(journals JournalWriter, store Store) error {
		desc := fmt.Sprintf("Perolehan aset tetap: %s", in.AssetName)
		lines := []JournalLineInput{
			{AccountID: p.assetAccountID, Debit: in.AcquisitionCost, Credit: domain.FromInt(0), ProjectID: in.ProjectID, Description: desc},
			{AccountID: p.paymentAcctID, Debit: domain.FromInt(0), Credit: in.AcquisitionCost, ProjectID: in.ProjectID, Description: desc},
		}
		journalID, err := journals.CreateJournal(ctx, tenantID, in.AcquisitionDate, desc, lines)
		if err != nil {
			return fmt.Errorf("membuat jurnal perolehan aset tetap: %w", err)
		}
		if err := journals.PostJournal(ctx, tenantID, journalID, true); err != nil {
			return fmt.Errorf("posting jurnal perolehan aset tetap: %w", err)
		}
		asset := &FixedAsset{
			TenantID: tenantID, ProjectID: in.ProjectID, CategoryID: p.category.ID,
			AssetName: in.AssetName, AcquisitionDate: in.AcquisitionDate,
			AcquisitionCost: in.AcquisitionCost, ResidualValue: in.ResidualValue,
			UsefulLifeMonths: in.UsefulLifeMonths, DepreciationMethod: in.DepreciationMethod,
			// Konvensi v1: penyusutan mulai pada tanggal perolehan (perolehan
			// 1 Agustus 2026 → penyusutan mulai bulan Agustus 2026). Ini
			// konvensi ERP standar (full-month-in-period-of-acquisition);
			// tidak ada kebijakan lain yang sudah ada di codebase/dokumen.
			DepreciationStartDate: in.AcquisitionDate,
			Status:                AssetStatusActive,
			AcquisitionJournalID:  journalID,
			Vendor:                in.Vendor,
			Description:           in.Description,
		}
		if err := store.CreateAsset(ctx, asset); err != nil {
			return fmt.Errorf("simpan register aset tetap: %w", err)
		}
		created = asset
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// ── Penyusutan ────────────────────────────────────────────────────────────

// depreciationSchedule membagi depreciable amount ke usefulLifeMonths baris
// via largest-remainder (domain.Money.Allocate) — Σ(schedule) == depreciable
// PERSIS, sisa pembulatan jatuh deterministik ke periode terakhir. Ini
// memakai primitive yang SAMA dengan alokasi biaya proyek (Invariant #3),
// diterapkan ke periode waktu, bukan pool biaya.
func depreciationSchedule(depreciable domain.Money, usefulLifeMonths uint) []domain.Money {
	if usefulLifeMonths == 0 {
		return nil
	}
	weights := make([]decimal.Decimal, usefulLifeMonths)
	one := decimal.NewFromInt(1)
	for i := range weights {
		weights[i] = one
	}
	return depreciable.Allocate(weights)
}

// monthsElapsed menghitung indeks periode 1-based dari bulan mulai penyusutan
// sampai (year, month) — independen dari urutan eksekusi RunDepreciation,
// sehingga catch-up run (menjalankan periode yang terlewat belakangan) tetap
// menghasilkan indeks jadwal yang benar.
func monthsElapsed(start time.Time, year int, month int) int {
	startIdx := start.Year()*12 + int(start.Month())
	targetIdx := year*12 + month
	return targetIdx - startIdx + 1
}

// DepreciationRunResult — ringkasan satu eksekusi RunDepreciation.
type DepreciationRunResult struct {
	Posted  []PostedDepreciation  `json:"posted"`
	Skipped []SkippedDepreciation `json:"skipped"`
}

type PostedDepreciation struct {
	AssetID uint64       `json:"asset_id"`
	Amount  domain.Money `json:"amount"`
}

type SkippedDepreciation struct {
	AssetID uint64 `json:"asset_id"`
	Reason  string `json:"reason"`
}

// RunDepreciation memposting penyusutan bulan (year, month) untuk semua aset
// active milik tenant yang periode itu termasuk dalam jadwalnya dan BELUM
// pernah diposting (INV-FA-1). Setiap aset diposting dalam transaksi
// TERPISAH — satu aset gagal/duplikat tidak membatalkan aset lain dalam
// batch yang sama (tidak ada silent failure: kegagalan tetap dilaporkan di
// Skipped, bukan diam-diam dilewati tanpa jejak).
func (s *Service) RunDepreciation(ctx context.Context, tenantID uint64, year int, month int) (*DepreciationRunResult, error) {
	if month < 1 || month > 12 {
		return nil, ErrInvalidPeriod
	}
	result := &DepreciationRunResult{}

	// Daftar kandidat dibaca sekali di luar transaksi tulis (read-only plan),
	// pola yang sama dengan AcquireAsset.plan().
	var assets []*FixedAsset
	err := s.store.InTx(ctx, func(_ JournalWriter, store Store) error {
		a, err := store.ListActiveAssets(ctx, tenantID)
		if err != nil {
			return err
		}
		assets = a
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("daftar aset aktif: %w", err)
	}

	for _, asset := range assets {
		idx := monthsElapsed(asset.DepreciationStartDate, year, month)
		if idx < 1 {
			result.Skipped = append(result.Skipped, SkippedDepreciation{AssetID: asset.ID, Reason: "periode sebelum tanggal mulai penyusutan"})
			continue
		}
		if uint(idx) > asset.UsefulLifeMonths {
			result.Skipped = append(result.Skipped, SkippedDepreciation{AssetID: asset.ID, Reason: "aset sudah disusutkan penuh (umur ekonomis terlampaui)"})
			continue
		}
		schedule := depreciationSchedule(asset.DepreciableAmount(), asset.UsefulLifeMonths)
		amount := schedule[idx-1]

		cat, err := s.categories.ResolveCategory(ctx, tenantID, asset.CategoryID)
		if err != nil {
			result.Skipped = append(result.Skipped, SkippedDepreciation{AssetID: asset.ID, Reason: err.Error()})
			continue
		}

		posted, skipReason, txErr := s.postOneDepreciation(ctx, tenantID, asset, cat, uint16(year), uint8(month), amount)
		if txErr != nil {
			return nil, txErr
		}
		if skipReason != "" {
			result.Skipped = append(result.Skipped, SkippedDepreciation{AssetID: asset.ID, Reason: skipReason})
			continue
		}
		if posted {
			result.Posted = append(result.Posted, PostedDepreciation{AssetID: asset.ID, Amount: amount})
		}
	}
	return result, nil
}

// postOneDepreciation menjalankan satu transaksi: cek idempotensi (aplikasi
// + fallback unique-constraint DB), posting jurnal, catat garis penyusutan.
func (s *Service) postOneDepreciation(ctx context.Context, tenantID uint64, asset *FixedAsset, cat CategoryPolicy, year uint16, month uint8, amount domain.Money) (posted bool, skipReason string, err error) {
	expAcctID, err := s.accounts.ResolveAccountID(ctx, tenantID, cat.DepreciationExpenseAccountCode)
	if err != nil {
		return false, "", err
	}
	accumAcctID, err := s.accounts.ResolveAccountID(ctx, tenantID, cat.AccumulatedDepreciationAccountCode)
	if err != nil {
		return false, "", err
	}

	txErr := s.store.InTx(ctx, func(journals JournalWriter, store Store) error {
		already, err := store.HasDepreciationLine(ctx, tenantID, asset.ID, year, month)
		if err != nil {
			return err
		}
		if already {
			skipReason = ErrDepreciationAlreadyPosted.Error()
			return nil
		}
		desc := fmt.Sprintf("Penyusutan %s periode %04d-%02d", asset.AssetName, year, month)
		lines := []JournalLineInput{
			{AccountID: expAcctID, Debit: amount, Credit: domain.FromInt(0), ProjectID: asset.ProjectID, Description: desc},
			{AccountID: accumAcctID, Debit: domain.FromInt(0), Credit: amount, ProjectID: asset.ProjectID, Description: desc},
		}
		periodDate := time.Date(int(year), time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		journalID, err := journals.CreateJournal(ctx, tenantID, periodDate, desc, lines)
		if err != nil {
			return fmt.Errorf("membuat jurnal penyusutan aset %d: %w", asset.ID, err)
		}
		if err := journals.PostJournal(ctx, tenantID, journalID, false); err != nil {
			return fmt.Errorf("posting jurnal penyusutan aset %d: %w", asset.ID, err)
		}
		line := &DepreciationLine{
			TenantID: tenantID, FixedAssetID: asset.ID,
			PeriodYear: year, PeriodMonth: month, Amount: amount, JournalEntryID: journalID,
		}
		if err := store.CreateDepreciationLine(ctx, line); err != nil {
			// Unique constraint (INV-FA-1) sebagai penjaga terakhir jika
			// pengecekan HasDepreciationLine di atas kalah race.
			skipReason = fmt.Sprintf("%s (%v)", ErrDepreciationAlreadyPosted.Error(), err)
			return errDuplicateDepreciation
		}
		posted = true
		return nil
	})
	if txErr != nil {
		if txErr == errDuplicateDepreciation {
			return false, skipReason, nil
		}
		return false, "", txErr
	}
	return posted, skipReason, nil
}
