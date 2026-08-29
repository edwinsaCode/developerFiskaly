package charge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
)

func gormLockingUpdate() clause.Locking { return clause.Locking{Strength: "UPDATE"} }

// ── Seams (di-wire di main; charge tidak menggandakan engine lain) ───────────

// ReceiptTxGenerator — kwitansi KWR dibuat DI DALAM transaksi pembayaran
// (implementasi: billing.ReceiptService via adapter — pola sale.ReceiptTxGenerator).
type ReceiptTxGenerator interface {
	GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64, amount domain.Money, bankAccountCode string, date time.Time, notes string) (receiptNumber string, receiptID uint64, err error)
}

// HouseTransferApplier — transfer titipan → pembayaran harga rumah lewat pintu
// pembayaran yang ADA (sale.ApplyDepositTransfer). Tidak ada engine kedua.
type HouseTransferApplier interface {
	ApplyDepositTransfer(ctx context.Context, tenantID uint64, req sale.DepositTransferRequest) (*sale.ReceivePaymentResult, error)
}

// InvoiceIssuer — invoice REALISASI (billing.Service; nominal dari formula
// kanonik grup yang DIPASOK charge, billing tidak menghitung sendiri).
//
// W-5: penerbitannya kini menumpang transaksi charge. Sejak piutang lahir dari
// invoice (R-5), invoice dan jurnal pengakuannya tidak boleh bisa terpisah —
// invoice tanpa jurnal = tagihan tanpa piutang; jurnal tanpa invoice = piutang
// tanpa dokumen.
type InvoiceIssuer interface {
	IssueChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID, createdBy uint64, outstanding domain.Money, dueDate time.Time, notes string) (invoiceID uint64, invoiceNumber string, effectiveDue time.Time, err error)
	// FindLiveChargeInvoiceInTx mengembalikan invoice realisasi hidup grup —
	// invoiceID 0 bila belum ada.
	FindLiveChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID uint64) (invoiceID uint64, invoiceNumber string, due time.Time, err error)
	SettleChargeInvoiceIfPaid(ctx context.Context, tenantID, contractID, chargeGroupID uint64, outstanding domain.Money)
}

// Service adalah SATU-SATUNYA penulis lifecycle charge group.
type Service struct {
	db            *gorm.DB
	receiptTx     ReceiptTxGenerator
	houseTransfer HouseTransferApplier
	invoices      InvoiceIssuer
	// products — katalog produk untuk item addon (W-13). Lihat addon.go.
	products ProductPolicyResolver
}

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) SetReceiptTxGenerator(g ReceiptTxGenerator)     { s.receiptTx = g }
func (s *Service) SetHouseTransferApplier(a HouseTransferApplier) { s.houseTransfer = a }
func (s *Service) SetInvoiceIssuer(i InvoiceIssuer)               { s.invoices = i }

// ── Memo transfer internal (T-1) ────────────────────────────────────────────
//
// W-2: nomor memo diambil langsung dari mesin penomoran dokumen. Sebelumnya
// lewat seam `MemoNumberer` yang di-wire ke billing — seam itu ada semata-mata
// agar charge tidak import billing. Setelah `document` menjadi package netral,
// perantaranya justru menyamarkan aturan yang ingin dijaga: SATU engine untuk
// semua dokumen. Sekarang aturan itu bisa dibuktikan dengan grep.
//
// FAIL-CLOSED tetap: jenis dokumen belum ada / nonaktif → transfer ditolak,
// bukan berjalan tanpa dokumen.

// nextMemo mengalokasikan satu nomor memo transfer internal.
func (s *Service) nextMemo(tx *gorm.DB, tenantID uint64, at time.Time) (*document.Allocation, error) {
	alloc, err := document.Allocate(tx, tenantID, document.TypeInternalTransfer, at)
	if err != nil {
		return nil, fmt.Errorf("nomor memo transfer: %w", err)
	}
	return alloc, nil
}

// registerMemo mencatat memo ke registry dokumen setelah baris settlement lahir.
func registerMemo(tx *gorm.DB, tenantID uint64, alloc *document.Allocation, settlementID uint64, amount domain.Money, createdBy *uint64) error {
	_, err := document.Register(tx, tenantID, alloc,
		document.Source{Table: "charge_settlements", ID: settlementID}, amount, createdBy)
	if err != nil {
		return fmt.Errorf("catat memo transfer: %w", err)
	}
	return nil
}

// ── Create / Add / Cancel ────────────────────────────────────────────────────

func validateItems(items []NewItemInput) error {
	if len(items) == 0 {
		return ErrItemsRequired
	}
	for _, it := range items {
		if it.Label == "" {
			return ErrLabelRequired
		}
		if it.Amount.IsZero() || it.Amount.IsNeg() || !it.Amount.IsWholeRupiah() {
			return ErrAmountInvalid
		}
	}
	return nil
}

// resolveItemTypes (W-1) menegakkan master jenis biaya realisasi: setiap item
// BARU pada grup realization wajib menunjuk jenis biaya aktif. FAIL-CLOSED —
// tidak ada fallback diam-diam ke akun titipan default, karena jenis biayalah
// yang menentukan ke akun kewajiban mana uang customer mendarat.
//
// Grup addon punya masternya sendiri (W-13): katalog produk. Fail-closed-nya
// setara — yang berbeda hanya master yang ditanya dan akun yang dihasilkan.
//
// Mengembalikan, per indeks item, akun-akun yang akan DI-SNAPSHOT ke item. Akun
// boleh berbeda antar item dalam satu grup — itu justru intinya: memberi
// Notaris/BPHTB/PDAM/Listrik akun kewajiban masing-masing tidak menuntut
// perubahan arsitektur.
func (s *Service) resolveItemTypes(ctx context.Context, tx *gorm.DB, tenantID uint64, kind GroupKind, items []NewItemInput) ([]itemAccounts, error) {
	out := make([]itemAccounts, len(items))
	for i, in := range items {
		if kind == KindAddon {
			// W-13 — produk tambahan menunjuk katalog produk. Uangnya mendarat
			// di Uang Muka Penjualan (Invariant #7), bukan titipan pihak ketiga.
			pol, err := s.resolveAddonProduct(ctx, tenantID, in.ProductCode)
			if err != nil {
				return nil, fmt.Errorf("%w (item %q)", err, in.Label)
			}
			code, rev := pol.code, pol.revenue
			out[i] = itemAccounts{ProductCode: &code, Deposit: pol.deposit, Revenue: &rev}
			continue
		}
		code := normalizeChargeTypeCode(in.ChargeTypeCode)
		if code == "" {
			return nil, fmt.Errorf("%w: %q", ErrChargeTypeRequired, in.Label)
		}
		pol, err := resolveChargeTypePolicyDB(ctx, tx, tenantID, code)
		if err != nil {
			return nil, err
		}
		c := pol.Code
		out[i] = itemAccounts{ChargeTypeCode: &c, Deposit: pol.DepositAccountCode}
	}
	return out, nil
}

// itemAccounts adalah hasil resolusi master untuk SATU item: tautan ke master
// dan akun-akun yang di-snapshot ke baris item.
type itemAccounts struct {
	ChargeTypeCode *string // realisasi: master jenis biaya (W-1)
	ProductCode    *string // addon: master katalog produk (W-13)
	Deposit        string  // akun kewajiban saat kas diterima
	Revenue        *string // addon saja: akun pendapatan saat diakui
}

// apply menyalin hasil resolusi ke baris item yang akan disimpan.
func (a itemAccounts) apply(it *ChargeItem) {
	it.ChargeTypeCode = a.ChargeTypeCode
	it.ProductCode = a.ProductCode
	it.DepositAccountCode = a.Deposit
	it.RevenueAccountCode = a.Revenue
}

// CreateGroup membuat grup + item dalam satu transaksi. TIDAK memposting jurnal
// (tagihan = dokumen; kewajiban titipan baru lahir saat kas diterima — Invariant #7).
func (s *Service) CreateGroup(ctx context.Context, tenantID uint64, req CreateGroupRequest) (*GroupSummary, error) {
	if !req.Kind.Valid() {
		return nil, ErrKindInvalid
	}
	if req.Label == "" {
		return nil, ErrLabelRequired
	}
	if err := validateItems(req.Items); err != nil {
		return nil, err
	}
	var group *ChargeGroup
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		c, err := findContractDB(ctx, tx, tenantID, req.SaleContractID)
		if err != nil {
			return err
		}
		g := &ChargeGroup{
			TenantID:       tenantID,
			SaleContractID: c.ID,
			UnitID:         c.UnitID,
			Kind:           req.Kind,
			Label:          req.Label,
			Status:         GroupOpen,
			Notes:          req.Notes,
			CreatedBy:      req.CreatedBy,
		}
		// W-1/W-13: master di-resolve SEBELUM apa pun disimpan (fail-closed).
		accounts, err := s.resolveItemTypes(ctx, tx, tenantID, req.Kind, req.Items)
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Create(g).Error; err != nil {
			return fmt.Errorf("simpan grup: %w", err)
		}
		for i, in := range req.Items {
			item := &ChargeItem{
				TenantID:       tenantID,
				ChargeGroupID:  g.ID,
				Label:          in.Label,
				Amount:         in.Amount,
				OriginalAmount: in.Amount,
				DueDate:        in.DueDate,
				Status:         ItemOpen,
				CreatedBy:      req.CreatedBy,
			}
			accounts[i].apply(item)
			if err := tx.WithContext(ctx).Create(item).Error; err != nil {
				return fmt.Errorf("simpan item: %w", err)
			}
		}
		group = g
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GroupSummary(ctx, tenantID, group.ID)
}

// AddItems menambah item (Additional Charge — rule klien & K-5 manual path).
func (s *Service) AddItems(ctx context.Context, tenantID, groupID uint64, items []NewItemInput, createdBy *uint64) (*GroupSummary, error) {
	if err := validateItems(items); err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		g, err := findGroupDB(ctx, tx, tenantID, groupID, true)
		if err != nil {
			return err
		}
		if g.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		// W-1: jenis biaya wajib. Akun kewajibannya di-snapshot per item dan
		// boleh berbeda dari item yang sudah ada di grup — jurnalnya yang pecah
		// per akun, grupnya tetap satu tagihan (INV-CG-1).
		accounts, err := s.resolveItemTypes(ctx, tx, tenantID, g.Kind, items)
		if err != nil {
			return err
		}
		for i, in := range items {
			item := &ChargeItem{
				TenantID:       tenantID,
				ChargeGroupID:  g.ID,
				Label:          in.Label,
				Amount:         in.Amount,
				OriginalAmount: in.Amount,
				DueDate:        in.DueDate,
				Status:         ItemOpen,
				CreatedBy:      createdBy,
			}
			accounts[i].apply(item)
			if err := tx.WithContext(ctx).Create(item).Error; err != nil {
				return fmt.Errorf("simpan item: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GroupSummary(ctx, tenantID, groupID)
}

// itemHasMoneyDB: item pernah menerima alokasi atau payout?
func itemHasMoneyDB(ctx context.Context, tx *gorm.DB, tenantID, itemID uint64) (bool, error) {
	var n int64
	if err := tx.WithContext(ctx).Table("payment_allocations").
		Where("tenant_id = ? AND charge_item_id = ?", tenantID, itemID).
		Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := tx.WithContext(ctx).Table("charge_payouts").
		Where("tenant_id = ? AND charge_item_id = ?", tenantID, itemID).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// CancelItem membatalkan satu item — hanya bila belum tersentuh uang.
func (s *Service) CancelItem(ctx context.Context, tenantID, itemID uint64) (*GroupSummary, error) {
	var groupID uint64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item ChargeItem
		if err := tx.WithContext(ctx).Clauses(gormLockingUpdate()).
			Where("id = ? AND tenant_id = ?", itemID, tenantID).
			First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrItemNotFound
			}
			return err
		}
		if item.Status != ItemOpen {
			return ErrItemNotOpen
		}
		g, err := findGroupDB(ctx, tx, tenantID, item.ChargeGroupID, true)
		if err != nil {
			return err
		}
		if g.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		has, err := itemHasMoneyDB(ctx, tx, tenantID, item.ID)
		if err != nil {
			return err
		}
		if has {
			return ErrItemHasMoney
		}
		groupID = g.ID
		if err := tx.WithContext(ctx).Model(&ChargeItem{}).
			Where("id = ? AND tenant_id = ?", item.ID, tenantID).
			Update("status", string(ItemCancelled)).Error; err != nil {
			return err
		}
		// Item boleh sudah di-invoice tanpa pernah dibayar. Piutangnya harus
		// ikut batal — tagihan yang tidak lagi ada tidak boleh menyisakan klaim.
		_, err = s.syncRecognitionInTx(ctx, tx, tenantID, g, recognitionOpts{
			Reason:    ReasonRecCancelItem,
			Date:      time.Now(),
			Desc:      fmt.Sprintf("Pembatalan item titipan #%d — piutang realisasi dibalik", item.ID),
			CreatedBy: nil,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GroupSummary(ctx, tenantID, groupID)
}

// CancelGroup membatalkan grup — hanya bila belum tersentuh uang sama sekali.
func (s *Service) CancelGroup(ctx context.Context, tenantID, groupID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		g, err := findGroupDB(ctx, tx, tenantID, groupID, true)
		if err != nil {
			return err
		}
		if g.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		sum, err := groupSummaryDB(ctx, tx, tenantID, g)
		if err != nil {
			return err
		}
		if !sum.Paid.IsZero() || !sum.Payout.IsZero() || !sum.Returned.IsZero() {
			return ErrGroupHasMoney
		}
		if err := tx.WithContext(ctx).Model(&ChargeItem{}).
			Where("tenant_id = ? AND charge_group_id = ?", tenantID, groupID).
			Update("status", string(ItemCancelled)).Error; err != nil {
			return err
		}
		// Grup tanpa uang masih bisa sudah di-invoice: balikkan piutangnya.
		if _, err := s.syncRecognitionInTx(ctx, tx, tenantID, g, recognitionOpts{
			Reason: ReasonRecCancelGroup,
			Date:   time.Now(),
			Desc:   fmt.Sprintf("Pembatalan grup titipan #%d — piutang realisasi dibalik", g.ID),
		}); err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&ChargeGroup{}).
			Where("id = ? AND tenant_id = ?", groupID, tenantID).
			Update("status", string(GroupCancelled)).Error
	})
}

// ── Pembayaran (K-1 + K-3) ───────────────────────────────────────────────────

// validateAllocations menegakkan K-3: alokasi eksplisit admin; Σ == amount
// PERSIS (Invariant #3); per-item ≤ outstanding; item open milik grup.
// Dipanggil DI DALAM tx dengan item TERKUNCI (FOR UPDATE).
func validateAllocations(allocs []AllocationInput, amount domain.Money, items []*ChargeItem, paid map[uint64]domain.Money) error {
	if len(allocs) == 0 {
		return ErrAllocationsRequired
	}
	byID := make(map[uint64]*ChargeItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	seen := make(map[uint64]bool, len(allocs))
	sum := domain.Zero
	for _, a := range allocs {
		if a.Amount.IsZero() || a.Amount.IsNeg() || !a.Amount.IsWholeRupiah() {
			return ErrAmountInvalid
		}
		if seen[a.ChargeItemID] {
			return ErrAllocationDuplicate
		}
		seen[a.ChargeItemID] = true
		item, ok := byID[a.ChargeItemID]
		if !ok {
			return ErrItemNotInGroup
		}
		if item.Status != ItemOpen {
			return ErrItemNotOpen
		}
		outstanding := item.Amount.Sub(moneyOrZero(paid[item.ID]))
		if a.Amount.GreaterThan(outstanding) {
			return fmt.Errorf("%w: %s (sisa %s, dialokasikan %s)", ErrAllocationExceeds, item.Label, outstanding, a.Amount)
		}
		sum = sum.Add(a.Amount)
	}
	if !sum.Sub(amount).IsZero() {
		return fmt.Errorf("%w (alokasi %s, pembayaran %s)", ErrAllocationMismatch, sum, amount)
	}
	return nil
}

// findExistingPayment mengembalikan hasil idempoten bila kunci sudah dipakai.
func (s *Service) findExistingPayment(ctx context.Context, tenantID, groupID uint64, key string) (*ReceivePaymentResult, error) {
	if key == "" {
		return nil, nil
	}
	t, err := sale.FindChargeReceiptByIdempotencyKeyInTx(ctx, s.db, tenantID, key)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	if t.ChargeGroupID == nil {
		return nil, ErrTerminNotForGroup
	}
	if *t.ChargeGroupID != groupID {
		return nil, ErrIdempotencyConflict
	}
	res := &ReceivePaymentResult{TerminID: t.ID, AlreadyExisted: true}
	var rec struct {
		ID            uint64 `gorm:"column:id"`
		ReceiptNumber string `gorm:"column:receipt_number"`
	}
	_ = s.db.WithContext(ctx).Table("receipts").Select("id, receipt_number").
		Where("tenant_id = ? AND termin_payment_id = ?", tenantID, t.ID).
		Scan(&rec).Error
	res.ReceiptID, res.ReceiptNumber = rec.ID, rec.ReceiptNumber
	if sum, serr := s.GroupSummary(ctx, tenantID, *t.ChargeGroupID); serr == nil {
		res.Summary = *sum
	}
	return res, nil
}

// ReceivePayment mencatat pembayaran biaya realisasi/addon — SATU transaksi:
// jurnal (Dr Kas / Cr 2-2400) + termin (counts_toward_price=FALSE) + alokasi
// manual admin + kwitansi KWR. Outstanding harga rumah TIDAK tersentuh.
func (s *Service) ReceivePayment(ctx context.Context, tenantID uint64, req ReceivePaymentRequest) (*ReceivePaymentResult, error) {
	if req.Amount.IsZero() || req.Amount.IsNeg() || !req.Amount.IsWholeRupiah() {
		return nil, ErrAmountInvalid
	}
	if req.Date.IsZero() {
		req.Date = time.Now()
	}
	// Idempotency — kunci sama tidak pernah membuat termin/jurnal/kwitansi kedua.
	if existing, err := s.findExistingPayment(ctx, tenantID, req.GroupID, req.IdempotencyKey); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	desc := "Pembayaran biaya realisasi"
	if req.Reference != "" {
		desc += " ref " + req.Reference
	}
	if req.Notes != "" {
		desc += " — " + req.Notes
	}

	var (
		terminID          uint64
		receiptNumber     string
		receiptID         uint64
		contractID, group uint64
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		g, err := findGroupDB(ctx, tx, tenantID, req.GroupID, true)
		if err != nil {
			return err
		}
		if g.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		contractID, group = g.SaleContractID, g.ID
		if g.Kind == KindAddon {
			desc = "Pembayaran produk tambahan"
			if req.Reference != "" {
				desc += " ref " + req.Reference
			}
		}

		// Item dikunci → validasi alokasi manual admin (K-3) bebas race.
		items, err := listItemsDB(ctx, tx, tenantID, g.ID, true)
		if err != nil {
			return err
		}
		paid, err := paidByItemDB(ctx, tx, tenantID, g.ID)
		if err != nil {
			return err
		}
		if err := validateAllocations(req.Allocations, req.Amount, items, paid); err != nil {
			return err
		}

		// Jurnal: Dr Kas/Bank / Cr <piutang dan/atau titipan>.
		//
		// W-5 mengubah sisi kreditnya, bukan strukturnya. Untuk item yang sudah
		// di-invoice, uang yang masuk MELUNASI PIUTANG (Cr 1-2000) — kewajiban
		// titipannya sudah diakui saat invoice terbit, jadi mengkreditnya lagi
		// berarti mencatat kewajiban dua kali. Untuk item yang belum di-invoice,
		// perilakunya persis seperti sebelumnya: Cr akun titipan jenis biaya itu
		// (K-1 — kewajiban, bukan pendapatan).
		//
		// Σ kredit == debit kas, karena Σ alokasi == nominal (K-3, PERSIS) dan
		// setiap alokasi terpecah utuh menjadi porsi piutang + porsi titipan.
		bankID, err := cashBankAccountID(ctx, tx, tenantID, req.BankAccountCode)
		if err != nil {
			return err
		}
		posted, err := recognizedByItemDB(ctx, tx, tenantID, g.ID)
		if err != nil {
			return err
		}
		split := splitAgainstReceivable(req.Allocations, items, posted)
		u, err := findUnitRefDB(ctx, tx, tenantID, g.UnitID)
		if err != nil {
			return err
		}
		pid, uid := u.ProjectID, g.UnitID
		lines := []ledger.LineInput{
			{AccountID: bankID, Debit: req.Amount, ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid, Description: desc},
		}
		// Keterangan baris kredit mengikuti KIND, karena akunnya memang berbeda:
		// realisasi mengkredit titipan pihak ketiga, addon mengkredit Uang Muka
		// Penjualan. Satu kalimat untuk keduanya akan membuat salah satunya
		// berbohong di buku besar.
		depositDesc := "Titipan Realisasi (kewajiban — bukan pendapatan)"
		if g.Kind == KindAddon {
			depositDesc = "Uang Muka Penjualan produk tambahan (belum diakui pendapatan)"
		}
		creditLines, err := receivableCreditLines(ctx, tx, tenantID, split, u, g.UnitID, depositDesc)
		if err != nil {
			return err
		}
		// Draft dulu: KWR baru lahir sesudah termin, dan termin butuh id jurnal.
		journalID, err := draftJournalTx(ctx, tx, tenantID, req.Date, desc, req.CreatedBy, append(lines, creditLines...))
		if err != nil {
			return err
		}

		// Termin — kas masuk tetap SATU tabel; TIDAK pernah mengurangi harga.
		// W-4/TD-1: barisnya ditulis PEMILIK tabelnya (sale), charge hanya
		// menyatakan maksud. counts_toward_price=false ditegakkan di sana.
		terminID, err = sale.RecordChargeReceiptInTx(ctx, tx, sale.ChargeReceiptInput{
			TenantID:          tenantID,
			UnitID:            g.UnitID,
			ProjectID:         u.ProjectID,
			PhaseID:           u.PhaseID,
			ChargeGroupID:     g.ID,
			Amount:            req.Amount,
			BankAccountCode:   req.BankAccountCode,
			Date:              req.Date,
			Description:       desc,
			JournalEntryID:    journalID,
			CreditAccountCode: primaryCreditCode(split),
			CreatedBy:         req.CreatedBy,
			IdempotencyKey:    req.IdempotencyKey,
			Source:            sale.PaymentSourceRealization,
		})
		if err != nil {
			return err
		}

		// Sub-ledger alokasi (K-3 — keputusan admin, satu tabel yang sama).
		if err := sale.RecordChargeAllocationsInTx(ctx, tx, tenantID, terminID,
			chargeAllocInputs(req.Allocations), req.CreatedBy); err != nil {
			return err
		}

		// Audit pengakuan: porsi yang melunasi piutang sudah menjadi baris kredit
		// 1-2000 di jurnal yang sama, jadi yang tersisa hanyalah mencatatnya.
		if err := recordPaymentRecognitionInTx(ctx, tx, tenantID, g, items, split,
			ReasonRecPayment, journalID, req.CreatedBy); err != nil {
			return err
		}

		// Kwitansi KWR — atomik dengan pembayaran. INV-DOC-1: kwitansi INI yang
		// menjadi dokumen jurnal kasnya, jadi generator tidak boleh opsional di
		// jalur kas. Wiring yang tidak lengkap dulu berarti kas bergerak tanpa
		// bukti bernomor — persis lubang yang W-3 tutup.
		if s.receiptTx == nil {
			return fmt.Errorf("wiring tidak lengkap: penerimaan kas tanpa generator kwitansi")
		}
		var cb uint64
		if req.CreatedBy != nil {
			cb = *req.CreatedBy
		}
		num, id, rerr := s.receiptTx.GenerateReceiptInTx(ctx, tx, tenantID, cb,
			terminID, g.UnitID, req.Amount, req.BankAccountCode, req.Date, req.Notes)
		if rerr != nil {
			return fmt.Errorf("buat kwitansi realisasi: %w", rerr)
		}
		receiptNumber, receiptID = num, id

		// Tautkan kwitansi ke jurnal kasnya (arah dua-arah INV-DOC-1).
		if _, derr := document.LinkJournalBySource(tx.WithContext(ctx), tenantID, "receipts", receiptID, journalID); derr != nil {
			return fmt.Errorf("tautkan kwitansi realisasi ke jurnal: %w", derr)
		}
		// Buktinya ada — kas baru boleh bergerak (W-3.5).
		return postDraftTx(ctx, tx, tenantID, journalID)
	})
	if err != nil {
		return nil, err
	}

	sum, err := s.GroupSummary(ctx, tenantID, group)
	if err != nil {
		return nil, err
	}
	// Sinkron invoice REALISASI (best-effort — pola SettleShortfallIfPaid).
	if s.invoices != nil {
		s.invoices.SettleChargeInvoiceIfPaid(ctx, tenantID, contractID, group, sum.Outstanding)
	}
	return &ReceivePaymentResult{
		TerminID:      terminID,
		ReceiptNumber: receiptNumber,
		ReceiptID:     receiptID,
		Summary:       *sum,
	}, nil
}

// ── Payout vendor (K-1 keluar + K-5 outstanding tambahan otomatis) ───────────

func (s *Service) RecordPayout(ctx context.Context, tenantID uint64, req PayoutRequest) (*GroupSummary, error) {
	if req.Amount.IsZero() || req.Amount.IsNeg() || !req.Amount.IsWholeRupiah() {
		return nil, ErrAmountInvalid
	}
	if req.Vendor == "" {
		return nil, ErrVendorRequired
	}
	if req.Date.IsZero() {
		req.Date = time.Now()
	}
	var groupID uint64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Idempotency — kunci sama tidak pernah membuat payout/jurnal kedua.
		if req.IdempotencyKey != "" {
			var existing ChargePayout
			ferr := tx.WithContext(ctx).
				Where("tenant_id = ? AND idempotency_key = ?", tenantID, req.IdempotencyKey).
				First(&existing).Error
			if ferr == nil {
				if existing.ChargeItemID != req.ChargeItemID {
					return ErrIdempotencyConflict
				}
				groupID = existing.ChargeGroupID
				return nil
			} else if !errors.Is(ferr, gorm.ErrRecordNotFound) {
				return ferr
			}
		}
		var item ChargeItem
		if err := tx.WithContext(ctx).Clauses(gormLockingUpdate()).
			Where("id = ? AND tenant_id = ?", req.ChargeItemID, tenantID).
			First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrItemNotFound
			}
			return err
		}
		if item.Status != ItemOpen {
			return ErrItemNotOpen
		}
		g, err := findGroupDB(ctx, tx, tenantID, item.ChargeGroupID, true)
		if err != nil {
			return err
		}
		if g.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		groupID = g.ID

		bankID, err := cashBankAccountID(ctx, tx, tenantID, req.BankAccountCode)
		if err != nil {
			return err
		}
		// Payout selalu menunjuk SATU item, jadi akun kewajibannya tidak pernah
		// ambigu: akun snapshot item itu sendiri.
		depositID, err := depositAccountIDByCode(ctx, tx, tenantID, itemDepositCode(&item))
		if err != nil {
			return err
		}
		u, err := findUnitRefDB(ctx, tx, tenantID, g.UnitID)
		if err != nil {
			return err
		}
		pid, uid := u.ProjectID, g.UnitID
		desc := fmt.Sprintf("Payout %s — %s", item.Label, req.Vendor)
		// BTP — uang yang keluar adalah titipan milik customer yang diteruskan ke
		// pihak ketiga, bukan kas perusahaan (economic ownership, D-W3-6).
		journalID, err := postCashJournalTx(ctx, tx, tenantID, req.Date, desc, req.CreatedBy, []ledger.LineInput{
			{AccountID: depositID, Debit: req.Amount, ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid, Description: "Pelepasan titipan realisasi"},
			{AccountID: bankID, Credit: req.Amount, ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid, Description: desc},
		}, ledger.DocumentSpec{TypeCode: ledger.DocThirdPartyPayout})
		if err != nil {
			return err
		}
		payout := &ChargePayout{
			TenantID:        tenantID,
			ChargeGroupID:   g.ID,
			ChargeItemID:    item.ID,
			Amount:          req.Amount,
			Date:            req.Date,
			BankAccountCode: req.BankAccountCode,
			Vendor:          req.Vendor,
			Notes:           req.Notes,
			JournalEntryID:  journalID,
			IdempotencyKey:  idemPtr(req.IdempotencyKey),
			CreatedBy:       req.CreatedBy,
		}
		if err := tx.WithContext(ctx).Create(payout).Error; err != nil {
			return fmt.Errorf("simpan payout: %w", err)
		}

		// K-5: aktual > tagihan → tagihan item di-raise otomatis (ber-audit).
		var totalPayout struct {
			Total domain.Money `gorm:"column:total"`
		}
		if err := tx.WithContext(ctx).Table("charge_payouts").
			Select("COALESCE(SUM(amount), 0) AS total").
			Where("tenant_id = ? AND charge_item_id = ?", tenantID, item.ID).
			Scan(&totalPayout).Error; err != nil {
			return err
		}
		if totalPayout.Total.GreaterThan(item.Amount) {
			adj := &ChargeItemAdjustment{
				TenantID:       tenantID,
				ChargeItemID:   item.ID,
				OldAmount:      item.Amount,
				NewAmount:      totalPayout.Total,
				Reason:         ReasonPayoutOverrun,
				ChargePayoutID: &payout.ID,
				CreatedBy:      req.CreatedBy,
			}
			if err := tx.WithContext(ctx).Create(adj).Error; err != nil {
				return fmt.Errorf("simpan adjustment K-5: %w", err)
			}
			if err := tx.WithContext(ctx).Model(&ChargeItem{}).
				Where("id = ? AND tenant_id = ?", item.ID, tenantID).
				Update("amount", totalPayout.Total).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GroupSummary(ctx, tenantID, groupID)
}

// ── K-4: Refund / Transfer sisa titipan ──────────────────────────────────────

// residualGuard mengunci grup + menghitung residual di dalam tx.
func residualGuard(ctx context.Context, tx *gorm.DB, tenantID, groupID uint64, amount domain.Money) (*ChargeGroup, *GroupSummary, error) {
	g, err := findGroupDB(ctx, tx, tenantID, groupID, true)
	if err != nil {
		return nil, nil, err
	}
	if g.Status != GroupOpen {
		return nil, nil, ErrGroupNotOpen
	}
	sum, err := groupSummaryDB(ctx, tx, tenantID, g)
	if err != nil {
		return nil, nil, err
	}
	if amount.GreaterThan(sum.Residual) {
		return nil, nil, fmt.Errorf("%w (sisa %s, diminta %s)", ErrExceedsResidual, sum.Residual, amount)
	}
	return g, sum, nil
}

// Refund mengembalikan sisa titipan ke customer: Dr 2-2400 / Cr Kas.
func (s *Service) Refund(ctx context.Context, tenantID uint64, req RefundRequest) (*GroupSummary, error) {
	if req.Amount.IsZero() || req.Amount.IsNeg() || !req.Amount.IsWholeRupiah() {
		return nil, ErrAmountInvalid
	}
	if req.Date.IsZero() {
		req.Date = time.Now()
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Idempotency — kunci sama tidak pernah membuat refund/jurnal kedua.
		if req.IdempotencyKey != "" {
			var existing ChargeSettlement
			ferr := tx.WithContext(ctx).
				Where("tenant_id = ? AND idempotency_key = ?", tenantID, req.IdempotencyKey).
				First(&existing).Error
			if ferr == nil {
				if existing.Action != ActionRefund || existing.ChargeGroupID != req.GroupID {
					return ErrIdempotencyConflict
				}
				return nil
			} else if !errors.Is(ferr, gorm.ErrRecordNotFound) {
				return ferr
			}
		}
		g, sum, err := residualGuard(ctx, tx, tenantID, req.GroupID, req.Amount)
		if err != nil {
			return err
		}
		bankID, err := cashBankAccountID(ctx, tx, tenantID, req.BankAccountCode)
		if err != nil {
			return err
		}
		// Refund bergerak di level grup: pecah dulu ke akun-akun kewajiban yang
		// masih memegang dana (proporsional, largest-remainder — Σ == nominal).
		parts, err := splitByResidual(sum.DepositResiduals, req.Amount)
		if err != nil {
			return err
		}
		u, err := findUnitRefDB(ctx, tx, tenantID, g.UnitID)
		if err != nil {
			return err
		}
		pid, uid := u.ProjectID, g.UnitID
		desc := fmt.Sprintf("Refund sisa titipan realisasi grup #%d", g.ID)
		debitLines, err := depositLines(ctx, tx, tenantID, parts, true, u, g.UnitID,
			"Pelepasan titipan realisasi (refund)")
		if err != nil {
			return err
		}
		// RFC — dana kembali ke customer, bukan ke pihak ketiga (bedakan dari BTP).
		journalID, err := postCashJournalTx(ctx, tx, tenantID, req.Date, desc, req.CreatedBy, append(debitLines,
			ledger.LineInput{AccountID: bankID, Credit: req.Amount, ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid, Description: desc},
		), ledger.DocumentSpec{TypeCode: ledger.DocCustomerRefund})
		if err != nil {
			return err
		}
		bank := req.BankAccountCode
		row := &ChargeSettlement{
			TenantID:        tenantID,
			ChargeGroupID:   g.ID,
			Action:          ActionRefund,
			Amount:          req.Amount,
			Date:            req.Date,
			BankAccountCode: &bank,
			JournalEntryID:  &journalID,
			Notes:           req.Notes,
			IdempotencyKey:  idemPtr(req.IdempotencyKey),
			CreatedBy:       req.CreatedBy,
		}
		if err := tx.WithContext(ctx).Create(row).Error; err != nil {
			return err
		}
		return saveSettlementLines(ctx, tx, tenantID, row.ID, parts)
	})
	if err != nil {
		return nil, err
	}
	return s.GroupSummary(ctx, tenantID, req.GroupID)
}

// TransferToHouse mengalihkan sisa titipan menjadi pembayaran harga rumah.
// Dua fase (reservasi → pintu pembayaran sale) dengan idempotency turunan:
// pengulangan kunci yang sama melanjutkan reservasi, tidak menggandakan dana.
func (s *Service) TransferToHouse(ctx context.Context, tenantID uint64, req TransferHouseRequest) (*GroupSummary, error) {
	if s.houseTransfer == nil {
		return nil, fmt.Errorf("transfer ke harga rumah belum dikonfigurasi")
	}
	if req.Amount.IsZero() || req.Amount.IsNeg() || !req.Amount.IsWholeRupiah() {
		return nil, ErrAmountInvalid
	}
	if req.Date.IsZero() {
		req.Date = time.Now()
	}

	// Fase 1 — reservasi dana (mengurangi residual) ATAU lanjutkan reservasi
	// idempoten yang tertinggal (target_termin_id masih NULL).
	var settlement *ChargeSettlement
	var contractID uint64
	// Sumber dana bisa lebih dari satu akun kewajiban bila grup memuat jenis
	// biaya dengan akun berbeda. Pecahannya DISIMPAN di fase 1: menghitung ulang
	// saat resume akan meleset, karena residual sudah berubah oleh langkah yang
	// sudah terlanjur jalan.
	var parts []DepositAmount
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if req.IdempotencyKey != "" {
			var existing ChargeSettlement
			ferr := tx.WithContext(ctx).Clauses(gormLockingUpdate()).
				Where("tenant_id = ? AND idempotency_key = ?", tenantID, req.IdempotencyKey).
				First(&existing).Error
			if ferr == nil {
				if existing.Action != ActionTransferHouse || existing.ChargeGroupID != req.GroupID {
					return ErrIdempotencyConflict
				}
				if existing.TargetTerminID != nil { // sudah tuntas
					settlement = &existing
					return nil
				}
				settlement = &existing // resume fase 2
				g, gerr := findGroupDB(ctx, tx, tenantID, existing.ChargeGroupID, false)
				if gerr != nil {
					return gerr
				}
				contractID = g.SaleContractID
				parts, gerr = settlementLinesDB(ctx, tx, tenantID, existing.ID)
				return gerr
			} else if !errors.Is(ferr, gorm.ErrRecordNotFound) {
				return ferr
			}
		}
		g, sum, err := residualGuard(ctx, tx, tenantID, req.GroupID, req.Amount)
		if err != nil {
			return err
		}
		contractID = g.SaleContractID
		if parts, err = splitByResidual(sum.DepositResiduals, req.Amount); err != nil {
			return err
		}
		memo, err := s.nextMemo(tx, tenantID, req.Date)
		if err != nil {
			return err
		}
		row := &ChargeSettlement{
			TenantID:       tenantID,
			ChargeGroupID:  g.ID,
			Action:         ActionTransferHouse,
			Amount:         req.Amount,
			Date:           req.Date,
			MemoNumber:     &memo.Number,
			Notes:          req.Notes,
			IdempotencyKey: idemPtr(req.IdempotencyKey),
			CreatedBy:      req.CreatedBy,
		}
		if err := tx.WithContext(ctx).Create(row).Error; err != nil {
			return fmt.Errorf("simpan reservasi transfer: %w", err)
		}
		if err := registerMemo(tx, tenantID, memo, row.ID, req.Amount, req.CreatedBy); err != nil {
			return err
		}
		if err := saveSettlementLines(ctx, tx, tenantID, row.ID, parts); err != nil {
			return err
		}
		settlement = row
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Fase 2 — pintu pembayaran yang ADA (jurnal Dr akun titipan / Cr routing
	// GAP-1, counts_toward_price=TRUE, alokasi waterfall, sinkron invoice).
	// Satu panggilan per akun sumber: masing-masing adalah pembayaran biasa
	// dengan sumber dana yang jelas, dan kuncinya idempoten per akun sehingga
	// pengulangan melanjutkan, tidak menggandakan.
	if settlement.TargetTerminID == nil {
		var firstTermin uint64
		for _, p := range parts {
			res, herr := s.houseTransfer.ApplyDepositTransfer(ctx, tenantID, sale.DepositTransferRequest{
				ContractID:         contractID,
				Amount:             p.Amount,
				Date:               req.Date,
				DepositAccountCode: p.AccountCode,
				Notes:              req.Notes,
				CreatedBy:          req.CreatedBy,
				IdempotencyKey:     fmt.Sprintf("cg-transfer-%d-%s", settlement.ID, p.AccountCode),
			})
			if herr != nil {
				if firstTermin == 0 {
					// Belum ada rupiah yang berpindah → lepaskan reservasi utuh.
					_ = s.db.WithContext(ctx).
						Where("tenant_id = ? AND charge_settlement_id = ?", tenantID, settlement.ID).
						Delete(&ChargeSettlementLine{}).Error
					_ = s.db.WithContext(ctx).
						Where("id = ? AND tenant_id = ? AND target_termin_id IS NULL", settlement.ID, tenantID).
						Delete(&ChargeSettlement{}).Error
					return nil, herr
				}
				// Sebagian sudah berpindah: reservasi TIDAK dilepas (uangnya nyata).
				// Ulangi dengan kunci idempotensi yang sama untuk menuntaskan sisanya.
				return nil, fmt.Errorf("transfer sebagian gagal pada akun %s (ulangi dengan kunci yang sama): %w", p.AccountCode, herr)
			}
			if firstTermin == 0 {
				firstTermin = res.TerminID
			}
		}
		_ = s.db.WithContext(ctx).Model(&ChargeSettlement{}).
			Where("id = ? AND tenant_id = ?", settlement.ID, tenantID).
			Update("target_termin_id", firstTermin).Error
	}
	return s.GroupSummary(ctx, tenantID, settlement.ChargeGroupID)
}

// TransferToGroup mengalihkan sisa titipan ke grup tagihan lain — SATU
// transaksi (kedua sisi milik charge): jurnal memo Dr/Cr 2-2400, termin
// non-kas di grup tujuan, alokasi manual admin (K-3), settlement di sumber.
func (s *Service) TransferToGroup(ctx context.Context, tenantID uint64, req TransferGroupRequest) (*GroupSummary, error) {
	if req.Amount.IsZero() || req.Amount.IsNeg() || !req.Amount.IsWholeRupiah() {
		return nil, ErrAmountInvalid
	}
	if req.GroupID == req.TargetGroupID {
		return nil, ErrTargetGroupSame
	}
	if req.Date.IsZero() {
		req.Date = time.Now()
	}
	// Idempotency (settlement-level).
	if req.IdempotencyKey != "" {
		var existing ChargeSettlement
		ferr := s.db.WithContext(ctx).
			Where("tenant_id = ? AND idempotency_key = ?", tenantID, req.IdempotencyKey).
			First(&existing).Error
		if ferr == nil {
			if existing.Action != ActionTransferGroup || existing.ChargeGroupID != req.GroupID {
				return nil, ErrIdempotencyConflict
			}
			return s.GroupSummary(ctx, tenantID, existing.ChargeGroupID)
		} else if !errors.Is(ferr, gorm.ErrRecordNotFound) {
			return nil, ferr
		}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		src, srcSum, err := residualGuard(ctx, tx, tenantID, req.GroupID, req.Amount)
		if err != nil {
			return err
		}
		target, err := findGroupDB(ctx, tx, tenantID, req.TargetGroupID, true)
		if err != nil {
			return err
		}
		if target.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		items, err := listItemsDB(ctx, tx, tenantID, target.ID, true)
		if err != nil {
			return err
		}
		paid, err := paidByItemDB(ctx, tx, tenantID, target.ID)
		if err != nil {
			return err
		}
		if err := validateAllocations(req.Allocations, req.Amount, items, paid); err != nil {
			return err
		}

		// Sisi SUMBER dipecah dari residual per akun; sisi TUJUAN dari alokasi
		// admin ke item tujuan. Keduanya independen — akun sumber dan akun tujuan
		// boleh sama sekali berbeda, dan jurnalnya tetap balanced karena kedua
		// sisi berjumlah sama persis dengan nominal transfer.
		srcParts, err := splitByResidual(srcSum.DepositResiduals, req.Amount)
		if err != nil {
			return err
		}
		// Sisi tujuan mengikuti aturan yang sama dengan pembayaran kas: item yang
		// sudah di-invoice dilunasi piutangnya (Cr 1-2000), item yang belum
		// menambah titipan. Dana yang dipindahkan tidak berubah sifatnya hanya
		// karena asalnya bukan kas.
		tgtPosted, err := recognizedByItemDB(ctx, tx, tenantID, target.ID)
		if err != nil {
			return err
		}
		tgtSplit := splitAgainstReceivable(req.Allocations, items, tgtPosted)
		uSrc, err := findUnitRefDB(ctx, tx, tenantID, src.UnitID)
		if err != nil {
			return err
		}
		uTgt, err := findUnitRefDB(ctx, tx, tenantID, target.UnitID)
		if err != nil {
			return err
		}
		desc := fmt.Sprintf("Transfer titipan realisasi grup #%d → grup #%d", src.ID, target.ID)
		// Memo balanced — jejak audit perpindahan antar grup.
		drLines, err := depositLines(ctx, tx, tenantID, srcParts, true, uSrc, src.UnitID, "Pelepasan titipan grup sumber")
		if err != nil {
			return err
		}
		crLines, err := receivableCreditLines(ctx, tx, tenantID, tgtSplit, uTgt, target.UnitID, "Penerimaan titipan grup tujuan")
		if err != nil {
			return err
		}
		journalID, err := postJournalTx(ctx, tx, tenantID, req.Date, desc, req.CreatedBy, append(drLines, crLines...))
		if err != nil {
			return err
		}

		// Termin non-kas di grup TUJUAN (sumber dana = akun titipan).
		transferTerminID, err := sale.RecordChargeReceiptInTx(ctx, tx, sale.ChargeReceiptInput{
			TenantID:          tenantID,
			UnitID:            target.UnitID,
			ProjectID:         uTgt.ProjectID,
			PhaseID:           uTgt.PhaseID,
			ChargeGroupID:     target.ID,
			Amount:            req.Amount,
			BankAccountCode:   primaryDepositCode(srcParts),
			Date:              req.Date,
			Description:       desc,
			JournalEntryID:    journalID,
			CreditAccountCode: primaryCreditCode(tgtSplit),
			CreatedBy:         req.CreatedBy,
			Source:            sale.PaymentSourceRealizationTransfer,
		})
		if err != nil {
			return err
		}
		if err := sale.RecordChargeAllocationsInTx(ctx, tx, tenantID, transferTerminID,
			chargeAllocInputs(req.Allocations), req.CreatedBy); err != nil {
			return err
		}
		if err := recordPaymentRecognitionInTx(ctx, tx, tenantID, target, items, tgtSplit,
			ReasonRecTransferIn, journalID, req.CreatedBy); err != nil {
			return err
		}
		memo, err := s.nextMemo(tx, tenantID, req.Date)
		if err != nil {
			return err
		}
		tgt := target.ID
		row := &ChargeSettlement{
			TenantID:       tenantID,
			ChargeGroupID:  src.ID,
			Action:         ActionTransferGroup,
			Amount:         req.Amount,
			Date:           req.Date,
			MemoNumber:     &memo.Number,
			TargetGroupID:  &tgt,
			TargetTerminID: &transferTerminID,
			JournalEntryID: &journalID,
			Notes:          req.Notes,
			IdempotencyKey: idemPtr(req.IdempotencyKey),
			CreatedBy:      req.CreatedBy,
		}
		if err := tx.WithContext(ctx).Create(row).Error; err != nil {
			return err
		}
		if err := registerMemo(tx, tenantID, memo, row.ID, req.Amount, req.CreatedBy); err != nil {
			return err
		}
		// Rincian yang dicatat adalah sisi SUMBER: inilah yang mengurangi dana
		// grup ini. Sisi tujuan sudah tercatat sebagai alokasi ke item tujuan.
		return saveSettlementLines(ctx, tx, tenantID, row.ID, srcParts)
	})
	if err != nil {
		return nil, err
	}
	return s.GroupSummary(ctx, tenantID, req.GroupID)
}

// ── Void pembayaran (koreksi append-only, Invariant #5) ──────────────────────

// VoidPayment membatalkan SATU pembayaran realisasi: jurnal PEMBALIK (Dr
// 2-2400 / Cr Kas) + baris alokasi mirror NEGATIF (charge_item_void) +
// settlement void_payment. Termin & jurnal asli TIDAK diubah/dihapus.
func (s *Service) VoidPayment(ctx context.Context, tenantID uint64, req VoidPaymentRequest) (*GroupSummary, error) {
	var groupID uint64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := sale.FindChargeReceiptForUpdateInTx(ctx, tx, tenantID, req.TerminID)
		if err != nil {
			if errors.Is(err, sale.ErrNotChargeReceipt) {
				return ErrTerminNotForGroup
			}
			return err
		}
		g, err := findGroupDB(ctx, tx, tenantID, *t.ChargeGroupID, true)
		if err != nil {
			return err
		}
		if g.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		groupID = g.ID

		// Void ganda mustahil (UNIQUE uk_cs_void) — cek eksplisit utk error ramah.
		var n int64
		if err := tx.WithContext(ctx).Table("charge_settlements").
			Where("tenant_id = ? AND voided_termin_id = ?", tenantID, t.ID).
			Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrAlreadyVoided
		}

		// Dana pembayaran ini harus masih dipegang (belum terpakai payout/
		// refund/transfer): residual − amount ≥ 0.
		sum, err := groupSummaryDB(ctx, tx, tenantID, g)
		if err != nil {
			return err
		}
		if t.Amount.GreaterThan(sum.Residual) {
			return ErrVoidBreaksFunds
		}

		// Alokasi asli pembayaran ini adalah sumber kebenaran akun mana saja yang
		// dulu dikredit: setiap alokasi menunjuk item, dan item membawa SNAPSHOT
		// akunnya. Pembalik karena itu selalu memukul akun yang benar-benar
		// dikredit dulu — walau master jenis biaya sudah dipindah sejak itu, dan
		// walau pembayaran itu menyentuh beberapa akun sekaligus.
		allocs, err := sale.ListChargeAllocationsInTx(ctx, tx, tenantID, t.ID)
		if err != nil {
			return err
		}
		items, err := listItemsDB(ctx, tx, tenantID, g.ID, true)
		if err != nil {
			return err
		}
		reversal := make([]AllocationInput, 0, len(allocs))
		for _, a := range allocs {
			if a.ChargeItemID == nil {
				continue
			}
			reversal = append(reversal, AllocationInput{ChargeItemID: *a.ChargeItemID, Amount: a.Amount})
		}
		// Sebagian pembayaran ini mungkin dulu mengkredit PIUTANG, bukan titipan
		// (W-5). Yang dibalik harus persis yang dulu dikredit — jejaknya ada di
		// baris pengakuan yang menempel pada jurnal kas aslinya, bukan pada
		// keadaan grup sekarang (keadaan itu sudah berubah sejak saat itu).
		recs, err := recognitionsByJournalDB(ctx, tx, tenantID, t.JournalEntryID)
		if err != nil {
			return err
		}
		arByItem := map[uint64]domain.Money{}
		arTotal := domain.Zero
		for _, r := range recs {
			if !r.Delta.IsNeg() {
				continue
			}
			amt := domain.Zero.Sub(r.Delta)
			arByItem[r.ChargeItemID] = moneyOrZero(arByItem[r.ChargeItemID]).Add(amt)
			arTotal = arTotal.Add(amt)
		}
		depReversal := make([]AllocationInput, 0, len(reversal))
		for _, a := range reversal {
			if rest := a.Amount.Sub(moneyOrZero(arByItem[a.ChargeItemID])); rest.GreaterThan(domain.Zero) {
				depReversal = append(depReversal, AllocationInput{ChargeItemID: a.ChargeItemID, Amount: rest})
			}
		}
		parts := depositSplitOfAllocations(depReversal, items)
		if len(parts) == 0 && arTotal.IsZero() {
			// Pembayaran tanpa alokasi mustahil lewat jalur normal (K-3 wajib);
			// baris warisan dibalik ke akun yang tercatat pada terminnya.
			parts = []DepositAmount{{AccountCode: t.CreditAccountCode, Amount: t.Amount}}
		}

		bankID, err := cashBankAccountID(ctx, tx, tenantID, t.BankAccountCode)
		if err != nil {
			return err
		}
		u, err := findUnitRefDB(ctx, tx, tenantID, g.UnitID)
		if err != nil {
			return err
		}
		pid, uid := u.ProjectID, g.UnitID
		desc := fmt.Sprintf("Pembalik pembayaran realisasi #%d — %s", t.ID, req.Reason)
		// Pembayaran yang dulu seluruhnya melunasi piutang tidak punya sisi
		// titipan untuk dibalik — pembaliknya murni 1-2000.
		var drLines []ledger.LineInput
		if len(parts) > 0 {
			drLines, err = depositLines(ctx, tx, tenantID, parts, true, u, g.UnitID, desc)
			if err != nil {
				return err
			}
		}
		if arTotal.GreaterThan(domain.Zero) {
			arID, err := accountIDByCode(ctx, tx, tenantID, AccountCodePiutangCustomer)
			if err != nil {
				return err
			}
			drLines = append(drLines, ledger.LineInput{AccountID: arID, Debit: arTotal,
				ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid,
				Description: "Piutang biaya realisasi hidup kembali (void)"})
		}
		// JR — pembalik mendapat nomor sendiri (append-only) yang menunjuk balik
		// ke kwitansi KWR yang dibatalkan. Dokumen asli boleh belum ada untuk
		// pembayaran pra-W-3 yang belum di-backfill; pembaliknya tetap bernomor.
		origDoc, err := document.FindByJournal(tx.WithContext(ctx), tenantID, t.JournalEntryID)
		if err != nil {
			return err
		}
		var reverses uint64
		if origDoc != nil {
			reverses = origDoc.ID
		}
		journalID, err := postCashJournalTx(ctx, tx, tenantID, time.Now(), desc, req.CreatedBy, append(drLines,
			ledger.LineInput{AccountID: bankID, Credit: t.Amount, ProjectID: &pid, PhaseID: u.PhaseID, UnitID: &uid, Description: "Pengembalian kas (void)"},
		), ledger.DocumentSpec{TypeCode: ledger.DocJournalReversal, ReversesDocumentID: reverses})
		if err != nil {
			return err
		}

		// Mirror alokasi NEGATIF — histori alokasi asli tetap utuh (Invariant #5).
		if err := sale.RecordChargeVoidMirrorsInTx(ctx, tx, tenantID, allocs, req.CreatedBy); err != nil {
			return err
		}
		// Piutang yang dulu dilunasi pembayaran ini hidup kembali — dicatat pada
		// jurnal PEMBALIK, bukan jurnal asli (append-only).
		for _, it := range items {
			amt, ok := arByItem[it.ID]
			if !ok || amt.IsZero() {
				continue
			}
			jid := journalID
			if err := recordRecognitionInTx(ctx, tx, tenantID, g, it, amt,
				ReasonRecVoidPayment, &jid, nil, req.CreatedBy); err != nil {
				return err
			}
		}
		// Bila item baru di-invoice SETELAH pembayaran ini, membalik apa yang dulu
		// dikredit saja belum cukup: piutangnya kini harus utuh kembali. Selisihnya
		// diposting sebagai jurnal penyesuaian tersendiri.
		if _, err := s.syncRecognitionInTx(ctx, tx, tenantID, g, recognitionOpts{
			Reason:    ReasonRecVoidPayment,
			Date:      time.Now(),
			Desc:      fmt.Sprintf("Penyesuaian piutang realisasi setelah void pembayaran #%d", t.ID),
			CreatedBy: req.CreatedBy,
		}); err != nil {
			return err
		}

		tid := t.ID
		row := &ChargeSettlement{
			TenantID:       tenantID,
			ChargeGroupID:  g.ID,
			Action:         ActionVoidPayment,
			Amount:         t.Amount,
			Date:           time.Now(),
			VoidedTerminID: &tid,
			JournalEntryID: &journalID,
			Notes:          req.Reason,
			CreatedBy:      req.CreatedBy,
		}
		if err := tx.WithContext(ctx).Create(row).Error; err != nil {
			return err
		}
		// Rincian per akun ikut dicatat sebagai audit jurnal pembaliknya. Baris
		// void TIDAK ikut mengurangi dana grup (returnedActions) — pembatalannya
		// sudah ternetralkan lewat mirror alokasi negatif di atas.
		return saveSettlementLines(ctx, tx, tenantID, row.ID, parts)
	})
	if err != nil {
		return nil, err
	}
	return s.GroupSummary(ctx, tenantID, groupID)
}

// ── Settlement (K-4 true-up + tutup grup) ────────────────────────────────────

// TrueUpAndSettle men-true-up tagihan tiap item ke aktual:
//
//	amount_baru = max(Σ payout item, Σ alokasi item)
//
// (aktual < tagihan → turun; dana yang sudah dibayar customer tidak pernah
// hilang — kelebihannya muncul sebagai residual → refund/transfer). Grup
// menjadi 'settled' hanya bila outstanding == 0 DAN residual == 0.
func (s *Service) TrueUpAndSettle(ctx context.Context, tenantID, groupID uint64, createdBy *uint64) (*SettleResult, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		g, err := findGroupDB(ctx, tx, tenantID, groupID, true)
		if err != nil {
			return err
		}
		if g.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		items, err := listItemsDB(ctx, tx, tenantID, g.ID, true)
		if err != nil {
			return err
		}
		paidMap, err := paidByItemDB(ctx, tx, tenantID, g.ID)
		if err != nil {
			return err
		}
		payoutMap, err := payoutByItemDB(ctx, tx, tenantID, g.ID)
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.Status != ItemOpen {
				continue
			}
			paid := moneyOrZero(paidMap[it.ID])
			payout := moneyOrZero(payoutMap[it.ID])
			target := payout
			if paid.GreaterThan(target) {
				target = paid
			}
			if target.Sub(it.Amount).IsZero() {
				continue // sudah pas
			}
			adj := &ChargeItemAdjustment{
				TenantID:     tenantID,
				ChargeItemID: it.ID,
				OldAmount:    it.Amount,
				NewAmount:    target,
				Reason:       ReasonTrueupSettlement,
				CreatedBy:    createdBy,
			}
			if err := tx.WithContext(ctx).Create(adj).Error; err != nil {
				return fmt.Errorf("simpan adjustment true-up: %w", err)
			}
			if err := tx.WithContext(ctx).Model(&ChargeItem{}).
				Where("id = ? AND tenant_id = ?", it.ID, tenantID).
				Update("amount", target).Error; err != nil {
				return err
			}
		}
		// True-up boleh menurunkan tagihan di bawah nilai yang sudah di-invoice.
		// Klaim ikut turun — piutang atas biaya yang ternyata tidak terjadi adalah
		// piutang fiktif.
		if _, err := s.syncRecognitionInTx(ctx, tx, tenantID, g, recognitionOpts{
			Reason:    ReasonRecTrueUp,
			Date:      time.Now(),
			Desc:      fmt.Sprintf("Penyesuaian piutang realisasi — true-up grup #%d", g.ID),
			CreatedBy: createdBy,
		}); err != nil {
			return err
		}
		// Settle bila tuntas: tidak ada tagihan tersisa & tidak ada dana tersisa.
		sum, err := groupSummaryDB(ctx, tx, tenantID, g)
		if err != nil {
			return err
		}
		if sum.Outstanding.IsZero() && sum.Residual.IsZero() {
			return tx.WithContext(ctx).Model(&ChargeGroup{}).
				Where("id = ? AND tenant_id = ?", g.ID, tenantID).
				Update("status", string(GroupSettled)).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sum, err := s.GroupSummary(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	return &SettleResult{Settled: sum.Status == GroupSettled, Summary: *sum}, nil
}

// ── Invoice REALISASI ────────────────────────────────────────────────────────

// IssueInvoice menerbitkan invoice REALISASI sebesar outstanding KANONIK grup
// DAN mengakui piutangnya (W-5 / R-5) — satu transaksi.
//
// Nominal invoice dan nominal jurnal pengakuan bisa berbeda, dan itu benar:
// invoice menyatakan seluruh sisa tagihan, sementara jurnalnya hanya memposting
// DELTA-nya. Selisihnya adalah bagian yang sudah menjadi piutang lewat invoice
// sebelumnya — mempostingnya lagi berarti menagih dua kali di buku besar.
// Nilai ketiga yang dikembalikan adalah DELTA pengakuan itu — nominal yang baru
// saja menjadi piutang. Layar menagihkannya kembali ke admin ("Rp12.000.000
// diakui sebagai Piutang Customer"), dan itu harus angka yang benar-benar
// diposting, bukan hasil hitung ulang di browser.
func (s *Service) IssueInvoice(ctx context.Context, tenantID, groupID, createdBy uint64, dueDate time.Time, notes string) (uint64, string, domain.Money, error) {
	if s.invoices == nil {
		return 0, "", domain.Zero, fmt.Errorf("invoice realisasi belum dikonfigurasi")
	}
	var (
		invoiceID  uint64
		invoiceNo  string
		recognized = domain.Zero
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		g, err := findGroupDB(ctx, tx, tenantID, groupID, true)
		if err != nil {
			return err
		}
		if g.Status != GroupOpen {
			return ErrGroupNotOpen
		}
		sum, err := groupSummaryDB(ctx, tx, tenantID, g)
		if err != nil {
			return err
		}
		id, no, due, err := s.invoices.IssueChargeInvoiceInTx(ctx, tx, tenantID,
			g.SaleContractID, g.ID, createdBy, sum.Outstanding, dueDate, notes)
		if err != nil {
			return err
		}
		invoiceID, invoiceNo = id, no
		recognized, err = s.recognizeGroupInTx(ctx, tx, tenantID, g, id, no, due, ReasonRecInvoice, actorPtr(createdBy))
		return err
	})
	if err != nil {
		return 0, "", domain.Zero, err
	}
	return invoiceID, invoiceNo, recognized, nil
}

// recognizeGroupInTx menerbitkan jurnal pengakuan untuk sebuah invoice yang baru
// terbit. Dipakai jalur manual (IssueInvoice) maupun otomatis (BAST) — satu
// jalan, supaya tidak ada dua definisi "kapan piutang lahir".
func (s *Service) recognizeGroupInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, g *ChargeGroup,
	invoiceID uint64, invoiceNo string, due time.Time, reason RecognitionReason, createdBy *uint64) (domain.Money, error) {

	dueDate := due
	return s.syncRecognitionInTx(ctx, tx, tenantID, g, recognitionOpts{
		Reason:    reason,
		Date:      time.Now(),
		Desc:      fmt.Sprintf("Piutang biaya realisasi — invoice %s (grup #%d)", invoiceNo, g.ID),
		InvoiceID: &invoiceID,
		DueDate:   &dueDate,
		CreatedBy: createdBy,
		Recognize: true,
	})
}

// RecognizeOnBASTInTx menerbitkan invoice realisasi yang belum terbit untuk
// sebuah unit, DI DALAM transaksi BAST — inilah yang memenuhi keputusan D-3:
// pada saat BAST, sisa biaya realisasi sudah menjadi Piutang Customer.
//
// Ini BUKAN gate. BAST tidak pernah ditolak karena realisasi belum lunas; yang
// dilakukan di sini adalah MENERBITKAN TAGIHAN, bukan menuntut pembayaran.
// Kegagalannya tetap menggagalkan transaksi karena kegagalan teknis di sini
// berarti piutang hilang tanpa jejak — bukan karena ada syarat bisnis baru.
//
// Grup yang invoice-nya masih hidup dilewati: INV-CG-1 hanya mengizinkan satu
// invoice realisasi hidup per grup, dan piutangnya memang sudah diakui lewat
// invoice itu.
func (s *Service) RecognizeOnBASTInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID uint64, createdBy *uint64) error {
	if s.invoices == nil {
		return fmt.Errorf("wiring tidak lengkap: pengakuan piutang realisasi tanpa penerbit invoice")
	}
	var groups []*ChargeGroup
	if err := tx.WithContext(ctx).Clauses(gormLockingUpdate()).
		Where("tenant_id = ? AND unit_id = ? AND kind = ? AND status = ?",
			tenantID, unitID, string(KindRealization), string(GroupOpen)).
		Order("id ASC").Find(&groups).Error; err != nil {
		return fmt.Errorf("muat grup realisasi unit: %w", err)
	}
	var actor uint64
	if createdBy != nil {
		actor = *createdBy
	}
	for _, g := range groups {
		sum, err := groupSummaryDB(ctx, tx, tenantID, g)
		if err != nil {
			return err
		}
		if !sum.Outstanding.GreaterThan(domain.Zero) {
			continue
		}
		liveID, _, _, err := s.invoices.FindLiveChargeInvoiceInTx(ctx, tx, tenantID, g.SaleContractID, g.ID)
		if err != nil {
			return err
		}
		if liveID != 0 {
			continue
		}
		id, no, due, err := s.invoices.IssueChargeInvoiceInTx(ctx, tx, tenantID,
			g.SaleContractID, g.ID, actor, sum.Outstanding, time.Time{},
			"Diterbitkan otomatis saat BAST — sisa biaya realisasi menjadi Piutang Customer (D-3)")
		if err != nil {
			return err
		}
		if _, err := s.recognizeGroupInTx(ctx, tx, tenantID, g, id, no, due, ReasonRecBAST, createdBy); err != nil {
			return err
		}
	}
	// W-13: produk tambahan diakui PENDAPATANNYA pada momen yang sama. Bukan
	// titipan — jurnalnya berbeda arah, jadi jalurnya pun terpisah (addon.go).
	return s.recognizeAddonRevenueInTx(ctx, tx, tenantID, unitID, createdBy)
}

func actorPtr(id uint64) *uint64 {
	if id == 0 {
		return nil
	}
	return &id
}

func idemPtr(key string) *string {
	if key == "" {
		return nil
	}
	return &key
}

// chargeAllocInputs menerjemahkan instruksi alokasi charge menjadi bentuk yang
// dimengerti port sub-ledger milik `sale` (W-4/TD-1).
func chargeAllocInputs(in []AllocationInput) []sale.ChargeAllocationInput {
	out := make([]sale.ChargeAllocationInput, 0, len(in))
	for _, a := range in {
		out = append(out, sale.ChargeAllocationInput{ChargeItemID: a.ChargeItemID, Amount: a.Amount})
	}
	return out
}
