package cost

import (
	"context"
	"fmt"
	"time"

	"esaproperti/internal/domain"
)

// ── Store interfaces ──────────────────────────────────────────────────────────

// AccountResolver resolves an account code to its database ID and display name.
// A single DB round-trip returns both; callers use the ID for journal creation
// and the name for preview display.
type AccountResolver interface {
	ResolveAccount(ctx context.Context, tenantID uint64, code string) (id uint64, name string, err error)
}

// JournalWriter creates and posts accounting journals.
// The production implementation wraps *ledger.PostingService.
// Using a custom interface here keeps the cost package free of a hard import on ledger types.
type JournalWriter interface {
	CreateJournal(ctx context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (journalID uint64, err error)
	// PostJournal memposting jurnal; cashOut menyatakan bahwa posting ini
	// menggerakkan kas keluar sehingga wajib berdokumen (INV-DOC-1).
	//
	// Sengaja boolean, bukan kode jenis dokumen: biaya proyek yang dibayar kas
	// SELALU BKK (kas perusahaan sendiri), dan menyalin literal "cash_out" ke
	// package ini berarti dua daftar kode yang bisa berselisih diam-diam.
	// Pemetaan boolean → jenis dokumen tinggal di adapter yang memang sudah
	// mengenal ledger.
	PostJournal(ctx context.Context, tenantID, journalID uint64, cashOut bool) error
}

// CostStore persists CostEntry records (tenant-scoped).
type CostStore interface {
	CreateCostEntry(ctx context.Context, e *CostEntry) error
	FindCostEntryByID(ctx context.Context, tenantID, id uint64) (*CostEntry, error)
	ListCostEntriesByProject(ctx context.Context, tenantID, projectID uint64) ([]*CostEntry, error)
	ListCostEntriesByUnit(ctx context.Context, tenantID, unitID uint64) ([]*CostEntry, error)
}

// TxRunner menjalankan fn dalam SATU transaksi database dengan kolaborator yang
// terikat transaksi tersebut (W-3.0 B-3).
//
// CreateCostEntry menulis dua kali — jurnal draft lalu baris cost entry. Tanpa
// transaksi, gagal di tulisan kedua meninggalkan jurnal draft yatim yang tidak
// pernah bisa ditelusuri ke biaya mana pun. PostCostEntry adalah jalur kas
// keluar O-1 (bayar tanah/subkontraktor/operasional), tempat dokumen BKK terbit
// di W-3.2 — dokumen hanya boleh ditumpuk di atas transaksi yang atomik.
//
// nil = jalur lama tanpa transaksi (unit test in-memory) — perilaku dipertahankan.
type TxRunner interface {
	InTx(ctx context.Context, fn func(journals JournalWriter, store CostStore) error) error
}

// BudgetItemLookup memvalidasi referensi budget_item_id dari sebuah CostEntry.
// Implementasi production: budget.GORMRepository via adapter di cost/handler.go.
// nil berarti tidak ada validasi (test tanpa tautan RAB, atau fitur dinonaktifkan).
type BudgetItemLookup interface {
	// FindItemForCostValidation memeriksa kepemilikan tenant/project dan mengembalikan
	// CostCategory yang dipetakan (alias construction→hard sudah diterapkan).
	// Error: ErrBudgetItemNotFound, ErrBudgetItemProjectMismatch, ErrBudgetItemCategoryMismatch.
	FindItemForCostValidation(ctx context.Context, tenantID, projectID, itemID uint64) (domain.CostCategory, error)
}

// PaymentAccountValidator membuktikan bahwa sebuah kode akun BOLEH dipakai
// sebagai sumber pembayaran: ada di COA tenant, aktif, bertipe aset, dan
// berkategori kas/bank (W-10 T-5).
//
// Sebelum ini bank_account_code hanya dicek "tidak kosong", lalu di-resolve
// menjadi account_id apa pun yang kodenya cocok — sehingga "4-1000 Pendapatan"
// bisa dikreditkan sebagai kalau-kalau itu rekening bank, menghasilkan jurnal
// yang balanced tapi mengarang pendapatan. Validasi di frontend saja tidak
// cukup: yang menulis jurnal adalah backend.
//
// nil = tanpa validasi (unit test in-memory lama); produksi selalu memasangnya.
type PaymentAccountValidator interface {
	ValidatePaymentAccountCode(ctx context.Context, tenantID uint64, code string) error
}

// UnitProjectResolver menjawab proyek pemilik sebuah unit. Dipakai untuk satu
// hal: menolak biaya yang di-tag ke unit milik proyek LAIN — kesalahan yang
// sebelumnya lolos diam-diam dan mencemari HPP unit yang salah.
type UnitProjectResolver interface {
	ResolveUnitProject(ctx context.Context, tenantID, unitID uint64) (projectID uint64, err error)
}

// CostQueryer derives accumulated cost totals from posted journal lines.
// Sumber kebenaran = JURNAL; tidak membaca angka dari cost_entries.amount.
type CostQueryer interface {
	// AccumulatedByProject sums all Persediaan debit lines tagged to the project.
	AccumulatedByProject(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error)
	// AccumulatedByUnit sums Persediaan debit lines tagged to a specific unit.
	AccumulatedByUnit(ctx context.Context, tenantID, projectID, unitID uint64) (domain.UnitCostBreakdown, error)
	// AccumulatedProjectWide sums Persediaan debit lines tagged to the project but NOT to any unit.
	AccumulatedProjectWide(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error)
}

// ── Request/response types ────────────────────────────────────────────────────

// CreateCostEntryRequest is the input for recording a development cost.
type CreateCostEntryRequest struct {
	ProjectID       uint64              // 0 = tanpa project (HANYA sah untuk tier overhead)
	UnitID          *uint64             // wajib untuk tier direct; harus nil untuk shared/overhead
	PhaseID         *uint64             // optional phase tag (butuh project)
	Category        domain.CostCategory
	CostTier        domain.CostTier     // kosong = di-infer (backward compat, lihat resolveTier)
	Amount          domain.Money
	PaymentMethod   PaymentMethod
	BankAccountCode string              // required when PaymentMethod == "bank" (e.g. "1-1300")
	Date            time.Time
	Vendor          string
	Description     string
	BudgetItemID    *uint64             // opsional: tautkan ke budget item RAB (butuh project)
	// ExpenseTypeID (W-10): jenis pengeluaran OPERASIONAL dari master
	// expense_types. Bila terisi, akun debit berasal dari master — bukan dari
	// enum Category — dan transaksi wajib bertier overhead tanpa tautan RAB.
	// nil = jalur biaya proyek dengan taksonomi CostCategory (tidak berubah).
	ExpenseTypeID *uint64
}

// resolveTier mengembalikan CostTier efektif sebuah request. Request lama tanpa
// cost_tier (API backward compat) di-infer dengan aturan deterministik yang SAMA
// dengan backfill migration 000035:
//
//	kategori beban (marketing/other) → overhead
//	unit_id terisi                   → direct
//	unit_id kosong                   → shared
//
// Tier eksplisit dikembalikan apa adanya (divalidasi terhadap matrix di validate).
func resolveTier(req CreateCostEntryRequest) domain.CostTier {
	if req.CostTier != "" {
		return req.CostTier
	}
	if req.Category.IsExpense() {
		return domain.CostTierOverhead
	}
	if req.UnitID != nil {
		return domain.CostTierDirect
	}
	return domain.CostTierShared
}

// JournalPreviewLine is one line of the dry-run journal preview.
// Frontend hanya menampilkan data ini; tidak menghitung kode akun sendiri.
type JournalPreviewLine struct {
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	Debit       string `json:"debit"`  // "0" jika sisi kredit
	Credit      string `json:"credit"` // "0" jika sisi debit
}

// ── Service ───────────────────────────────────────────────────────────────────

// UnitProductPolicyResolver menjawab kebijakan produk sebuah unit dari master
// Product Catalog. Dipakai untuk satu hal saja di package ini: memastikan biaya
// yang dikapitalisasi ke sebuah unit memang unit yang ikut HPP.
//
// Implementasi produksi: project.GORMRepository (dipasang lewat handler).
type UnitProductPolicyResolver interface {
	ResolveUnitProductPolicy(ctx context.Context, tenantID, unitID uint64) (domain.ProductPolicy, error)
}

type Service struct {
	accounts    AccountResolver
	journals    JournalWriter
	store       CostStore
	query       CostQueryer
	budgetItems BudgetItemLookup // nil = tidak ada validasi RAB
	// unitPolicies (Product Catalog): nil hanya pada unit test in-memory lama —
	// produksi selalu memasangnya (SetUnitPolicyResolver di handler).
	unitPolicies UnitProductPolicyResolver
	// tx (W-3.0): seam transaksi. nil = tanpa transaksi (unit test in-memory).
	tx TxRunner
	// expenseTypes (W-10): master jenis pengeluaran operasional. nil = jalur
	// operasional tidak tersedia (request ber-expense_type_id ditolak, bukan
	// diam-diam jatuh ke akun default).
	expenseTypes ExpenseTypeResolver
	// paymentAccounts (W-10 T-5): pembuktian akun kas/bank. nil = tanpa
	// validasi (unit test in-memory lama).
	paymentAccounts PaymentAccountValidator
	// unitProjects (W-10): pembuktian unit milik proyek yang dipilih.
	unitProjects UnitProjectResolver
	// expenses (W-10): pembaca daftar transaksi pengeluaran.
	expenses ExpenseReader
}

// SetUnitPolicyResolver memasang seam Product Catalog untuk validasi biaya
// direct (wiring produksi).
func (s *Service) SetUnitPolicyResolver(r UnitProductPolicyResolver) { s.unitPolicies = r }

// SetTxRunner memasang seam transaksi (W-3.0). Wajib pada wiring produksi:
// tanpa ini CreateCostEntry/PostCostEntry bisa menghasilkan tulisan parsial.
func (s *Service) SetTxRunner(tr TxRunner) { s.tx = tr }

// SetExpenseTypeResolver memasang master jenis pengeluaran W-10.
func (s *Service) SetExpenseTypeResolver(r ExpenseTypeResolver) { s.expenseTypes = r }

// SetPaymentAccountValidator memasang pembuktian akun kas/bank (T-5).
func (s *Service) SetPaymentAccountValidator(v PaymentAccountValidator) { s.paymentAccounts = v }

// SetUnitProjectResolver memasang pembuktian unit ↔ proyek.
func (s *Service) SetUnitProjectResolver(r UnitProjectResolver) { s.unitProjects = r }

// inTx menjalankan fn dengan kolaborator terikat transaksi.
// Tanpa seam transaksi, fn berjalan dengan kolaborator biasa (perilaku lama).
func (s *Service) inTx(ctx context.Context, fn func(JournalWriter, CostStore) error) error {
	if s.tx == nil {
		return fn(s.journals, s.store)
	}
	return s.tx.InTx(ctx, fn)
}

// NewService membuat Service baru.
// budgetItems boleh nil: cost entry tanpa tautan RAB tetap valid.
func NewService(accounts AccountResolver, journals JournalWriter, store CostStore, query CostQueryer, budgetItems BudgetItemLookup) *Service {
	return &Service{accounts: accounts, journals: journals, store: store, query: query, budgetItems: budgetItems}
}

// ── Pure helpers ──────────────────────────────────────────────────────────────

// resolveDebitCreditCodes returns the two COA codes that a cost entry will use.
// The debit side follows the tier (Increment 2, taxonomy = source of truth):
//
//	direct/shared (kapitalisasi) → Persediaan 1-3xxx (Category.InventoryAccountCode)
//	overhead (beban periode)     → Beban 5-3000/5-4000 (Category.ExpenseAccountCode)
//
// Pure function: no DB, no side effects. Called by both CreateCostEntry and PreviewCostEntry
// so the same logic determines the accounts in both paths — DRY guarantee.
// Caller must pass the EFFECTIVE tier (resolveTier), already matrix-validated.
func resolveDebitCreditCodes(tier domain.CostTier, req CreateCostEntryRequest) (debitCode, creditCode string) {
	if tier == domain.CostTierOverhead {
		debitCode = req.Category.ExpenseAccountCode()
	} else {
		debitCode = req.Category.InventoryAccountCode()
	}
	// Hutang Usaha berasal dari registry peran akun ledger, bukan literal.
	creditCode = payableAccountCode()
	if req.PaymentMethod == PaymentMethodBank {
		creditCode = req.BankAccountCode
	}
	return debitCode, creditCode
}

// costPlan adalah hasil resolusi sebuah request menjadi rencana jurnal: tier
// efektif, akun debit/kredit, dan (bila jalur operasional) kebijakan jenis
// pengeluaran yang dipakai. Preview dan Create SAMA-SAMA memakainya sehingga
// pratinjau yang dilihat user tidak mungkin berbeda dari jurnal yang terbit.
type costPlan struct {
	tier        domain.CostTier
	debitCode   string
	creditCode  string
	expenseType *ExpenseTypePolicy // nil = jalur biaya proyek (taksonomi CostCategory)
}

// plan memvalidasi request lalu menentukan rencana jurnalnya.
func (s *Service) plan(ctx context.Context, tenantID uint64, req CreateCostEntryRequest) (costPlan, error) {
	tier := resolveTier(req)
	if err := s.validate(ctx, tenantID, tier, req); err != nil {
		return costPlan{}, err
	}

	debitCode, creditCode := resolveDebitCreditCodes(tier, req)
	p := costPlan{tier: tier, debitCode: debitCode, creditCode: creditCode}

	if req.ExpenseTypeID == nil {
		return p, nil
	}

	// ── Jalur OPERASIONAL (W-10) ────────────────────────────────────────────
	// Jenis pengeluaran hanya berlaku untuk beban periode. Membiarkannya pada
	// tier direct/shared berarti akun beban dipakai untuk mengkapitalisasi
	// persediaan — pool HPP akan membaca angka yang tak pernah dianggarkan.
	if tier != domain.CostTierOverhead {
		return costPlan{}, ErrExpenseTypeNotForProjectCost
	}
	// Item RAB dianggarkan dalam taksonomi CostCategory; pengeluaran kantor
	// tidak pernah menjadi realisasi salah satu item itu.
	if req.BudgetItemID != nil {
		return costPlan{}, ErrExpenseTypeWithBudgetItem
	}
	if s.expenseTypes == nil {
		// Fail-closed: resolver tidak terpasang bukan alasan memakai akun default.
		return costPlan{}, ErrExpenseTypeUnknown
	}
	policy, err := s.expenseTypes.ResolveExpenseType(ctx, tenantID, *req.ExpenseTypeID)
	if err != nil {
		return costPlan{}, err
	}
	// INV-EXP-2 (turunan BD-2 "project tag ≠ realisasi RAB").
	// budget.GetRealisasiByProject menghitung realisasi RAB per kategori dengan
	// membaca LEDGER: seluruh baris pada akun taksonomi biaya yang ber-tag
	// project_id. Jadi begitu sebuah pengeluaran operasional yang akunnya
	// kebetulan 5-3000/5-4000/1-3xxx diberi tag proyek, ia langsung terhitung
	// sebagai realisasi RAB — persis yang dilarang BD-2. Menolaknya di depan
	// membuat BD-2 berlaku by construction, tanpa mengubah satu pun pembaca
	// laporan yang sudah dipakai W-5/W-6.
	if req.ProjectID != 0 && IsCostTaxonomyAccount(policy.ExpenseAccountCode) {
		return costPlan{}, fmt.Errorf("%w: jenis %q → akun %s", ErrExpenseTypeTaxonomyAccount, policy.Name, policy.ExpenseAccountCode)
	}
	p.debitCode = policy.ExpenseAccountCode
	p.expenseType = &policy
	return p, nil
}

// journalDescription menyusun deskripsi jurnal sebuah rencana biaya.
func (p costPlan) journalDescription(req CreateCostEntryRequest) string {
	if p.expenseType != nil {
		return fmt.Sprintf("Beban %s — %s", p.expenseType.Name, req.Description)
	}
	descPrefix := "Kapitalisasi"
	if p.tier == domain.CostTierOverhead {
		descPrefix = "Beban"
	}
	return fmt.Sprintf("%s %s — %s", descPrefix, req.Category, req.Description)
}

// validate runs all input checks shared between CreateCostEntry and PreviewCostEntry.
// tier adalah tier efektif dari resolveTier (bukan req.CostTier mentah).
// Returns the first validation error found, or nil if the request is valid.
func (s *Service) validate(ctx context.Context, tenantID uint64, tier domain.CostTier, req CreateCostEntryRequest) error {
	if !tier.Valid() {
		return ErrInvalidCostTier
	}
	if !req.Category.Valid() {
		return ErrInvalidCategory
	}
	// Matrix CostTier × CostCategory (Increment 2): direct/shared hanya kategori
	// kapitalisasi; overhead hanya kategori beban. Kombinasi lain merusak HPP/laba.
	if !tier.AllowsCategory(req.Category) {
		return ErrTierCategoryMismatch
	}
	// Konsistensi tier ↔ atribusi (selaras dengan cara allocation engine membaca
	// unit_id pada journal lines: direct = unit line, shared = pool tanpa unit).
	switch tier {
	case domain.CostTierDirect:
		if req.UnitID == nil {
			return ErrUnitRequiredForDirect
		}
		// Product Catalog (fail-closed): biaya direct MENGKAPITALISASI ke
		// Persediaan unit dan baru lepas menjadi HPP saat BAST. Unit non-properti
		// tidak pernah mengakui HPP (domain.ProductPolicy.ParticipatesInHPP),
		// jadi biaya yang ditempel ke sana akan mengendap selamanya di neraca —
		// persediaan hantu. Tolak sebelum jurnal dibuat.
		if s.unitPolicies != nil {
			policy, err := s.unitPolicies.ResolveUnitProductPolicy(ctx, tenantID, *req.UnitID)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrUnitProductPolicyUnresolved, err)
			}
			if !policy.ParticipatesInHPP() {
				return fmt.Errorf("%w: produk %q berkategori %s", ErrUnitNotHPPEligible, policy.Code, policy.Category)
			}
		}
		// Unit harus milik proyek yang dipilih. Tanpa cek ini biaya bisa
		// dikapitalisasi ke unit proyek LAIN: HPP unit itu naik tanpa pernah
		// ada anggarannya, dan laporan biaya kedua proyek sama-sama salah.
		if req.ProjectID != 0 && s.unitProjects != nil {
			ownerProject, err := s.unitProjects.ResolveUnitProject(ctx, tenantID, *req.UnitID)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrUnitNotInProject, err)
			}
			if ownerProject != req.ProjectID {
				return fmt.Errorf("%w: unit %d milik proyek %d", ErrUnitNotInProject, *req.UnitID, ownerProject)
			}
		}
	case domain.CostTierShared, domain.CostTierOverhead:
		if req.UnitID != nil {
			return ErrUnitNotAllowedForTier
		}
	}
	// Project: wajib untuk direct/shared; overhead boleh tanpa project (Tenant-level)
	// atau di-tag ke project sebagai cost center reporting.
	if req.ProjectID == 0 {
		if tier != domain.CostTierOverhead {
			return ErrProjectRequired
		}
		if req.PhaseID != nil {
			return ErrPhaseRequiresProject
		}
		if req.BudgetItemID != nil {
			return ErrBudgetLinkRequiresProject
		}
	}
	if req.Amount.IsZero() || req.Amount.IsNeg() {
		return ErrCostAmountZeroOrNeg
	}
	if !req.Amount.IsWholeRupiah() {
		return ErrCostAmountFractional
	}
	if !req.PaymentMethod.Valid() {
		return ErrInvalidPaymentMethod
	}
	if req.PaymentMethod == PaymentMethodBank {
		if req.BankAccountCode == "" {
			return ErrBankAccountCodeRequired
		}
		// T-5: kode akun pembayaran harus TERBUKTI kas/bank yang aktif.
		// Ini pengetatan kontrak yang disengaja — payload yang dulu diterima
		// diam-diam (mis. mengkreditkan akun pendapatan sebagai "bank") kini
		// ditolak, karena jurnal yang dihasilkannya balanced tetapi bohong.
		if s.paymentAccounts != nil {
			if err := s.paymentAccounts.ValidatePaymentAccountCode(ctx, tenantID, req.BankAccountCode); err != nil {
				return fmt.Errorf("%w: %v", ErrNotCashBankAccount, err)
			}
		}
	}

	// Validasi budget_item_id (opsional). Sejak Increment 2 juga berlaku untuk
	// kategori beban: budget item marketing/other hanya cocok dengan cost entry
	// kategori sama (yang oleh matrix di atas pasti tier overhead).
	if req.BudgetItemID != nil && s.budgetItems != nil {
		mappedCC, err := s.budgetItems.FindItemForCostValidation(ctx, tenantID, req.ProjectID, *req.BudgetItemID)
		if err != nil {
			return err
		}
		if mappedCC != req.Category {
			return ErrBudgetItemCategoryMismatch
		}
	}
	return nil
}

// resolveAccounts looks up the (id, name) pair for the debit and credit accounts.
// Called by both CreateCostEntry (uses IDs) and PreviewCostEntry (uses IDs + names).
type resolvedAccount struct {
	id   uint64
	code string
	name string
}

func (s *Service) resolveAccounts(ctx context.Context, tenantID uint64, debitCode, creditCode string) (debit, credit resolvedAccount, err error) {
	debitID, debitName, err := s.accounts.ResolveAccount(ctx, tenantID, debitCode)
	if err != nil {
		return resolvedAccount{}, resolvedAccount{}, fmt.Errorf("%w: debit %q", ErrAccountNotFound, debitCode)
	}
	creditID, creditName, err := s.accounts.ResolveAccount(ctx, tenantID, creditCode)
	if err != nil {
		return resolvedAccount{}, resolvedAccount{}, fmt.Errorf("%w: kredit %q", ErrAccountNotFound, creditCode)
	}
	return resolvedAccount{debitID, debitCode, debitName},
		resolvedAccount{creditID, creditCode, creditName},
		nil
}

// ── Service methods ───────────────────────────────────────────────────────────

// PreviewCostEntry runs all validations and resolves accounts exactly as CreateCostEntry
// would, but does NOT create any journal entry or CostEntry record.
// The returned lines show the exact journal that Create would produce for the same input.
// Frontend must call this instead of hardcoding account codes.
func (s *Service) PreviewCostEntry(ctx context.Context, tenantID uint64, req CreateCostEntryRequest) ([]JournalPreviewLine, error) {
	p, err := s.plan(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}

	debit, credit, err := s.resolveAccounts(ctx, tenantID, p.debitCode, p.creditCode)
	if err != nil {
		return nil, err
	}

	zero := domain.Zero.String()
	amount := req.Amount.String()

	return []JournalPreviewLine{
		{AccountCode: debit.code, AccountName: debit.name, Debit: amount, Credit: zero},
		{AccountCode: credit.code, AccountName: credit.name, Debit: zero, Credit: amount},
	}, nil
}

// CreateCostEntry validates, posts a DRAFT journal, and persists the CostEntry.
// The journal is initially a draft; call PostCostEntry separately to finalize it.
//
// Invariant #2: amount must be whole rupiah — service rejects fractional amounts.
// Invariant: amount must be positive.
// Invariant (Increment 2): direct/shared diposting ke Persediaan (1-3xxx), TIDAK
// ke beban; overhead diposting ke beban (5-3000/5-4000), TIDAK ke Persediaan —
// sehingga pool HPP/alokasi (yang membaca 1-3xxx) tidak pernah tercemar overhead.
func (s *Service) CreateCostEntry(ctx context.Context, tenantID uint64, req CreateCostEntryRequest) (*CostEntry, error) {
	return s.createCostEntry(ctx, tenantID, req, false)
}

// CreateAndPostCostEntry mencatat DAN memposting dalam SATU transaksi (W-10 §7).
//
// Jalur dua panggilan (create lalu post) meninggalkan jendela di mana jurnal
// draft sudah ada tetapi kas belum tercatat keluar; bila proses mati di antara
// keduanya, yang tertinggal adalah biaya yang tak pernah masuk buku besar dan
// hanya bisa ditemukan lewat query manual. Untuk transaksi pengeluaran yang
// memang sudah terjadi (uangnya sudah keluar), tidak ada tahap "draft" yang
// bermakna. Satu transaksi database: jurnal + cost entry + posting + dokumen
// BKK sukses bersama, atau tidak terjadi sama sekali.
func (s *Service) CreateAndPostCostEntry(ctx context.Context, tenantID uint64, req CreateCostEntryRequest) (*CostEntry, error) {
	return s.createCostEntry(ctx, tenantID, req, true)
}

func (s *Service) createCostEntry(ctx context.Context, tenantID uint64, req CreateCostEntryRequest, postNow bool) (*CostEntry, error) {
	p, err := s.plan(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	tier := p.tier

	debit, credit, err := s.resolveAccounts(ctx, tenantID, p.debitCode, p.creditCode)
	if err != nil {
		return nil, err
	}

	// Build journal lines (same logic as preview; only IDs differ from names).
	// project tag nil untuk overhead Tenant-level (tanpa project).
	var pid *uint64
	if req.ProjectID != 0 {
		p := req.ProjectID
		pid = &p
	}
	lines := []JournalLineInput{
		{
			AccountID:   debit.id,
			Debit:       req.Amount,
			Credit:      domain.Zero,
			ProjectID:   pid,
			PhaseID:     req.PhaseID,
			UnitID:      req.UnitID,
			Description: req.Description,
		},
		{
			AccountID:   credit.id,
			Debit:       domain.Zero,
			Credit:      req.Amount,
			ProjectID:   pid,
			PhaseID:     req.PhaseID,
			UnitID:      req.UnitID,
			Description: req.Description,
		},
	}

	desc := p.journalDescription(req)

	// W-3.0 (B-3): jurnal draft dan baris cost entry dalam SATU transaksi.
	// W-10 (§7): bila postNow, posting dan penerbitan dokumen BKK ikut di dalam
	// transaksi yang sama — tidak ada keadaan "jurnal terposting tapi cost entry
	// gagal" atau "BKK terbit tapi jurnalnya di-rollback".
	var e *CostEntry
	err = s.inTx(ctx, func(journals JournalWriter, store CostStore) error {
		journalID, err := journals.CreateJournal(ctx, tenantID, req.Date, desc, lines)
		if err != nil {
			return fmt.Errorf("buat jurnal biaya: %w", err)
		}
		entry := &CostEntry{
			TenantID:        tenantID,
			ProjectID:       pid,
			UnitID:          req.UnitID,
			PhaseID:         req.PhaseID,
			Category:        req.Category,
			ExpenseTypeID:   req.ExpenseTypeID,
			CostTier:        tier,
			Amount:          req.Amount,
			PaymentMethod:   req.PaymentMethod,
			BankAccountCode: req.BankAccountCode,
			Date:            req.Date,
			Vendor:          req.Vendor,
			Description:     req.Description,
			JournalEntryID:  journalID,
			BudgetItemID:    req.BudgetItemID,
		}
		if err := store.CreateCostEntry(ctx, entry); err != nil {
			return fmt.Errorf("simpan cost entry: %w", err)
		}
		if postNow {
			if err := journals.PostJournal(ctx, tenantID, journalID, req.PaymentMethod == PaymentMethodBank); err != nil {
				return fmt.Errorf("posting jurnal biaya: %w", err)
			}
		}
		e = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

// PostCostEntry marks the associated journal entry as posted (immutable).
// Call this after reviewing the draft to finalize the cost entry.
//
// W-3.0 (B-3): pencarian dan posting dalam satu transaksi — inilah titik di mana
// kas benar-benar bergerak (jalur O-1) dan tempat dokumen BKK terbit di W-3.2,
// sesuai D-W3-2 (nomor dialokasikan saat posting, bukan saat draft dibuat).
func (s *Service) PostCostEntry(ctx context.Context, tenantID, id uint64) error {
	return s.inTx(ctx, func(journals JournalWriter, store CostStore) error {
		e, err := store.FindCostEntryByID(ctx, tenantID, id)
		if err != nil {
			return err
		}
		// Hanya biaya yang dibayar kas/bank yang menggerakkan kas; yang berstatus
		// payable baru menaikkan utang usaha (kasnya bergerak nanti, lewat jalur
		// pembayaran utang sendiri).
		return journals.PostJournal(ctx, tenantID, e.JournalEntryID, e.PaymentMethod == PaymentMethodBank)
	})
}

// GetCostEntry retrieves a single CostEntry (tenant-scoped).
func (s *Service) GetCostEntry(ctx context.Context, tenantID, id uint64) (*CostEntry, error) {
	return s.store.FindCostEntryByID(ctx, tenantID, id)
}

// ListByProject returns all CostEntry records for a project.
func (s *Service) ListByProject(ctx context.Context, tenantID, projectID uint64) ([]*CostEntry, error) {
	return s.store.ListCostEntriesByProject(ctx, tenantID, projectID)
}

// ListByUnit returns all CostEntry records tagged to a specific unit.
func (s *Service) ListByUnit(ctx context.Context, tenantID, unitID uint64) ([]*CostEntry, error) {
	return s.store.ListCostEntriesByUnit(ctx, tenantID, unitID)
}

// AccumulatedByProject returns the total capitalized cost for a project, broken down by category.
// Source of truth: posted journal_lines (ledger), NOT cost_entries.amount.
func (s *Service) AccumulatedByProject(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error) {
	return s.query.AccumulatedByProject(ctx, tenantID, projectID)
}

// AccumulatedByUnit returns the total capitalized cost attributed to a specific unit.
func (s *Service) AccumulatedByUnit(ctx context.Context, tenantID, projectID, unitID uint64) (domain.UnitCostBreakdown, error) {
	return s.query.AccumulatedByUnit(ctx, tenantID, projectID, unitID)
}

// AccumulatedProjectWide returns project-level costs NOT tagged to any unit.
// Phase 5 uses this as the pool to allocate across units.
func (s *Service) AccumulatedProjectWide(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error) {
	return s.query.AccumulatedProjectWide(ctx, tenantID, projectID)
}
