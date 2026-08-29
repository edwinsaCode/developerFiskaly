package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// ── Interfaces (implemented by GORMRepository) ────────────────────────────────

type ContractLoader interface {
	LoadContractInfo(ctx context.Context, tenantID, contractID uint64) (*ContractInfo, error)
}

type ScheduleLoader interface {
	LoadScheduleInfo(ctx context.Context, tenantID, scheduleID uint64) (*ScheduleInfo, error)
}

type InvoiceStore interface {
	CreateInvoice(ctx context.Context, inv *Invoice) error
	FindInvoiceByID(ctx context.Context, tenantID, id uint64) (*Invoice, error)
	ListByContract(ctx context.Context, tenantID, contractID uint64) ([]*Invoice, error)
	FindByScheduleID(ctx context.Context, tenantID, scheduleID uint64) (*Invoice, error)
	UpdateStatus(ctx context.Context, tenantID, id uint64, status InvoiceStatus) error
}

type PrintLoader interface {
	LoadPrintData(ctx context.Context, tenantID, invoiceID uint64) (*PrintData, error)
}

// ── Service ───────────────────────────────────────────────────────────────────

type Service struct {
	contracts ContractLoader
	schedules ScheduleLoader
	invoices  InvoiceStore
	prints    PrintLoader
	summary   ContractSummaryProvider // opsional (hardening) — nil = tanpa ringkasan
	policy    TenantPolicyReader      // opsional (R1) — nil = auto-shortfall off
}

// SetContractSummaryProvider memasang sumber ringkasan finansial kontrak
// (adapter sale.Service di wiring). Additive & opsional — nil aman.
func (s *Service) SetContractSummaryProvider(p ContractSummaryProvider) { s.summary = p }

func NewService(contracts ContractLoader, schedules ScheduleLoader, invoices InvoiceStore, prints PrintLoader) *Service {
	return &Service{contracts: contracts, schedules: schedules, invoices: invoices, prints: prints}
}

// GenerateInvoice creates an invoice for a specific payment schedule.
func (s *Service) GenerateInvoice(
	ctx context.Context,
	tenantID, contractID, createdBy uint64,
	req GenerateInvoiceRequest,
) (*Invoice, error) {
	// Validate contract exists and belongs to this tenant.
	if _, err := s.contracts.LoadContractInfo(ctx, tenantID, contractID); err != nil {
		return nil, fmt.Errorf("kontrak: %w", err)
	}

	// Load the payment schedule.
	sched, err := s.schedules.LoadScheduleInfo(ctx, tenantID, req.ScheduleID)
	if err != nil {
		return nil, fmt.Errorf("jadwal: %w", err)
	}

	// Schedule must belong to this contract.
	if sched.SaleContractID != contractID {
		return nil, ErrScheduleContractMismatch
	}

	// Cannot invoice a schedule that is already paid.
	if sched.Status == "received" {
		return nil, ErrScheduleAlreadyReceived
	}

	// Only one invoice allowed per schedule.
	existing, err := s.invoices.FindByScheduleID(ctx, tenantID, req.ScheduleID)
	if err != nil && !errors.Is(err, ErrInvoiceNotFound) {
		return nil, fmt.Errorf("cek invoice existing: %w", err)
	}
	if existing != nil {
		return nil, ErrInvoiceAlreadyExists
	}

	issueDate := req.IssueDate
	if issueDate.IsZero() {
		issueDate = time.Now()
	}

	schedID := req.ScheduleID
	inv := &Invoice{
		TenantID:       tenantID,
		SaleContractID: contractID,
		ScheduleID:     &schedID,
		InvoiceType:    scheduleTypeToInvoiceType(sched.Type),
		IssueDate:      issueDate,
		DueDate:        sched.DueDate,
		Amount:         sched.Amount,
		Status:         StatusIssued,
		Notes:          req.Notes,
		CreatedBy:      createdBy,
	}

	if err := s.invoices.CreateInvoice(ctx, inv); err != nil {
		return nil, fmt.Errorf("simpan invoice: %w", err)
	}
	return inv, nil
}

// ── R1: Invoice Kekurangan Pembayaran ─────────────────────────────────────────

// GenerateShortfallInvoice membuat invoice KEKURANGAN sebesar Outstanding
// kontrak (SATU rumus — ContractFinancialSummary via provider). Aturan:
//   - Outstanding harus > 0 (ErrNoOutstanding).
//   - Maksimal SATU invoice KEKURANGAN yang belum dibayar per kontrak
//     (ErrShortfallInvoiceExists) — mencegah tagihan ganda saat pencairan
//     bertahap; invoice lama harus diselesaikan/dibatalkan dulu.
// TIDAK memposting jurnal (invoice = dokumen tagihan, bukan pengakuan).
func (s *Service) GenerateShortfallInvoice(ctx context.Context, tenantID, contractID, createdBy uint64, dueDate time.Time, notes string) (*Invoice, error) {
	if s.summary == nil {
		return nil, fmt.Errorf("ringkasan kontrak tidak tersedia (provider belum terpasang)")
	}
	if _, err := s.contracts.LoadContractInfo(ctx, tenantID, contractID); err != nil {
		return nil, fmt.Errorf("kontrak: %w", err)
	}
	cs, err := s.summary.SummaryByContractID(ctx, tenantID, contractID)
	if err != nil {
		return nil, fmt.Errorf("hitung outstanding: %w", err)
	}
	if cs.Outstanding.IsZero() || cs.Outstanding.IsNeg() {
		return nil, ErrNoOutstanding
	}
	// Dedup: satu KEKURANGAN unpaid per kontrak.
	existing, err := s.invoices.ListByContract(ctx, tenantID, contractID)
	if err != nil {
		return nil, fmt.Errorf("cek invoice existing: %w", err)
	}
	for _, inv := range existing {
		if inv.InvoiceType == TypeKekurangan && inv.Status != StatusPaid && inv.Status != StatusCancelled {
			return nil, ErrShortfallInvoiceExists
		}
	}
	if dueDate.IsZero() {
		dueDate = time.Now().AddDate(0, 0, 14) // default: 14 hari
	}
	inv := &Invoice{
		TenantID:       tenantID,
		SaleContractID: contractID,
		ScheduleID:     nil, // tidak terikat cicilan — tagihan sisa kontrak
		InvoiceType:    TypeKekurangan,
		IssueDate:      time.Now(),
		DueDate:        dueDate,
		Amount:         cs.Outstanding,
		Status:         StatusIssued,
		Notes:          notes,
		CreatedBy:      createdBy,
	}
	if err := s.invoices.CreateInvoice(ctx, inv); err != nil {
		return nil, fmt.Errorf("simpan invoice kekurangan: %w", err)
	}
	return inv, nil
}

// ── Billing Batch 2: Invoice REALISASI per charge group ──────────────────────

// GenerateChargeGroupInvoice membuat invoice REALISASI untuk satu grup tagihan.
// Nominal `outstanding` WAJIB dipasok pemanggil dari formula kanonik grup
// (charge.Service.GroupSummary) — billing tidak menghitung outstanding grup
// sendiri (no duplicate SoT). Aturan pola KEKURANGAN: outstanding > 0, maksimal
// SATU invoice REALISASI unpaid per grup. TIDAK memposting jurnal.
func (s *Service) GenerateChargeGroupInvoice(ctx context.Context, tenantID, contractID, chargeGroupID, createdBy uint64, outstanding domain.Money, dueDate time.Time, notes string) (*Invoice, error) {
	if outstanding.IsZero() || outstanding.IsNeg() {
		return nil, ErrChargeNoOutstanding
	}
	if _, err := s.contracts.LoadContractInfo(ctx, tenantID, contractID); err != nil {
		return nil, fmt.Errorf("kontrak: %w", err)
	}
	existing, err := s.invoices.ListByContract(ctx, tenantID, contractID)
	if err != nil {
		return nil, fmt.Errorf("cek invoice existing: %w", err)
	}
	for _, inv := range existing {
		if inv.InvoiceType == TypeRealisasi && inv.ChargeGroupID != nil && *inv.ChargeGroupID == chargeGroupID &&
			inv.Status != StatusPaid && inv.Status != StatusCancelled {
			return nil, ErrChargeInvoiceExists
		}
	}
	if dueDate.IsZero() {
		dueDate = time.Now().AddDate(0, 0, 14)
	}
	gid := chargeGroupID
	inv := &Invoice{
		TenantID:       tenantID,
		SaleContractID: contractID,
		ChargeGroupID:  &gid,
		InvoiceType:    TypeRealisasi,
		IssueDate:      time.Now(),
		DueDate:        dueDate,
		Amount:         outstanding,
		Status:         StatusIssued,
		Notes:          notes,
		CreatedBy:      createdBy,
	}
	if err := s.invoices.CreateInvoice(ctx, inv); err != nil {
		// INV-CG-1 dijaga ganda: pemeriksaan di atas untuk pesan yang ramah,
		// UNIQUE (tenant_id, charge_group_live) untuk balapan dua request yang
		// lolos pemeriksaan bersamaan. Yang kedua harus keluar sebagai aturan
		// bisnis yang sama, bukan sebagai error driver.
		if isDuplicateKeyError(err) {
			return nil, ErrChargeInvoiceExists
		}
		return nil, fmt.Errorf("simpan invoice realisasi: %w", err)
	}
	return inv, nil
}

// InvoiceTxStore adalah kemampuan TAMBAHAN menerbitkan invoice di dalam
// transaksi pemanggil (W-5). Dipisah dari InvoiceStore supaya implementasi test
// yang tidak berbasis DB tetap sah — jalur in-tx memang hanya berlaku untuk
// penyimpanan nyata.
type InvoiceTxStore interface {
	CreateInvoiceInTx(ctx context.Context, tx *gorm.DB, inv *Invoice) error
	ListByContractInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID uint64) ([]*Invoice, error)
}

// ErrInvoiceTxUnsupported — penyimpanan invoice tidak mendukung jalur in-tx.
var ErrInvoiceTxUnsupported = errors.New("penyimpanan invoice tidak mendukung transaksi bersama")

// liveChargeInvoiceInTx mencari invoice REALISASI yang masih hidup untuk sebuah
// grup (INV-CG-1: maksimal satu per grup).
func liveChargeInvoiceInTx(invs []*Invoice, chargeGroupID uint64) *Invoice {
	for _, inv := range invs {
		if inv.InvoiceType == TypeRealisasi && inv.ChargeGroupID != nil && *inv.ChargeGroupID == chargeGroupID &&
			inv.Status != StatusPaid && inv.Status != StatusCancelled {
			return inv
		}
	}
	return nil
}

// FindLiveChargeInvoiceInTx mengembalikan invoice realisasi yang masih hidup
// untuk sebuah grup — id 0 bila belum ada. Dipakai jalur BAST untuk membedakan
// "belum pernah ditagihkan" (terbitkan) dari "sudah ditagihkan" (lewati) tanpa
// memperlakukan aturan INV-CG-1 sebagai error, dan dipakai jurnal susulan R-3
// untuk mengakui piutang invoice yang terbit sebelum W-5.
func (s *Service) FindLiveChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID uint64) (uint64, string, time.Time, error) {
	store, ok := s.invoices.(InvoiceTxStore)
	if !ok {
		return 0, "", time.Time{}, ErrInvoiceTxUnsupported
	}
	invs, err := store.ListByContractInTx(ctx, tx, tenantID, contractID)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	inv := liveChargeInvoiceInTx(invs, chargeGroupID)
	if inv == nil {
		return 0, "", time.Time{}, nil
	}
	return inv.ID, inv.InvoiceNumber, inv.DueDate, nil
}

// GenerateChargeGroupInvoiceInTx sama dengan GenerateChargeGroupInvoice, tetapi
// menumpang transaksi pemanggil — sejak W-5 penerbitan invoice realisasi dan
// jurnal pengakuan piutangnya wajib atomik.
//
// Validasi kontrak sengaja tidak diulang di sini: pemanggilnya (charge) sudah
// membaca grup beserta kontraknya dari baris yang terkunci di transaksi yang
// sama, jadi mengulangnya hanya menambah query tanpa menambah jaminan.
func (s *Service) GenerateChargeGroupInvoiceInTx(ctx context.Context, tx *gorm.DB,
	tenantID, contractID, chargeGroupID, createdBy uint64, outstanding domain.Money, dueDate time.Time, notes string) (*Invoice, error) {

	if outstanding.IsZero() || outstanding.IsNeg() {
		return nil, ErrChargeNoOutstanding
	}
	store, ok := s.invoices.(InvoiceTxStore)
	if !ok {
		return nil, ErrInvoiceTxUnsupported
	}
	existing, err := store.ListByContractInTx(ctx, tx, tenantID, contractID)
	if err != nil {
		return nil, fmt.Errorf("cek invoice existing: %w", err)
	}
	if liveChargeInvoiceInTx(existing, chargeGroupID) != nil {
		return nil, ErrChargeInvoiceExists
	}
	if dueDate.IsZero() {
		dueDate = time.Now().AddDate(0, 0, 14)
	}
	gid := chargeGroupID
	inv := &Invoice{
		TenantID:       tenantID,
		SaleContractID: contractID,
		ChargeGroupID:  &gid,
		InvoiceType:    TypeRealisasi,
		IssueDate:      time.Now(),
		DueDate:        dueDate,
		Amount:         outstanding,
		Status:         StatusIssued,
		Notes:          notes,
		CreatedBy:      createdBy,
	}
	if err := store.CreateInvoiceInTx(ctx, tx, inv); err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrChargeInvoiceExists
		}
		return nil, fmt.Errorf("simpan invoice realisasi: %w", err)
	}
	return inv, nil
}

// isDuplicateKeyError mendeteksi pelanggaran UNIQUE MySQL (error 1062).
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var me *mysqldriver.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return strings.Contains(err.Error(), "Duplicate entry")
}

// SettleChargeInvoiceIfPaid menandai invoice REALISASI grup menjadi paid bila
// outstanding kanonik grup (dipasok pemanggil) sudah nol. Best-effort — tidak
// pernah menggagalkan pembayaran (pola SettleShortfallIfPaid).
func (s *Service) SettleChargeInvoiceIfPaid(ctx context.Context, tenantID, contractID, chargeGroupID uint64, outstanding domain.Money) {
	if !(outstanding.IsZero() || outstanding.IsNeg()) {
		return
	}
	invs, err := s.invoices.ListByContract(ctx, tenantID, contractID)
	if err != nil {
		return
	}
	for _, inv := range invs {
		if inv.InvoiceType == TypeRealisasi && inv.ChargeGroupID != nil && *inv.ChargeGroupID == chargeGroupID &&
			(inv.Status == StatusIssued || inv.Status == StatusOverdue) {
			_ = s.invoices.UpdateStatus(ctx, tenantID, inv.ID, StatusPaid)
		}
	}
}

// TenantPolicyReader membaca kebijakan tenant (diimplementasikan GORMRepository).
type TenantPolicyReader interface {
	AutoShortfallInvoiceEnabled(ctx context.Context, tenantID uint64) (bool, error)
}

// SetTenantPolicyReader memasang pembaca kebijakan tenant (wiring; opsional).
func (s *Service) SetTenantPolicyReader(r TenantPolicyReader) { s.policy = r }

// MaybeAutoShortfallInvoice membuat invoice kekurangan OTOMATIS bila tenant
// mengaktifkan kebijakan auto_shortfall_invoice (default OFF — manual via CTA).
// Best-effort: ErrNoOutstanding / ErrShortfallInvoiceExists diabaikan diam-diam.
func (s *Service) MaybeAutoShortfallInvoice(ctx context.Context, tenantID, contractID, createdBy uint64) {
	if s.policy == nil {
		return
	}
	enabled, err := s.policy.AutoShortfallInvoiceEnabled(ctx, tenantID)
	if err != nil || !enabled {
		return
	}
	_, _ = s.GenerateShortfallInvoice(ctx, tenantID, contractID, createdBy, time.Time{}, "Dibuat otomatis pasca pencairan bank (kebijakan tenant)")
}

// SettleShortfallIfPaid implements sale.ShortfallInvoicer: bila outstanding
// kontrak (satu rumus — ContractFinancialSummary) sudah nol, seluruh invoice
// KEKURANGAN yang masih terbuka ditandai paid. Best-effort — tidak pernah
// menggagalkan pembayaran; invoice kekurangan tak terikat schedule sehingga
// jalur MarkPaidByScheduleID tidak menjangkaunya.
func (s *Service) SettleShortfallIfPaid(ctx context.Context, tenantID, contractID uint64) {
	if s.summary == nil {
		return
	}
	cs, err := s.summary.SummaryByContractID(ctx, tenantID, contractID)
	if err != nil || !(cs.Outstanding.IsZero() || cs.Outstanding.IsNeg()) {
		return
	}
	invs, err := s.invoices.ListByContract(ctx, tenantID, contractID)
	if err != nil {
		return
	}
	for _, inv := range invs {
		if inv.InvoiceType == TypeKekurangan &&
			(inv.Status == StatusIssued || inv.Status == StatusOverdue) {
			_ = s.invoices.UpdateStatus(ctx, tenantID, inv.ID, StatusPaid)
		}
	}
}

func (s *Service) GetInvoice(ctx context.Context, tenantID, id uint64) (*Invoice, error) {
	return s.invoices.FindInvoiceByID(ctx, tenantID, id)
}

func (s *Service) ListByContract(ctx context.Context, tenantID, contractID uint64) ([]*Invoice, error) {
	return s.invoices.ListByContract(ctx, tenantID, contractID)
}

// GetPrintData returns the fully-joined data needed to render an invoice print page.
func (s *Service) GetPrintData(ctx context.Context, tenantID, invoiceID uint64) (*PrintData, error) {
	if s.prints == nil {
		return nil, ErrInvoiceNotFound
	}
	data, err := s.prints.LoadPrintData(ctx, tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	// Hardening: ringkasan finansial kontrak — sumber sama dengan receipt.
	// Best-effort: kegagalan ringkasan tidak menggagalkan cetak invoice.
	if s.summary != nil {
		if inv, ierr := s.invoices.FindInvoiceByID(ctx, tenantID, invoiceID); ierr == nil && inv != nil {
			if cs, serr := s.summary.SummaryByContractID(ctx, tenantID, inv.SaleContractID); serr == nil && cs != nil {
				data.HasSummary = true
				data.UnitPrice = cs.UnitPrice
				data.Discount = cs.Discount
				data.NetContract = cs.NetContract
				data.TotalPaid = cs.TotalPaid
				data.Outstanding = cs.Outstanding
				data.PriceIsSnapshot = cs.PriceIsSnapshot
			}
		}
	}
	return data, nil
}

// MarkPaidByScheduleID implements sale.InvoiceStatusUpdater.
// Called by sale.Handler after a payment schedule is marked received.
// Errors are intentionally ignored by the caller — invoice sync is best-effort.
func (s *Service) MarkPaidByScheduleID(ctx context.Context, tenantID, scheduleID uint64) error {
	inv, err := s.invoices.FindByScheduleID(ctx, tenantID, scheduleID)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			return nil // no invoice for this schedule — nothing to update
		}
		return err
	}
	if inv.Status == StatusPaid || inv.Status == StatusCancelled {
		return nil // already terminal
	}
	return s.invoices.UpdateStatus(ctx, tenantID, inv.ID, StatusPaid)
}

// invoiceTxStore adalah kapabilitas tx-aware opsional dari InvoiceStore
// (diimplementasikan GORMRepository) — ditemukan via type assertion agar
// interface InvoiceStore tidak perlu diperluas (mock billing tak terpengaruh).
type invoiceTxStore interface {
	FindByScheduleIDInTx(ctx context.Context, tx *gorm.DB, tenantID, scheduleID uint64) (*Invoice, error)
	UpdateStatusInTx(ctx context.Context, tx *gorm.DB, tenantID, id uint64, status InvoiceStatus) error
}

// MarkPaidByScheduleIDInTx adalah varian tx-aware dari MarkPaidByScheduleID:
// menandai invoice cicilan menjadi paid MEMAKAI transaksi yang diberikan, agar
// sinkron invoice ikut atomik dengan penerimaan pembayaran (sale.InvoiceTxUpdater).
func (s *Service) MarkPaidByScheduleIDInTx(ctx context.Context, tx *gorm.DB, tenantID, scheduleID uint64) error {
	store, ok := s.invoices.(invoiceTxStore)
	if !ok {
		// Fallback (mestinya tak terjadi di produksi): best-effort non-tx.
		return s.MarkPaidByScheduleID(ctx, tenantID, scheduleID)
	}
	inv, err := store.FindByScheduleIDInTx(ctx, tx, tenantID, scheduleID)
	if err != nil {
		if errors.Is(err, ErrInvoiceNotFound) {
			return nil // tidak ada invoice untuk cicilan ini — tidak ada yang diubah
		}
		return err
	}
	if inv.Status == StatusPaid || inv.Status == StatusCancelled {
		return nil
	}
	return store.UpdateStatusInTx(ctx, tx, tenantID, inv.ID, StatusPaid)
}
