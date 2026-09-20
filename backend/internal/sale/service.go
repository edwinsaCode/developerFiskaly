package sale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/project"
	"esaproperti/internal/receivable"
	"esaproperti/internal/scheme"
)

// Kode akun kredit untuk penerimaan pembayaran pelanggan.
const (
	accountCodeUangMuka = "2-2000" // Uang Muka Penjualan (kewajiban) — sebelum BAST
	accountCodePiutang  = "1-2000" // Piutang Usaha — pelunasan setelah BAST
	// accountCodeBankFeeExpense (UAT 2026-09-03, Rule #5): provisi/biaya admin
	// bank yang dipotong saat pencairan KPR, ditanggung developer — BUKAN
	// titipan customer (K-1/2-2400 tidak berubah). Lihat preparePaymentFunded.
	accountCodeBankFeeExpense = "5-3200"
)

// ── Interfaces ────────────────────────────────────────────────────────────────

// AccountFinder melihat ID akun berdasarkan kode COA (tenant-scoped).
type AccountFinder interface {
	FindAccountIDByCode(ctx context.Context, tenantID uint64, code string) (uint64, error)
	// ValidateCashBankAccount memastikan code adalah akun pembayaran yang valid:
	// ada, milik tenant, aktif, kategori cash/bank (COA-driven). Mengembalikan
	// ErrPaymentAccountNotFound / ErrPaymentAccountInactive / ErrInvalidBankAccount.
	ValidateCashBankAccount(ctx context.Context, tenantID uint64, code string) error
}

// JournalWriter membuat dan memposting satu jurnal.
// Digunakan untuk Event 2 (termin). Event 3+4 menggunakan BASTAtomicWriter.
type JournalWriter interface {
	CreateJournal(ctx context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (uint64, error)
	PostJournal(ctx context.Context, tenantID, journalID uint64) error
}

// UnitReader menyediakan data unit yang diperlukan untuk memproses penjualan.
type UnitReader interface {
	FindUnitSaleInfo(ctx context.Context, tenantID, unitID uint64) (*UnitSaleInfo, error)
}

// TerminStore mengelola TerminPayment dan menyediakan Σ termin per unit.
type TerminStore interface {
	SaveTermin(ctx context.Context, t *TerminPayment) error
	SumTerminsByUnit(ctx context.Context, tenantID, unitID uint64) (domain.Money, error)
	// SumTerminsByUnitAndCreditAccount (7C, UAT 2026-09-07) menjumlahkan
	// penerimaan sebuah unit yang dikreditkan ke SATU akun tertentu — dasar
	// menghitung sisa Dana Jaminan Bank secara independen dari Piutang Usaha
	// (bukan estimasi ulang, langsung dari termin_payments.credit_account_code
	// yang append-only).
	SumTerminsByUnitAndCreditAccount(ctx context.Context, tenantID, unitID uint64, creditAccountCode string) (domain.Money, error)
	// ListTerminsByUnit mengembalikan semua penerimaan (termin) sebuah unit.
	ListTerminsByUnit(ctx context.Context, tenantID, unitID uint64) ([]*TerminPayment, error)
	// FindSaleRecord mengembalikan SaleRecord unit (bukti BAST) atau
	// ErrSaleRecordNotFound bila unit belum BAST. Tenant-scoped (Invariant #6).
	FindSaleRecord(ctx context.Context, tenantID, unitID uint64) (*SaleRecord, error)
	// FindTerminByIdempotencyKey mengembalikan termin dengan idempotency_key
	// tersebut, atau ErrTerminNotFound. Untuk proteksi double-submit.
	FindTerminByIdempotencyKey(ctx context.Context, tenantID uint64, key string) (*TerminPayment, error)
}

// UnitCostProvider menghitung total biaya akumulasi per unit dari engine Phase 5.
// Hasil Total = Direct + Allocated (Invariant #4).
type UnitCostProvider interface {
	GetUnitCost(ctx context.Context, tenantID, projectID, unitID uint64) (domain.UnitCostBreakdown, error)
}

// BASTAtomicWriter mengeksekusi Event 3+4 + update status unit dalam satu DB transaction.
// Atomik: jika salah satu gagal, semua di-rollback (tidak ada pendapatan tanpa HPP).
type BASTAtomicWriter interface {
	Execute(ctx context.Context, params BASTAtomicParams) (*SaleRecord, error)
}

// HandoverWriter mengeksekusi serah terima fisik (sold→occupied +
// handed_over_at) dalam satu DB transaction kecil — TANPA jurnal, TANPA HPP
// (Temuan #7: pisah dari BASTAtomicWriter, yang tetap murni jalur finansial).
type HandoverWriter interface {
	MarkPhysicalHandover(ctx context.Context, params PhysicalHandoverParams) (*SaleRecord, error)
}

// LandAkadPreparer (kelebihan-tanah-booking-integration-2026-08): resolves +
// builds the Kelebihan Tanah journal lines for a bundled Akad WITHOUT
// executing the write — implemented by *land.Service.PrepareBundledAkad.
// Kept as a narrow interface (not a direct *land.Service field) so unit
// tests can fake it without wiring the whole land package.
type LandAkadPreparer interface {
	PrepareBundledAkad(ctx context.Context, tenantID uint64, req land.PrepareBundledAkadRequest) (land.RecordAkadParams, error)
}

// ── Service ───────────────────────────────────────────────────────────────────

type Service struct {
	accounts     AccountFinder
	journals     JournalWriter
	units        UnitReader
	store        TerminStore
	costProvider UnitCostProvider
	bastWriter   BASTAtomicWriter
	// handoverWriter (Temuan #7, opsional via WithHandoverWriter): eksekutor
	// serah terima fisik. Bila nil, RecordPhysicalHandover menolak fail-closed
	// (ErrHandoverWriterNotConfigured) — bukan diam-diam no-op.
	handoverWriter HandoverWriter
	contracts      ContractStore // opsional; wajib untuk PaymentSchedule methods
	// Dependensi orkestrasi ReceivePayment (opsional; di-set dari wiring layer).
	receiptGen     ReceiptGenerator     // auto-kwitansi
	shortfallInv   ShortfallInvoicer    // R1: auto-invoice kekurangan (kebijakan tenant)
	invoiceUpdater InvoiceStatusUpdater // sinkron status invoice
	// committer mempersist SATU penerimaan (jurnal+termin+alokasi+cache+kwitansi).
	// Produksi: GORMRepository.CommitPayment (atomik, satu transaksi). Bila nil,
	// NewService memasang defaultCommitter (berbasis interface, non-atomik) —
	// cukup untuk unit test in-memory.
	committer PaymentCommitter
	// hppResolver menentukan metode HPP saat BAST (budgeted vs actual) + draft
	// snapshot RAB. Opsional: bila nil, RecordBAST memakai costProvider (metode
	// actual legacy) — menjaga kompatibilitas test in-memory yang ada.
	hppResolver HPPResolver
	// ── Payment Scheme seam (Increment 3, opsional via WithSchemeFlow) ────────
	// Bila nil, seluruh alur berperilaku legacy (tanpa gate/state/enforcement).
	schemeFlow     SchemeFlowStore
	schemeRegistry *scheme.Registry
	parties        PartyLookup
	// unitTransitioner memproyeksikan lifecycle unit dari event sale (Increment 6
	// D3, opsional). Best-effort: kegagalan proyeksi TIDAK membatalkan operasi sale.
	unitTransitioner UnitTransitioner
	// bookings: fitur Booking (Increment 7, opsional via WithBookingStore).
	bookings BookingStore
	// finalizedHPP (P0-4 D2, opsional): sumber act_HPP finalized pasca-completion.
	finalizedHPP FinalizedHPPSource
	// productPolicies (Product Catalog): kebijakan produk per unit_type —
	// kategori (ikut HPP atau tidak) + akun pendapatan. Seam WAJIB di produksi
	// (dipasang di main.go); nil hanya pada unit test in-memory lama, yang
	// memperlakukan setiap unit sebagai properti dengan akun default.
	productPolicies ProductPolicyResolver
	// realization (W-4, opsional): sumber tagihan biaya realisasi untuk blok
	// eksposur pada Customer Statement. Interface, bukan import langsung —
	// `charge` sudah meng-import paket ini, jadi arah sebaliknya akan siklik.
	realization RealizationExposureReader
	// houseAR (W-8): bahan mentah piutang harga rumah. Dipasang di produksi lewat
	// WithHouseARStore; bila nil, pembacanya menolak dengan
	// ErrHouseARNotConfigured — laporan piutang yang diam-diam kosong lebih
	// berbahaya daripada laporan yang menolak tampil.
	houseAR HouseARStore
	// houseARLedger (W-8): sisi buku besar dari tie-out INV-AR-4. Dipasang lewat
	// WithHouseARLedger; hanya dipakai ReconcileHouseAR, sehingga laporan piutang
	// tetap jalan meski rekonsiliasi belum terpasang — tetapi rekonsiliasinya
	// sendiri menolak, bukan melaporkan "cocok" atas nol lawan nol.
	houseARLedger HouseARLedgerReader
	// billingPlan (W-8/P6): jadwal pra-BAST — komplemen house AR. Dipasang lewat
	// WithBillingPlanStore; fail-closed dengan alasan yang sama.
	billingPlan BillingPlanStore
	// landAkad (kelebihan-tanah-booking-integration-2026-08, opsional via
	// WithLandAkadPreparer): resolusi+bangun jurnal komponen Kelebihan Tanah
	// pada Akad. Nil di produksi = misconfiguration — RecordAkad menolak
	// fail-closed HANYA bila kontrak yang sedang diproses memang punya
	// komponen tanah (kontrak tanpa tanah, mayoritas kasus, tidak terpengaruh).
	landAkad LandAkadPreparer
	// landSaleReader (P1 — Kelebihan Tanah outstanding & payment flow,
	// 2026-09-04, opsional via SetLandSaleReader): membaca rincian satu
	// land_sale (quantity_m2, harga/m²) untuk ditampilkan pada baris jadwal
	// tanah di Customer Statement. Nil = baris tanah tetap tampil (jumlah,
	// outstanding, status tetap benar dari payment_schedules) hanya tanpa
	// rincian m²/harga satuan — degradasi anggun, bukan kegagalan.
	landSaleReader LandSaleReader
}

// WithLandAkadPreparer mengaktifkan pengakuan Kelebihan Tanah bundled-Akad
// (kelebihan-tanah-booking-integration-2026-08). Wiring produksi: land.Service.
func WithLandAkadPreparer(p LandAkadPreparer) ServiceOption {
	return func(s *Service) { s.landAkad = p }
}

// LandSaleReader membaca detail satu land_sale — implementasi produksi:
// *land.Service.GetLandSale (interface sempit, pola identik LandAkadPreparer,
// supaya unit test tidak perlu wiring seluruh paket land).
type LandSaleReader interface {
	GetLandSale(ctx context.Context, tenantID, id uint64) (*land.LandSale, error)
}

// SetLandSaleReader memasang pembaca land_sales (wiring produksi: land.Service).
func (s *Service) SetLandSaleReader(r LandSaleReader) { s.landSaleReader = r }

// RealizationRecognizer dijalankan DI DALAM transaksi BAST — diimplementasikan
// package charge via adapter wiring (sale tidak import charge; pola
// FinalizedHPPSource / taxAccruer).
//
// Ini BUKAN gate: ia tidak pernah menolak BAST karena biaya realisasi belum
// lunas. Yang dilakukannya adalah menerbitkan tagihan yang belum terbit sehingga
// sisanya menjadi Piutang Customer pada saat BAST (D-3). Kegagalannya tetap
// membatalkan transaksi — kegagalan teknis di sini berarti piutang hilang tanpa
// jejak.
type RealizationRecognizer interface {
	RecognizeOnBASTInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID uint64, createdBy *uint64) error
}

// ProductPolicyResolver me-resolve kebijakan produk sebuah unit_type
// (diimplementasi project.GORMRepository dari master product_types).
//
// FAIL-CLOSED: implementasi produksi mengembalikan error untuk kode tak
// terdaftar / produk nonaktif / mapping akun kosong / error DB. RecordBAST
// meneruskan error itu apa adanya — tidak ada jurnal yang terbentuk.
type ProductPolicyResolver interface {
	ResolveProductPolicy(ctx context.Context, tenantID uint64, unitType string) (domain.ProductPolicy, error)
}

// SetProductPolicyResolver memasang resolver Product Catalog (wiring produksi).
func (s *Service) SetProductPolicyResolver(r ProductPolicyResolver) { s.productPolicies = r }

// resolveUnitPolicy mengambil kebijakan produk sebuah unit.
//
// Tanpa resolver terpasang (HANYA unit test in-memory lama — produksi selalu
// memasangnya di main.go) perilaku legacy dipertahankan: unit diperlakukan
// sebagai properti dengan akun pendapatan default. Di produksi jalur ini tidak
// pernah dipakai; lihat integration test TestProductCatalog_* yang membuktikan
// unit_type tak terdaftar menolak BAST.
func (s *Service) resolveUnitPolicy(ctx context.Context, tenantID uint64, unitType string) (domain.ProductPolicy, error) {
	if s.productPolicies == nil {
		return domain.ProductPolicy{
			Code:               unitType,
			Category:           domain.ProductCategoryProperty,
			RevenueAccountCode: project.DefaultPropertyRevenueAccount,
		}, nil
	}
	policy, err := s.productPolicies.ResolveProductPolicy(ctx, tenantID, unitType)
	if err != nil {
		return domain.ProductPolicy{}, fmt.Errorf("%w: %v", ErrProductPolicyUnresolved, err)
	}
	if !policy.Category.Valid() || policy.RevenueAccountCode == "" {
		return domain.ProductPolicy{}, fmt.Errorf("%w: kebijakan produk %q tidak lengkap", ErrProductPolicyUnresolved, unitType)
	}
	return policy, nil
}

// FinalizedHPPSource menyediakan HPP finalized (P0-4 D2) untuk unit yang dijual
// SETELAH project completion finalized. Kontrak:
//   - (breakdown, true, nil)  → pakai HPP finalized (metode 'finalized').
//   - (zero, false, nil)      → tidak berlaku (belum finalized) — jalur normal.
//   - error                   → blokir BAST (mis. finalized tapi true-up belum posted).
//
// Diimplementasikan oleh internal/closing (via adapter main.go — sale tidak
// meng-import closing).
type FinalizedHPPSource interface {
	FinalizedUnitHPP(ctx context.Context, tenantID, projectID, unitID uint64) (domain.UnitCostBreakdown, bool, error)
}

// SetFinalizedHPPSource memasang sumber HPP finalized (wiring produksi P0-4).
func (s *Service) SetFinalizedHPPSource(src FinalizedHPPSource) { s.finalizedHPP = src }

// UnitTransitioner adalah seam opsional ke project.Service (Increment 6 D3).
type UnitTransitioner interface {
	Transition(ctx context.Context, tenantID, id uint64, req project.TransitionRequest) (*project.Unit, error)
}

// SetUnitTransitioner memasang seam proyeksi lifecycle unit (wiring produksi).
func (s *Service) SetUnitTransitioner(t UnitTransitioner) { s.unitTransitioner = t }

// SetReceiptGenerator memasang generator kwitansi (dipanggil dari wiring layer).
func (s *Service) SetReceiptGenerator(g ReceiptGenerator) { s.receiptGen = g }

// ShortfallInvoicer (R1): dipasang wiring — billing memutuskan sendiri
// berdasarkan kebijakan tenant (default OFF). Best-effort, tidak pernah
// menggagalkan pembayaran.
type ShortfallInvoicer interface {
	MaybeAutoShortfallInvoice(ctx context.Context, tenantID, contractID, createdBy uint64)
	// SettleShortfallIfPaid menandai invoice KEKURANGAN paid bila outstanding
	// kontrak sudah nol (dipanggil setelah SETIAP pembayaran kontrak) —
	// invoice kekurangan tidak terikat schedule sehingga jalur sinkron
	// MarkPaidByScheduleID tidak menjangkaunya.
	SettleShortfallIfPaid(ctx context.Context, tenantID, contractID uint64)
}

// SetShortfallInvoicer memasang pembuat invoice kekurangan otomatis.
func (s *Service) SetShortfallInvoicer(g ShortfallInvoicer) { s.shortfallInv = g }

// SetInvoiceUpdater memasang updater status invoice.
func (s *Service) SetInvoiceUpdater(u InvoiceStatusUpdater) { s.invoiceUpdater = u }

// RealizationExposureReader (W-4) menyediakan tagihan biaya realisasi satu
// kontrak. Diimplementasi charge.Service; dipasang di main.go.
type RealizationExposureReader interface {
	ReceivableRowsByContract(ctx context.Context, tenantID, contractID uint64) ([]receivable.Row, error)
}

// SetRealizationExposure memasang sumber tagihan biaya realisasi sehingga
// statement menampilkan SATU eksposur. Opsional: tanpa ini statement tetap
// benar, hanya menandai realization_available=false — bukan Rp0 palsu.
func (s *Service) SetRealizationExposure(r RealizationExposureReader) { s.realization = r }

// ServiceOption adalah functional option untuk konfigurasi opsional Service.
type ServiceOption func(*Service)

// WithContractStore mengaktifkan fitur PaymentSchedule pada Service.
func WithContractStore(cs ContractStore) ServiceOption {
	return func(s *Service) { s.contracts = cs }
}

// WithPaymentCommitter memasang committer atomik (produksi:
// GORMRepository.CommitPayment). Bila tidak dipasang, Service memakai
// defaultCommitter berbasis interface (non-atomik) — untuk unit test.
func WithPaymentCommitter(c PaymentCommitter) ServiceOption {
	return func(s *Service) { s.committer = c }
}

// WithHPPResolver mengaktifkan metode Budgeted Cost Allocation di RecordAkad.
// Tanpa opsi ini, RecordAkad memakai UnitCostProvider (metode actual legacy).
func WithHPPResolver(r HPPResolver) ServiceOption {
	return func(s *Service) { s.hppResolver = r }
}

// WithHandoverWriter memasang eksekutor serah terima fisik (Temuan #7,
// produksi: GORMRepository.MarkPhysicalHandover). Tanpa ini,
// RecordPhysicalHandover menolak fail-closed.
func WithHandoverWriter(w HandoverWriter) ServiceOption {
	return func(s *Service) { s.handoverWriter = w }
}

func NewService(
	accounts AccountFinder,
	journals JournalWriter,
	units UnitReader,
	store TerminStore,
	costProvider UnitCostProvider,
	bastWriter BASTAtomicWriter,
	opts ...ServiceOption,
) *Service {
	svc := &Service{
		accounts:     accounts,
		journals:     journals,
		units:        units,
		store:        store,
		costProvider: costProvider,
		bastWriter:   bastWriter,
	}
	for _, o := range opts {
		o(svc)
	}
	// Fallback: tanpa committer atomik, pakai defaultCommitter (non-atomik).
	if svc.committer == nil {
		svc.committer = &defaultCommitter{svc: svc}
	}
	return svc
}

// requireContractStore mengembalikan error jika ContractStore belum dikonfigurasi.
func (s *Service) requireContractStore() error {
	if s.contracts == nil {
		return ErrContractStoreNotConfigured
	}
	return nil
}

// ── RecordTermin — Event 2 ────────────────────────────────────────────────────

// preparedPayment adalah hasil validasi + routing SEBELUM commit (tanpa tulis DB).
type preparedPayment struct {
	unitID     uint64
	projectID  uint64
	phaseID    *uint64
	creditCode string             // 2-2000 (pra-BAST) | 1-2000 (pasca-BAST)
	lines      []JournalLineInput // jurnal balanced (Dr Bank / Cr kredit)
}

// preparePayment memvalidasi penerimaan, menentukan akun kredit (routing GAP-1),
// dan MEMBANGUN baris jurnal balanced — TANPA menulis apa pun ke DB. Persistensi
// dilakukan committer (satu transaksi). Menggantikan primitif lama
// recordTerminEntry; satu-satunya pemanggil sah adalah ReceivePayment.
//
// Akun kredit ditentukan oleh status BAST unit (business rule GAP-1):
//   - SEBELUM BAST  → Dr Bank / Cr Uang Muka Penjualan (2-2000) — kewajiban (Invariant #7).
//   - SETELAH BAST  → Dr Bank / Cr akun piutang KONTRAK — default Piutang Usaha
//     (1-2000); kontrak ber-scheme me-resolve via policy (KPR pasca-akad →
//     piutang bank, konsisten dengan sisi debit BAST/reklas — Increment 3).
//
// contract boleh nil (advance tanpa kontrak / pemanggil legacy).
func (s *Service) preparePayment(ctx context.Context, tenantID uint64, req RecordTerminRequest, contract *SaleContract) (*preparedPayment, error) {
	return s.preparePaymentFunded(ctx, tenantID, req, contract, true)
}

// preparePaymentFunded adalah inti preparePayment dengan sumber dana eksplisit.
// requireCashBank=true (jalur normal): debit wajib akun kategori cash/bank.
// requireCashBank=false (Billing Batch 2, K-4 transfer titipan → harga): debit
// adalah akun liability titipan (mis. 2-2400) — routing kredit & guard
// overpayment TETAP SAMA (satu jalur, no duplicate engine).
func (s *Service) preparePaymentFunded(ctx context.Context, tenantID uint64, req RecordTerminRequest, contract *SaleContract, requireCashBank bool) (*preparedPayment, error) {
	if req.UnitID == 0 {
		return nil, ErrUnitRequired
	}
	if !req.Amount.IsWholeRupiah() {
		return nil, ErrTerminAmountFractional
	}
	if req.Amount.IsZero() || req.Amount.IsNeg() {
		return nil, ErrTerminAmountZeroOrNeg
	}
	// BankFee=0 (default, semua caller lama) selalu lolos tanpa perubahan
	// perilaku. BankFee>0 harus rupiah bulat, tidak negatif, dan < Amount
	// (bukan <=): pencairan harus selalu menyisakan kas bersih > 0, jika tidak
	// baris debit bank menjadi nol dan jurnal ditolak posting service
	// (ErrLineInvalid) — dipotong 100% bukan kasus bisnis nyata.
	if !req.BankFee.IsZero() {
		if !req.BankFee.IsWholeRupiah() || req.BankFee.IsNeg() || !req.BankFee.LessThan(req.Amount) {
			return nil, ErrBankFeeInvalid
		}
	}
	// Validasi rekening tujuan COA-driven (bukan hardcode): ada, milik tenant,
	// aktif, kategori cash/bank.
	if requireCashBank {
		if err := s.accounts.ValidateCashBankAccount(ctx, tenantID, req.BankAccountCode); err != nil {
			return nil, err
		}
	}

	unit, err := s.units.FindUnitSaleInfo(ctx, tenantID, req.UnitID)
	if err != nil {
		return nil, err
	}

	// Status BAST: SaleRecord ada ⟺ unit sudah BAST (tenant-scoped).
	saleRec, srErr := s.store.FindSaleRecord(ctx, tenantID, req.UnitID)
	if srErr != nil && !errors.Is(srErr, ErrSaleRecordNotFound) {
		return nil, fmt.Errorf("cek status BAST unit: %w", srErr)
	}
	isBAST := saleRec != nil

	// Sebelum BAST: unit "sold" tanpa SaleRecord = inkonsisten → tolak (perilaku lama).
	if !isBAST && unit.Status == "sold" {
		return nil, ErrUnitAlreadySold
	}

	// Pilih akun kredit sesuai status BAST.
	creditCode := accountCodeUangMuka
	if isBAST {
		creditCode = accountCodePiutang
		// Kontrak ber-scheme: akun piutang dari policy (bukan hardcode) —
		// KPR antara akad dan pencairan mengkredit piutang bank (1-2200);
		// setelah pencairan sisa tagihan sudah direklas ke piutang customer
		// (T-3), sehingga pelunasan kekurangan mengkredit 1-2000.
		if contract != nil {
			if code, ok := s.schemeCreditAccountForPayment(ctx, contract, req.Source); ok {
				creditCode = code
			}
		}
		// 7C guard (UAT 2026-09-07): pencairan KPR dibatasi ke SISA Dana
		// Jaminan Bank saja (bukan sisa piutang unit gabungan) — supaya
		// pencairan tak pernah melebihi apa yang benar-benar masih ada di
		// akun itu, dan supaya kelebihan TIDAK diam-diam "melimpah" ke
		// Piutang Usaha (tidak ada aturan bisnis untuk itu). Dana Jaminan
		// Bank sudah lunas → tolak eksplisit, jangan redirect diam-diam.
		if req.Source == PaymentSourceKPRDisbursement && creditCode != accountCodePiutang && isBAST {
			finRemaining, ferr := s.financingOutstanding(ctx, tenantID, contract, creditCode)
			if ferr != nil {
				return nil, ferr
			}
			if req.Amount.GreaterThan(finRemaining) {
				return nil, ErrDisbursementExceedsFinancing
			}
		}
		// Guard overpayment: penerimaan tidak boleh melebihi sisa piutang
		// (gross − total diterima sebelum pembayaran ini). Mencegah saldo
		// Piutang menjadi negatif (kelebihan bayar ditangani terpisah, GAP-7).
		//
		// Bug spillover (2026-09-04): unitOutstanding (gross rumah − ΣSemua
		// termin counts_toward_price) TIDAK BOLEH dipakai lagi begitu ada
		// pembayaran yang melimpah dari rumah ke tanah dalam SATU termin —
		// termin campuran itu tetap CountsTowardPrice=true untuk NILAI PENUH
		// (lihat planAllocationsLocked), jadi bagian yang sebetulnya masuk
		// tanah ikut mengurangi "collected" rumah, membuat unitOutstanding
		// negatif sebesar limpahan itu. Menjumlahkannya dengan
		// landOutstandingForContract (yang sudah benar, berbasis paid_amount
		// jadwal) lalu MENGURANGI limpahan itu DUA KALI — total tergerus, dan
		// pelunasan sisa tanah yang sah pun ditolak (ErrPaymentExceedsReceivable)
		// padahal previewnya valid. outstandingForContract (dipakai juga oleh
		// PreviewCollectionPayment, SoT yang sama) menghitung house+land
		// langsung dari payment_schedules.PaidAmount sehingga limpahan sudah
		// tercermin sekali saja di kedua sisi — aman dipakai di sini.
		var outstanding domain.Money
		if contract != nil {
			var oerr error
			outstanding, oerr = s.outstandingForContract(ctx, tenantID, contract)
			if oerr != nil {
				return nil, oerr
			}
		} else {
			var cerr error
			outstanding, cerr = s.unitOutstanding(ctx, tenantID, saleRec)
			if cerr != nil {
				return nil, cerr
			}
		}
		if req.Amount.GreaterThan(outstanding) {
			return nil, ErrPaymentExceedsReceivable
		}
	}

	bankAccID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, req.BankAccountCode)
	if err != nil {
		return nil, fmt.Errorf("cari akun bank %s: %w", req.BankAccountCode, err)
	}
	creditAccID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, creditCode)
	if err != nil {
		return nil, fmt.Errorf("cari akun kredit %s: %w", creditCode, err)
	}

	pid := unit.ProjectID
	uid := req.UnitID
	netCash := req.Amount.Sub(req.BankFee)
	lines := []JournalLineInput{
		{
			AccountID: bankAccID, Debit: netCash,
			ProjectID: &pid, PhaseID: unit.PhaseID, UnitID: &uid,
			Description: req.Description,
		},
	}
	// BankFee (UAT 2026-09-03, Rule #5): provisi/administrasi bank yang
	// dipotong SEBELUM kas diterima developer — dibebankan langsung ke P&L
	// (5-3200), bukan mengurangi piutang yang diselesaikan (creditAccID tetap
	// menerima Amount penuh — nilai piutang/DP yang lunas tidak berubah).
	if !req.BankFee.IsZero() {
		feeAccID, ferr := s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodeBankFeeExpense)
		if ferr != nil {
			return nil, fmt.Errorf("cari akun beban provisi bank %s: %w", accountCodeBankFeeExpense, ferr)
		}
		lines = append(lines, JournalLineInput{
			AccountID: feeAccID, Debit: req.BankFee,
			ProjectID: &pid, PhaseID: unit.PhaseID, UnitID: &uid,
			Description: "provisi/administrasi bank — " + req.Description,
		})
	}
	lines = append(lines, JournalLineInput{
		AccountID: creditAccID, Credit: req.Amount,
		ProjectID: &pid, PhaseID: unit.PhaseID, UnitID: &uid,
		Description: req.Description,
	})

	return &preparedPayment{
		unitID:     req.UnitID,
		projectID:  unit.ProjectID,
		phaseID:    unit.PhaseID,
		creditCode: creditCode,
		lines:      lines,
	}, nil
}

// saleRecordGross menghitung nilai bruto yang ditagih ke pembeli dari SaleRecord:
// DPP + PPN (jika PKP). Konsisten dengan tagihan buyer dan dasar sisa Piutang.
func saleRecordGross(sr *SaleRecord) domain.Money {
	if sr == nil {
		return domain.Zero
	}
	gross := sr.SalePrice
	if sr.IsVAT {
		gross = gross.Add(vatAmountOf(sr.SalePrice, sr.VATRate)) // rumus PPN tunggal (S9)
	}
	return gross
}

// unitOutstanding adalah SISA PIUTANG HARGA RUMAH sebuah unit yang sudah BAST:
// nilai bruto dikurangi seluruh penerimaan yang mengurangi harga.
//
// W-8 memberi nama pada rumus yang sudah dipakai dua tempat (guard overpayment
// di ReceivePayment dan nilai reklas T-3 di reclassReceivableIfBAST) supaya
// pembaca piutang menjadi pemakai KETIGA, bukan definisi kedua. Angka inilah
// yang wajib sama dengan saldo akun kontrol piutang unit di buku besar
// (INV-AR-4) — dan ia sama justru karena `termin_payments` ditulis dalam
// transaksi yang sama dengan jurnal penerimaannya.
func (s *Service) unitOutstanding(ctx context.Context, tenantID uint64, sr *SaleRecord) (domain.Money, error) {
	collected, err := s.store.SumTerminsByUnit(ctx, tenantID, sr.UnitID)
	if err != nil {
		return domain.Zero, fmt.Errorf("hitung termin terkumpul: %w", err)
	}
	return saleRecordGross(sr).Sub(collected), nil
}

// financingOutstanding (Item 7C, UAT 2026-09-07) menghitung sisa Dana Jaminan
// Bank kontrak: Nilai Persetujuan KPR Bank (c.LoanAmount, diisi saat Akad —
// lihat Item 7A) dikurangi Σ pencairan KPR yang SUDAH dikreditkan ke
// financingCode untuk unit ini. Dibaca langsung dari termin_payments
// (append-only, SoT), bukan estimasi ulang — sehingga pencairan ke-2/ke-3
// selalu dibatasi ke sisa yang benar, tidak pernah melimpah diam-diam ke
// Piutang Usaha.
func (s *Service) financingOutstanding(ctx context.Context, tenantID uint64, c *SaleContract, financingCode string) (domain.Money, error) {
	if c == nil || c.LoanAmount == nil {
		return domain.Zero, nil
	}
	disbursed, err := s.store.SumTerminsByUnitAndCreditAccount(ctx, tenantID, c.UnitID, financingCode)
	if err != nil {
		return domain.Zero, fmt.Errorf("hitung pencairan KPR terkumpul: %w", err)
	}
	remaining := c.LoanAmount.Sub(disbursed)
	if remaining.IsNeg() {
		remaining = domain.Zero
	}
	return remaining, nil
}

// ── RecordAkad — Event 3+4 atomik ────────────────────────────────────────────

// RecordAkad mengakui pendapatan + HPP saat Akad (Event 3 dan Event 4) — Temuan
// #7: dulu bernama RecordBAST/dipicu BAST; titik pengakuan sekarang Akad (legal),
// bukan serah terima fisik. Kedua jurnal dieksekusi atomik: jika salah satu
// gagal, keduanya dibatalkan. HPP = biaya akumulasi unit (Direct + Allocated)
// dari engine Phase 5 (Invariant #4). Serah terima fisik adalah aksi TERPISAH,
// belakangan, tanpa jurnal — lihat RecordPhysicalHandover.
func (s *Service) RecordAkad(ctx context.Context, tenantID uint64, req RecordBASTRequest) (*SaleRecord, error) {
	// ── Validasi dasar ────────────────────────────────────────────────────────
	if req.UnitID == 0 {
		return nil, ErrUnitRequired
	}
	if !req.SalePrice.IsWholeRupiah() {
		return nil, ErrSalePriceFractional
	}
	if req.SalePrice.IsZero() || req.SalePrice.IsNeg() {
		return nil, ErrSalePriceZeroOrNeg
	}
	if req.IsVAT && req.VATRate.LessThanOrEqual(decimal.Zero) {
		return nil, ErrVATRateRequired
	}
	if req.BASTDate.IsZero() {
		return nil, ErrBASTDateRequired
	}

	// ── Unit check ────────────────────────────────────────────────────────────
	unit, err := s.units.FindUnitSaleInfo(ctx, tenantID, req.UnitID)
	if err != nil {
		return nil, err
	}
	if unit.Status == "sold" {
		return nil, ErrUnitAlreadySold
	}

	// ── Kebijakan produk (Product Catalog) — KANONIK, fail-closed ─────────────
	// Menentukan dua hal sekaligus: akun pendapatan yang dipakai, dan apakah
	// produk ini ikut HPP. Gagal resolve = BAST dibatalkan sebelum jurnal apa
	// pun terbentuk (unit_type tak terdaftar, produk nonaktif, mapping akun
	// kosong, atau error DB).
	policy, err := s.resolveUnitPolicy(ctx, tenantID, unit.UnitType)
	if err != nil {
		return nil, err
	}

	// ── HPP: finalized (P0-4 D2) → budgeted (resolver) → actual legacy ────────
	// D2: proyek yang completion-nya sudah FINALIZED memakai act_HPP finalized
	// dari true-up (tanpa alokasi live baru, tanpa snapshot). Bila belum
	// finalized → jalur normal: budgeted (RAB aktif) atau actual (legacy).
	//
	// H-1: produk NON-PROPERTI melewati seluruh blok ini. Ia tidak pernah
	// menerima alokasi biaya proyek, jadi tidak ada HPP untuk diakui dan tidak
	// ada snapshot RAB yang dibuat — pendapatannya diakui penuh sesuai mapping
	// akun produk.
	var hpp domain.UnitCostBreakdown
	hppMethod := HPPMethodActual
	var snapshot *SnapshotDraft
	hppResolved := false
	if !policy.ParticipatesInHPP() {
		hppMethod, hppResolved = HPPMethodNone, true // hpp tetap nol
	}
	if !hppResolved && s.finalizedHPP != nil {
		fb, ok, ferr := s.finalizedHPP.FinalizedUnitHPP(ctx, tenantID, unit.ProjectID, req.UnitID)
		if ferr != nil {
			return nil, ferr
		}
		if ok {
			hpp, hppMethod, hppResolved = fb, HPPMethodFinalized, true
		}
	}
	if !hppResolved {
		if s.hppResolver != nil {
			res, rerr := s.hppResolver.ResolveHPP(ctx, tenantID, unit.ProjectID, unit.PhaseID, req.UnitID)
			if rerr != nil {
				return nil, rerr
			}
			hpp, hppMethod, snapshot = res.Breakdown, res.Method, res.Snapshot
		} else {
			hpp, err = s.costProvider.GetUnitCost(ctx, tenantID, unit.ProjectID, req.UnitID)
			if err != nil {
				return nil, fmt.Errorf("hitung HPP unit: %w", err)
			}
		}
	}

	// ── Σ termin yang sudah diterima ─────────────────────────────────────────
	totalAdvance, err := s.store.SumTerminsByUnit(ctx, tenantID, req.UnitID)
	if err != nil {
		return nil, fmt.Errorf("hitung total termin: %w", err)
	}

	// ── Gross harga rumah ──────────────────────────────────────────────────────
	var gross domain.Money
	var ppn domain.Money
	if req.IsVAT {
		ppn = vatAmountOf(req.SalePrice, req.VATRate) // rumus PPN tunggal (S9)
		gross = req.SalePrice.Add(ppn)
	} else {
		gross = req.SalePrice
	}
	pid := unit.ProjectID
	uid := req.UnitID

	// ── Produk Tambahan: Kelebihan Tanah (kelebihan-tanah-booking-integration-2026-08) ──
	// Disiapkan LEBIH AWAL (bug 2026-09-04) — bukan lagi setelah Event3/4
	// dibangun — karena validasi advance-vs-harga di bawah butuh gross tanah
	// (landParams.GrossAmount) untuk kontrak yang membawa komponen tanah.
	// Kontrak unit ini mungkin punya komponen tanah (dibawa dari Booking atau
	// diisi langsung saat CreateContract). Disiapkan (BUKAN dieksekusi) di sini
	// via PrepareBundledAkad — hasilnya diteruskan ke Execute (repository.go)
	// untuk diposting ATOMIK bersama Event3/4 unit di transaksi yang sama
	// (§D3: revenue+HPP tanah = jurnal sendiri, timing identik dengan Akad
	// rumah).
	//
	// KOREKSI KLIEN (2026-08-31): akun debit piutang tanah TIDAK BOLEH ikut
	// accIDs.piutang/receivableCode di bawah — itu hasil ResolveReceivableAccount
	// milik SKEMA PEMBIAYAAN RUMAH, yang untuk KPR pada state Akad resolve ke
	// Piutang Bank (1-2200, T-3). Kelebihan Tanah bukan bagian dari KPR — bank
	// tidak pernah membiayainya — jadi piutangnya SELALU ke Piutang Customer
	// (accountCodePiutang, 1-2000), independen dari skema pembiayaan rumah.
	// ── Buyer Credit — resolusi kontrak, dipakai konsumsi otomatis Akad ────────
	// SaleContractID kini WAJIB diresolusi untuk SEMUA Akad (bukan hanya yang
	// membawa komponen tanah): BASTAtomicWriter.Execute memakainya utk menutup
	// SISA saldo kredit buyer yang dikonsumsi oleh netting Uang Muka Penjualan
	// di Event 3 (lihat consumeRemainingCreditInTx). Kontrak tak ditemukan
	// (mis. data legacy tanpa SaleContract formal) dibiarkan 0 — Execute hanya
	// menuntut nilai ini bila memang ada sisa saldo kredit untuk ditutup.
	var landParams *land.RecordAkadParams
	var landReceivableID uint64
	var saleContractID uint64
	// contract dihoist ke scope RecordAkad (bukan lagi lokal ke blok Kelebihan
	// Tanah) — Item 7A butuh akses SaleContract penuh untuk resolusi split
	// Dana Jaminan Bank/Piutang Usaha di bawah.
	var contract *SaleContract
	if s.contracts != nil {
		var cerr error
		contract, cerr = s.contracts.FindContractByUnitID(ctx, tenantID, req.UnitID)
		if cerr != nil && !errors.Is(cerr, ErrContractNotFound) {
			return nil, fmt.Errorf("resolve kontrak unit %d: %w", req.UnitID, cerr)
		}
		if cerr == nil {
			saleContractID = contract.ID
		}
		if cerr == nil && contract.LandStockID != nil {
			if contract.LandReservationID == nil || contract.LandQuantityM2 == nil {
				return nil, ErrLandComponentRequiresLandStock
			}
			if s.landAkad == nil {
				return nil, ErrLandAkadPreparerNotConfigured
			}
			var aerr error
			landReceivableID, aerr = s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodePiutang)
			if aerr != nil {
				return nil, fmt.Errorf("resolve akun piutang Kelebihan Tanah: %w", aerr)
			}
			var priceSnapshot domain.Money
			if contract.LandUnitPriceSnapshot != nil {
				priceSnapshot = *contract.LandUnitPriceSnapshot
			}
			var custID uint64
			if contract.CustomerID != nil {
				custID = *contract.CustomerID
			}
			prepared, lerr := s.landAkad.PrepareBundledAkad(ctx, tenantID, land.PrepareBundledAkadRequest{
				ProjectID:             unit.ProjectID,
				ReservationID:         contract.LandReservationID,
				CustomerID:            custID,
				SalesPersonID:         contract.SalesPersonID,
				QuantityM2:            *contract.LandQuantityM2,
				UnitPriceSnapshot:     priceSnapshot,
				IsPKP:                 req.IsVAT,
				VATRateSnapshot:       req.VATRate,
				ReceivableAccountID:   landReceivableID,
				ReceivableAccountCode: accountCodePiutang,
				RecognitionDate:       req.BASTDate,
				CreatedBy:             req.CreatedBy,
				UnitCode:              unit.Code,
				ProjectName:           unit.ProjectName,
			})
			if lerr != nil {
				return nil, fmt.Errorf("siapkan Akad Kelebihan Tanah: %w", lerr)
			}
			landParams = &prepared
		}
	}

	// ── Pemisahan advance rumah vs Kelebihan Tanah + validasi vs harga (bug 2026-09-04) ──
	// totalAdvance adalah SATU angka gabungan: kontrak Tunai lump-sum tanpa
	// baris payment_schedules pra-Akad menaruh SELURUH termin (rumah+tanah)
	// sebagai satu Uang Muka Penjualan. Bagian rumah untuk Event 3 di-cap ke
	// gross rumah — tanpa cap ini "piutang := gross.Sub(advance)" di
	// buildEvent3Lines bisa jadi NEGATIF dan ditolak posting service
	// (ErrLineNegative). Sisa (landAdvance) secara ekonomi milik Kelebihan
	// Tanah, dinetkan terpisah ke piutang tanah (landAdvanceLines di bawah) —
	// bukan hilang, bukan double count.
	houseAdvance := totalAdvance
	if houseAdvance.GreaterThan(gross) {
		houseAdvance = gross
	}
	landAdvance := totalAdvance.Sub(houseAdvance)
	landGross := domain.Zero
	if landParams != nil {
		landGross = landParams.GrossAmount
	}
	if landAdvance.GreaterThan(landGross) {
		return nil, ErrAdvanceExceedsSalePrice
	}

	// ── Gate Akad scheme (Increment 3/7) + resolusi akun piutang via policy ───
	// Kontrak legacy / tanpa scheme flow → gate lewat & akun default 1-2000
	// (perilaku lama). KPR pasca-akad → sisa tagihan didebit ke piutang bank
	// (ResolveReceivableAccount — approval note #1: dari konfigurasi, bukan hardcode).
	receivableCode, err := s.schemeAkadGuard(ctx, tenantID, req.UnitID, totalAdvance, gross)
	if err != nil {
		return nil, err
	}

	// ── Item 7A (UAT 2026-09-07): split Dana Jaminan Bank vs Piutang Usaha ────
	// Kontrak KPR-financed (FinancingReceivableAccount terkonfigurasi di
	// scheme params) memecah sisa tagihan (piutangTotal = gross − houseAdvance)
	// menjadi DUA baris SEKALIGUS di Akad: Dana Jaminan Bank = min(Nilai
	// Persetujuan KPR Bank, piutangTotal); sisanya (jika ada, mis. selisih DP
	// bank vs harga unit) = Piutang Usaha — BUKAN direklas belakangan saat
	// pencairan (itu bug lama, sudah dicabut di reclassOnStateChange/7C).
	// Kontrak non-KPR/legacy: perilaku lama persis (financingCode="" →
	// financingAmount selalu nol, receivableCode dari schemeAkadGuard dipakai
	// apa adanya).
	piutangTotal := gross.Sub(houseAdvance)
	if piutangTotal.IsNeg() {
		piutangTotal = domain.Zero
	}
	financingCode := ""
	financingAmount := domain.Zero
	receivableAmount := piutangTotal
	if contract != nil {
		candidateFinancingCode, defaultReceivableCode := s.schemeAkadSplitAccounts(ctx, contract)
		// Split hanya aktif bila state SAAT INI sudah `akad` — dibuktikan
		// dengan receivableCode (hasil schemeAkadGuard, berbasis state) SUDAH
		// SAMA dengan kode financing. BAST yang terjadi SEBELUM Akad (mis.
		// gate dp_paid) tetap 100% ke Piutang Usaha di sini; pemecahan baru
		// terjadi nanti saat event Akad benar-benar dipicu pasca-BAST (lihat
		// reclassOnStateChange/reclassToFinancingIfBAST) — bukan diantisipasi
		// lebih awal, karena bank belum tentu approve di titik BAST ini.
		if candidateFinancingCode != "" && receivableCode == candidateFinancingCode {
			financingCode = candidateFinancingCode
			approved, aerr := s.resolveBankApprovedAmount(ctx, tenantID, contract, req.BankApprovedAmount)
			if aerr != nil {
				return nil, aerr
			}
			financingAmount = approved
			if financingAmount.GreaterThan(piutangTotal) {
				financingAmount = piutangTotal
			}
			receivableAmount = piutangTotal.Sub(financingAmount)
			if defaultReceivableCode != "" {
				receivableCode = defaultReceivableCode
			}
		}
	}

	// Gate Biaya Realisasi (K-2) DICABUT di W-5 — keputusan klien D-3: BAST tidak
	// boleh mensyaratkan biaya realisasi lunas. Sisanya diakui sebagai Piutang
	// Customer di dalam transaksi BAST (RealizationRecognizer, dipanggil dari
	// repository bersama jurnalnya).

	// ── Resolve account IDs ───────────────────────────────────────────────────
	// UAT Batch 2 §2: akun pendapatan dari mapping Product Catalog unit
	// (rumah/ruko/kavling/… masing-masing bisa punya akun sendiri, COA-driven).
	// M-1: kode sudah dipastikan sah oleh resolveUnitPolicy di atas — tidak ada
	// lagi "kalau resolver error, pakai 4-1000".
	accIDs, err := s.resolveAccountsWithFinancing(ctx, tenantID, req.IsVAT, receivableCode, financingCode, policy.RevenueAccountCode)
	if err != nil {
		return nil, err
	}

	// ── Build Event 3 lines — advance di-cap ke gross rumah (houseAdvance) ────
	revenueLines := s.buildEvent3Lines(accIDs, pid, uid, unit.PhaseID, req.SalePrice, ppn, houseAdvance, financingAmount, receivableAmount)

	// ── Build Event 4 lines ───────────────────────────────────────────────────
	var cogsLines []JournalLineInput
	if !hpp.Total().IsZero() {
		cogsLines, err = s.buildEvent4Lines(ctx, tenantID, accIDs, pid, uid, unit.PhaseID, hpp)
		if err != nil {
			return nil, err
		}
	}

	// ── Netting advance Kelebihan Tanah (bug 2026-09-04) ──────────────────────
	// Bagian uang muka gabungan yang melebihi harga rumah (landAdvance) sudah
	// dikapitalisasi sebagai kas diterima, tapi TIDAK dinolkan oleh Event 3
	// (yang cuma menyentuh houseAdvance) — tanpa jurnal ini saldo Uang Muka
	// Penjualan menyisakan landAdvance selamanya (yatim, tak pernah dinolkan)
	// SEKALIGUS piutang Kelebihan Tanah yang baru dibuat Execute tampak utuh
	// padahal sebagian/seluruhnya sudah lunas pra-Akad. Konsumsi sub-ledgernya
	// (credit_applications + payment_allocations + cache paid_amount jadwal
	// tanah) dilakukan di Execute (repository.go), atomik bersama jurnal ini.
	var landAdvanceLines []JournalLineInput
	if !landAdvance.IsZero() && !landAdvance.IsNeg() {
		landAdvanceLines = s.buildLandAdvanceNettingLines(accIDs.ump, landReceivableID, pid, uid, unit.PhaseID, landAdvance)
	}

	// ── Eksekusi atomik (Event 3+4 + update unit status) ─────────────────────
	params := BASTAtomicParams{
		TenantID:         tenantID,
		UnitID:           req.UnitID,
		ProjectID:        unit.ProjectID,
		PhaseID:          unit.PhaseID,
		BASTDate:         req.BASTDate,
		BuyerRef:         req.BuyerRef,
		CreatedBy:        req.CreatedBy,
		SalePrice:        req.SalePrice,
		IsVAT:            req.IsVAT,
		VATRate:          req.VATRate,
		TotalAdvance:     totalAdvance,
		HPPLand:          hpp.Land,
		HPPHard:          hpp.Hard,
		HPPSoft:          hpp.Soft,
		HPPFinancing:     hpp.Financing,
		HPPMethod:        hppMethod,
		Snapshot:         snapshot,
		RevenueLines:     revenueLines,
		COGSLines:        cogsLines,
		LandAdvance:      landAdvance,
		LandAdvanceLines: landAdvanceLines,
		Land:             landParams,
		SaleContractID:   saleContractID,
	}
	rec, err := s.bastWriter.Execute(ctx, params)
	if err != nil {
		return nil, err
	}
	// Proyeksi lifecycle scheme (best-effort — SoT tetap sale_records + jurnal).
	s.applyAkadMilestone(ctx, tenantID, req.UnitID, req.BASTDate)
	return rec, nil
}

// ── RecordPhysicalHandover — serah terima fisik (Temuan #7) ─────────────────

// RecordPhysicalHandoverRequest adalah input untuk pencatatan serah terima
// fisik unit — TANPA jurnal, TANPA resolusi HPP. Pendapatan+HPP sudah diakui
// sebelumnya di RecordAkad; ini murni mencatat kapan kunci diserahkan.
type RecordPhysicalHandoverRequest struct {
	UnitID       uint64
	CreatedBy    *uint64
	HandoverDate time.Time
}

// RecordPhysicalHandover mencatat serah terima fisik unit (Temuan #7): unit
// sudah `sold` (Akad sudah tercatat) → `occupied`, `sale_records.handed_over_at`
// diisi, transisi dilog dengan event `EventPhysicallyOccupied`. TIDAK ADA
// jurnal, TIDAK ADA resolusi HPP — murni pencatatan fisik, terpisah dari
// pengakuan finansial yang sudah selesai di Akad.
func (s *Service) RecordPhysicalHandover(ctx context.Context, tenantID uint64, req RecordPhysicalHandoverRequest) (*SaleRecord, error) {
	if req.UnitID == 0 {
		return nil, ErrUnitRequired
	}
	if req.HandoverDate.IsZero() {
		return nil, ErrBASTDateRequired
	}
	if s.handoverWriter == nil {
		return nil, ErrHandoverWriterNotConfigured
	}
	rec, err := s.handoverWriter.MarkPhysicalHandover(ctx, PhysicalHandoverParams{
		TenantID:     tenantID,
		UnitID:       req.UnitID,
		HandoverDate: req.HandoverDate,
	})
	if err != nil {
		return nil, err
	}
	// Proyeksi lifecycle scheme (best-effort — SoT tetap sale_records).
	s.applyHandedOverMilestone(ctx, tenantID, req.UnitID, req.HandoverDate)
	return rec, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type resolvedAccounts struct {
	ump        uint64
	piutang    uint64
	pendapatan uint64
	ppnKeluar  uint64
	hpp        uint64
	land       uint64
	hard       uint64
	soft       uint64
	financing  uint64
	// financingReceivable (Item 7A, UAT 2026-09-07): akun Dana Jaminan Bank
	// (mis. 1-2200) — TERPISAH dari `financing` (1-3300, biaya HPP financing).
	// Hanya di-resolve saat kontrak KPR-financed (schemeAkadSplitAccounts).
	financingReceivable uint64
}

// resolveAccounts me-resolve seluruh akun jurnal BAST. receivableCode berasal
// dari policy scheme (ResolveReceivableAccount) atau default 1-2000 (legacy).
// revenueCode: akun pendapatan PRODUK unit dari Product Catalog.
//
// M-1 (fail-closed): revenueCode kosong TIDAK lagi jatuh ke 4-1000 diam-diam.
// Kode pendapatan selalu berasal dari domain.ProductPolicy yang sudah divalidasi
// (resolveUnitPolicy); kosong berarti seam salah pasang → tolak sebelum jurnal.
func (s *Service) resolveAccounts(ctx context.Context, tenantID uint64, needPPN bool, receivableCode, revenueCode string) (resolvedAccounts, error) {
	return s.resolveAccountsWithFinancing(ctx, tenantID, needPPN, receivableCode, "", revenueCode)
}

// resolveAccountsWithFinancing (Item 7A, UAT 2026-09-07) tambahan dari
// resolveAccounts: financingReceivableCode kosong = perilaku lama persis
// (ra.financingReceivable tetap 0). Non-kosong = kontrak KPR-financed di
// Akad ini — resolve akun Dana Jaminan Bank juga.
func (s *Service) resolveAccountsWithFinancing(ctx context.Context, tenantID uint64, needPPN bool, receivableCode, financingReceivableCode, revenueCode string) (resolvedAccounts, error) {
	var ra resolvedAccounts
	var err error

	lookup := func(code string, dest *uint64) {
		if err != nil || code == "" {
			return
		}
		*dest, err = s.accounts.FindAccountIDByCode(ctx, tenantID, code)
	}

	if revenueCode == "" {
		return ra, fmt.Errorf("%w: akun pendapatan produk kosong", ErrProductPolicyUnresolved)
	}
	lookup("2-2000", &ra.ump)
	lookup(receivableCode, &ra.piutang)
	lookup(financingReceivableCode, &ra.financingReceivable)
	lookup(revenueCode, &ra.pendapatan)
	if needPPN {
		lookup("2-3000", &ra.ppnKeluar)
	}
	lookup("5-1000", &ra.hpp)
	lookup("1-3000", &ra.land)
	lookup("1-3100", &ra.hard)
	lookup("1-3200", &ra.soft)
	lookup("1-3300", &ra.financing)

	return ra, err
}

// buildEvent3Lines membangun baris jurnal Event 3 (pengakuan pendapatan).
// Hanya baris dengan nominal > 0 yang disertakan (karena PostingService menolak baris nol).
//
// financingAmount+receivableAmount HARUS persis sama dengan (gross − advance)
// — caller (RecordAkad) yang menjamin ini (Item 7A, UAT 2026-09-07): untuk
// kontrak non-KPR/legacy, financingAmount selalu nol dan receivableAmount =
// gross−advance persis seperti perilaku lama (satu baris piutang). Untuk
// kontrak KPR-financed, financingAmount = min(Nilai Persetujuan KPR Bank,
// gross−advance) ke Dana Jaminan Bank, sisanya ke Piutang Usaha — dua baris
// terpisah, diposting BERSAMAAN saat Akad (bukan direklas belakangan).
func (s *Service) buildEvent3Lines(
	acc resolvedAccounts,
	projectID, unitID uint64,
	phaseID *uint64,
	salePrice, ppn, advance domain.Money,
	financingAmount, receivableAmount domain.Money,
) []JournalLineInput {
	pid := projectID
	uid := unitID
	var lines []JournalLineInput

	// Dr Uang Muka Penjualan (jika ada advance)
	if !advance.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.ump, Debit: advance,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "nolkan uang muka penjualan saat BAST",
		})
	}

	// Dr Dana Jaminan Bank (KPR) — hanya kontrak KPR-financed (Item 7A)
	if !financingAmount.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.financingReceivable, Debit: financingAmount,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "Dana Jaminan Bank (KPR) saat Akad — Nilai Persetujuan KPR Bank",
		})
	}

	// Dr Piutang (sisa di luar Dana Jaminan Bank, bruto jika PKP)
	if !receivableAmount.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.piutang, Debit: receivableAmount,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "piutang usaha saat BAST",
		})
	}

	// Cr Pendapatan Penjualan Unit
	lines = append(lines, JournalLineInput{
		AccountID: acc.pendapatan, Credit: salePrice,
		ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
		Description: "pendapatan penjualan unit saat BAST",
	})

	// Cr PPN Keluaran (jika PKP)
	if !ppn.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.ppnKeluar, Credit: ppn,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "PPN keluaran saat BAST",
		})
	}

	return lines
}

// buildLandAdvanceNettingLines membangun jurnal penolan (netting) bagian
// Uang Muka Penjualan yang secara ekonomi milik Kelebihan Tanah — bug
// 2026-09-04: pada kontrak Tunai lump-sum, termin pra-Akad rumah+tanah
// tercampur jadi satu Uang Muka Penjualan; Event 3 (buildEvent3Lines) hanya
// menolkan bagian rumah (dicap ke gross rumah), sehingga sisanya
// (landAdvance) harus dinolkan di sini terhadap piutang Kelebihan Tanah yang
// baru dibuat Akad ini. Dr UMP / Cr Piutang Kelebihan Tanah — balanced,
// tanpa baris negatif (dipanggil hanya saat landAdvance > 0).
func (s *Service) buildLandAdvanceNettingLines(
	umpAccountID, landReceivableAccountID uint64,
	projectID, unitID uint64,
	phaseID *uint64,
	landAdvance domain.Money,
) []JournalLineInput {
	pid := projectID
	uid := unitID
	return []JournalLineInput{
		{
			AccountID: umpAccountID, Debit: landAdvance,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "nolkan uang muka penjualan (bagian Kelebihan Tanah) saat BAST",
		},
		{
			AccountID: landReceivableAccountID, Credit: landAdvance,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "netting piutang Kelebihan Tanah dari uang muka pra-Akad",
		},
	}
}

// buildEvent4Lines membangun baris jurnal Event 4 (pengakuan HPP).
// Cr Persediaan dipecah per kategori sesuai UnitCostBreakdown (Invariant #4).
func (s *Service) buildEvent4Lines(
	ctx context.Context,
	tenantID uint64,
	acc resolvedAccounts,
	projectID, unitID uint64,
	phaseID *uint64,
	hpp domain.UnitCostBreakdown,
) ([]JournalLineInput, error) {
	pid := projectID
	uid := unitID
	hppTotal := hpp.Total()

	lines := []JournalLineInput{
		{
			AccountID: acc.hpp, Debit: hppTotal,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "HPP unit saat BAST",
		},
	}

	// Cr per kategori Persediaan — hanya jika > 0 (Invariant: no zero lines)
	if !hpp.Land.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.land, Credit: hpp.Land,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "kredit Persediaan Tanah saat BAST",
		})
	}
	if !hpp.Hard.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.hard, Credit: hpp.Hard,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "kredit Persediaan Hard Cost saat BAST",
		})
	}
	if !hpp.Soft.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.soft, Credit: hpp.Soft,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "kredit Persediaan Soft Cost saat BAST",
		})
	}
	if !hpp.Financing.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.financing, Credit: hpp.Financing,
			ProjectID: &pid, PhaseID: phaseID, UnitID: &uid,
			Description: "kredit Persediaan Biaya Pembiayaan saat BAST",
		})
	}

	return lines, nil
}

// ── PaymentSchedule methods (Phase 7) ────────────────────────────────────────

// CreateContract membuat SaleContract baru (tenant-scoped, Invariant #6).
func (s *Service) CreateContract(ctx context.Context, tenantID uint64, req CreateContractRequest) (*SaleContract, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	if req.UnitID == 0 {
		return nil, ErrUnitRequired
	}
	if req.TotalPrice.IsZero() || req.TotalPrice.IsNeg() {
		return nil, ErrContractTotalPriceInvalid
	}
	if req.LandQuantityM2 != nil && !req.LandQuantityM2.IsPositive() {
		return nil, ErrLandQuantityInvalid
	}
	// payment_type legacy: kontrak ber-scheme boleh mengosongkannya — diisi
	// otomatis dari policy (mapping terpusat scheme.PolicyType.LegacyPaymentType,
	// approval note #4). Kontrak legacy tetap wajib kpr|tunai.
	schemeContract := s.schemeEnabled() && req.PaymentSchemeID != nil
	if !schemeContract || req.PaymentType != "" {
		if req.PaymentType != PaymentTypeKPR && req.PaymentType != PaymentTypeTunai {
			return nil, ErrInvalidPaymentType
		}
	}

	// Hitung GrossAmount: DPP + PPN untuk PKP; == DPP untuk non-PKP.
	// GrossAmount = jumlah yang DITAGIH ke buyer (dasar BuyerRemainingBalance).
	// Jurnal BAST tidak terpengaruh — Cr Pendapatan tetap DPP, Cr PPN terpisah.
	dpp := req.TotalPrice
	var grossAmount domain.Money
	if req.IsPKP && !req.VATRateSnapshot.IsZero() {
		grossDec := dpp.Decimal().Mul(decimal.NewFromInt(1).Add(req.VATRateSnapshot)).Round(0)
		grossAmount = domain.FromDecimal(grossDec)
	} else {
		grossAmount = dpp
	}

	// 000049: bekukan harga list unit ke kontrak (dasar derivasi diskon).
	// Best-effort — kegagalan load unit TIDAK menggagalkan kontrak (snapshot
	// nil = kontrak lama; diskon 0).
	var priceSnapshot *domain.Money
	if info, uerr := s.units.FindUnitSaleInfo(ctx, tenantID, req.UnitID); uerr == nil && !info.ListPrice.IsZero() {
		lp := info.ListPrice
		priceSnapshot = &lp
	}

	c := &SaleContract{
		TenantID:        tenantID,
		UnitID:          req.UnitID,
		BuyerName:       req.BuyerName,
		BuyerID:         req.BuyerID,
		PaymentType:     req.PaymentType,
		BankKPR:         req.BankKPR,
		LoanAmount:      req.LoanAmount,
		ContractDate:    req.ContractDate,
		DPPAmount:       dpp,
		IsPKP:           req.IsPKP,
		VATRateSnapshot: req.VATRateSnapshot,
		GrossAmount:     grossAmount,
		TotalPrice:      grossAmount, // alias gross — dipertahankan untuk backward-compat
	}
	c.UnitPriceSnapshot = priceSnapshot
	// CustomerID dibawa apa adanya di sini (bukan hanya di dalam
	// applySchemeToNewContract) karena land.ReserveTx di bawah (via
	// SaveContract, kontrak langsung dengan komponen Kelebihan Tanah) butuh
	// customer_id FK yang valid TERLEPAS dari scheme aktif atau tidak.
	// applySchemeToNewContract tetap men-set ulang nilai yang sama saat scheme
	// aktif (idempoten) sekaligus memvalidasi req.CustomerID != nil.
	c.CustomerID = req.CustomerID

	// kelebihan-tanah-konversi-kontrak-2026-08: komponen tanah (bila ada)
	// SELALU diambil dari req.LandQuantityM2 di sini — baik kontrak langsung
	// (BookingID nil, SaveContract mereservasi atomik lewat land.ReserveTx)
	// maupun konversi booking (BookingID != nil, ConvertWithContractAtomic
	// mereservasi atomik dengan pola yang sama). Booking TIDAK LAGI membawa
	// komponen tanahnya sendiri — dipilih di Konversi Kontrak, bukan Booking.
	c.LandQuantityM2 = req.LandQuantityM2

	// Increment 3: dengan scheme flow aktif, kontrak baru WAJIB scheme + customer
	// + sales person (approval note #4); params scheme dibekukan sebagai Terms
	// Snapshot; state awal = signed. Tanpa scheme flow (unit test lama) → legacy.
	if s.schemeEnabled() {
		if err := s.applySchemeToNewContract(ctx, tenantID, req, c); err != nil {
			return nil, err
		}
	}

	// Increment 7: konversi booking → kontrak. Pra-validasi dini (error jelas),
	// lalu kontrak DIBUAT DI DALAM tx konversi (atomik penuh: kontrak + reklas
	// titipan→uang muka + buyer credit + booking converted + unit booked→reserved).
	// Tanpa booking → jalur SaveContract lama.
	if req.BookingID != nil {
		if _, err := s.validateBookingForConversion(ctx, tenantID, *req.BookingID, req); err != nil {
			return nil, err
		}
		// Kelebihan Tanah (bila c.LandQuantityM2 != nil, diisi di atas dari
		// req.LandQuantityM2) direservasi atomik DI DALAM ConvertWithContractAtomic
		// — pola identik SaveContract, lihat booking_repo.go.
		if err := s.convertBookingWithContract(ctx, tenantID, *req.BookingID, c, req.CreatedBy); err != nil {
			return nil, fmt.Errorf("konversi booking: %w", err)
		}
	} else if err := s.contracts.SaveContract(ctx, c); err != nil {
		return nil, fmt.Errorf("simpan sale contract: %w", err)
	}
	if err := s.recordContractSignedEvent(ctx, c, req.CreatedBy); err != nil {
		return nil, fmt.Errorf("catat event contract_signed: %w", err)
	}

	// D3 (Increment 6): proyeksi lifecycle unit → ppjb (best-effort). Matriks
	// hanya mengizinkan reserved → ppjb; unit di status lain = no-op (gagal
	// proyeksi diabaikan, tidak membatalkan kontrak — pola milestone scheme).
	//
	// Jalur langsung "Buat Kontrak" (tanpa booking) berangkat dari available,
	// bukan reserved — matriks tidak mengizinkan available → ppjb langsung.
	// Dahulukan available → reserved ("shortcut cash", matriks model.go) secara
	// best-effort juga; no-op aman untuk kontrak dari booking (unit sudah
	// reserved oleh convertBookingWithContract).
	if s.unitTransitioner != nil {
		_, _ = s.unitTransitioner.Transition(ctx, tenantID, c.UnitID, project.TransitionRequest{
			Next:          project.UnitStatusReserved,
			Event:         project.EventReservationConfirmed,
			EventDate:     &c.ContractDate,
			ActorID:       req.CreatedBy,
			ReferenceType: project.RefTypeSaleContract,
			ReferenceID:   &c.ID,
		})
		_, _ = s.unitTransitioner.Transition(ctx, tenantID, c.UnitID, project.TransitionRequest{
			Next:          project.UnitStatusPPJB,
			Event:         project.EventContractSigned,
			EventDate:     &c.ContractDate,
			ActorID:       req.CreatedBy,
			ReferenceType: project.RefTypeSaleContract,
			ReferenceID:   &c.ID,
		})
	}
	return c, nil
}

// CreatePaymentSchedule membuat baris jadwal cicilan untuk sebuah kontrak.
// Status awal setiap baris adalah ScheduleStatusScheduled.
//
// Kontrak ber-scheme (Increment 3): Σ(items) WAJIB == GrossAmount persis
// (policy mengusulkan via GetSchedulePlan; user boleh override — Σ tetap
// divalidasi), dan jadwal aktif tidak boleh dobel (ganti = schedule-regenerate).
// Kontrak legacy: perilaku lama tidak berubah.
func (s *Service) CreatePaymentSchedule(ctx context.Context, tenantID, contractID uint64, items []ScheduleItem) ([]*PaymentSchedule, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	contract, err := s.contracts.FindContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}

	version := 1
	if sctx, serr := s.schemeContextFor(ctx, contract); serr != nil {
		return nil, serr
	} else if sctx != nil {
		if err := validateScheduleSum(items, contract.GrossAmount); err != nil {
			return nil, err
		}
		existing, lerr := s.contracts.ListSchedulesByContract(ctx, tenantID, contractID)
		if lerr != nil {
			return nil, lerr
		}
		for _, row := range existing {
			if row.Status != ScheduleStatusSuperseded {
				return nil, ErrActiveScheduleExists
			}
		}
		maxVer, verr := s.schemeFlow.MaxScheduleVersion(ctx, tenantID, contractID)
		if verr != nil {
			return nil, verr
		}
		version = maxVer + 1
	}

	rows := make([]*PaymentSchedule, len(items))
	for i, item := range items {
		rows[i] = &PaymentSchedule{
			TenantID:          tenantID,
			SaleContractID:    contractID,
			UnitID:            contract.UnitID,
			InstallmentNumber: item.InstallmentNumber,
			DueDate:           item.DueDate,
			Amount:            item.Amount,
			Type:              item.Type,
			Status:            ScheduleStatusScheduled,
			ScheduleVersion:   version,
		}
	}
	if err := s.contracts.SaveScheduleItems(ctx, rows); err != nil {
		return nil, fmt.Errorf("simpan jadwal cicilan: %w", err)
	}
	return rows, nil
}

// RecordInstallmentPaid adalah ADAPTER KOMPATIBILITAS untuk endpoint lama
// POST /schedules/{id}/received. Tidak lagi memposting jurnal sendiri —
// MENDELEGASIKAN ke ReceivePayment (engine penerimaan tunggal). Membayar sisa
// cicilan secara penuh dan mengembalikan schedule terbaru (kontrak respons lama).
func (s *Service) RecordInstallmentPaid(ctx context.Context, tenantID uint64, req RecordInstallmentPaidRequest) (*PaymentSchedule, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	schedule, err := s.contracts.FindScheduleByID(ctx, tenantID, req.ScheduleID)
	if err != nil {
		return nil, err
	}
	if schedule.Status == ScheduleStatusReceived {
		return nil, ErrInstallmentAlreadyReceived
	}

	remaining := schedule.Amount.Sub(schedule.PaidAmount)
	scheduleID := req.ScheduleID
	if _, err := s.ReceivePayment(ctx, tenantID, ReceivePaymentRequest{
		Source:          PaymentSourceScheduleReceived,
		ScheduleID:      &scheduleID,
		Amount:          remaining,
		Date:            req.ReceivedAt,
		BankAccountCode: req.BankAccountCode,
		Notes:           req.Description,
	}); err != nil {
		return nil, err
	}

	// Kembalikan schedule terbaru (backward-compat dengan respons lama).
	return s.contracts.FindScheduleByID(ctx, tenantID, req.ScheduleID)
}

// MarkOverdue menandai semua jadwal berstatus scheduled yang jatuh tempo sebelum `now` menjadi overdue.
// Parameter `now` diinjektabel — TIDAK memanggil time.Now() secara langsung (deterministik).
// Mengembalikan jumlah baris yang diubah statusnya.
//
// T-4 (keputusan klien 2026-08-05): job ini TIDAK LAGI menjadi sumber angka
// laporan mana pun. Dashboard dan AR Aging kini sama-sama menurunkan tunggakan
// dari tanggal + sisa cicilan (BuildARAging), sehingga tidak ada laporan yang
// menampilkan "0 tunggakan" hanya karena job belum jalan. Job dipertahankan
// sebagai housekeeping kolom status (dipakai UI/filter operasional).
func (s *Service) MarkOverdue(ctx context.Context, tenantID uint64, now time.Time) (int, error) {
	if err := s.requireContractStore(); err != nil {
		return 0, err
	}
	candidates, err := s.contracts.ListScheduledBefore(ctx, tenantID, now)
	if err != nil {
		return 0, fmt.Errorf("list scheduled before %v: %w", now, err)
	}
	count := 0
	for _, item := range candidates {
		if err := s.contracts.UpdateScheduleStatus(ctx, tenantID, item.ID, ScheduleStatusOverdue, nil, nil); err != nil {
			return count, fmt.Errorf("tandai overdue ID=%d: %w", item.ID, err)
		}
		count++
	}
	return count, nil
}

// BuyerRemainingBalance menghitung sisa hutang buyer = GrossAmount − Σ termin yang sudah diterima.
// Untuk PKP: GrossAmount = DPP + PPN (bruto, konsisten dengan tagihan buyer).
// Untuk non-PKP: GrossAmount == DPP; perilaku identik dengan sebelumnya.
func (s *Service) BuyerRemainingBalance(ctx context.Context, tenantID, contractID uint64) (domain.Money, error) {
	// SATU rumus dengan receipt/invoice/dll — delegasi ke financial summary
	// (hardening: tidak boleh ada dua jalur hitung Terutang).
	sum, err := s.ContractFinancialSummaryByID(ctx, tenantID, contractID)
	if err != nil {
		return domain.Zero, err
	}
	return sum.Outstanding, nil
}

// TotalCollectedByUnit menjumlah semua termin yang sudah diterima untuk sebuah unit.
func (s *Service) TotalCollectedByUnit(ctx context.Context, tenantID, unitID uint64) (domain.Money, error) {
	return s.store.SumTerminsByUnit(ctx, tenantID, unitID)
}

// ListTerminsByUnit mengembalikan semua penerimaan (termin) sebuah unit, urut tanggal.
func (s *Service) ListTerminsByUnit(ctx context.Context, tenantID, unitID uint64) ([]*TerminPayment, error) {
	return s.store.ListTerminsByUnit(ctx, tenantID, unitID)
}

// ListDueInPeriod mengembalikan jadwal cicilan yang jatuh tempo dalam rentang [from, to].
func (s *Service) ListDueInPeriod(ctx context.Context, tenantID uint64, from, to time.Time) ([]*PaymentSchedule, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	return s.contracts.ListDueInPeriod(ctx, tenantID, from, to)
}

// GetContractByUnitID mengembalikan kontrak aktif untuk sebuah unit.
func (s *Service) GetContractByUnitID(ctx context.Context, tenantID, unitID uint64) (*SaleContract, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	return s.contracts.FindContractByUnitID(ctx, tenantID, unitID)
}

// ListSchedulesByContract mengembalikan semua jadwal cicilan untuk sebuah kontrak.
func (s *Service) ListSchedulesByContract(ctx context.Context, tenantID, contractID uint64) ([]*PaymentSchedule, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	return s.contracts.ListSchedulesByContract(ctx, tenantID, contractID)
}

// SetAdminMarketing (P1 — Sales ≠ Admin Marketing) menetapkan/mengubah/menghapus
// penanggung jawab administrasi kontrak (dokumen/KPR/follow-up), independen dari
// SalesPersonID. nil menghapus penugasan. Reuse master sales_persons yang sama
// dengan Sales — internal/commission tidak pernah membaca kolom ini.
func (s *Service) SetAdminMarketing(ctx context.Context, tenantID, contractID uint64, adminMarketingPersonID *uint64) (*SaleContract, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	if adminMarketingPersonID != nil && s.parties != nil {
		if err := s.parties.SalesPersonExists(ctx, tenantID, *adminMarketingPersonID); err != nil {
			return nil, err
		}
	}
	c, err := s.contracts.FindContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	c.AdminMarketingPersonID = adminMarketingPersonID
	if err := s.contracts.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}
