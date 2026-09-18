package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
)

// ── TerminLoader ──────────────────────────────────────────────────────────────

// LoadTerminInfo memuat satu transaksi termin (tenant-scoped) dari tabel
// termin_payments. Tidak import package sale (hindari circular import).
func (r *GORMRepository) LoadTerminInfo(ctx context.Context, tenantID, terminID uint64) (*TerminInfo, error) {
	type row struct {
		ID              uint64       `gorm:"column:id"`
		UnitID          uint64       `gorm:"column:unit_id"`
		Amount          domain.Money `gorm:"column:amount"`
		BankAccountCode string       `gorm:"column:bank_account_code"`
		Date            time.Time    `gorm:"column:date"`
		Description     string       `gorm:"column:description"`
	}
	var res row
	err := r.db.WithContext(ctx).
		Table("termin_payments").
		Select("id, unit_id, amount, bank_account_code, date, description").
		Where("id = ? AND tenant_id = ?", terminID, tenantID).
		First(&res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTerminNotFound
		}
		return nil, fmt.Errorf("LoadTerminInfo: %w", err)
	}
	return &TerminInfo{
		ID:              res.ID,
		UnitID:          res.UnitID,
		Amount:          res.Amount,
		BankAccountCode: res.BankAccountCode,
		Date:            res.Date,
		Description:     res.Description,
	}, nil
}

// findContractIDByUnit mencari kontrak penjualan aktif untuk sebuah unit.
// Mengembalikan nil bila tidak ada (receipt tetap bisa dibuat tanpa kontrak).
func (r *GORMRepository) findContractIDByUnit(ctx context.Context, tenantID, unitID uint64) *uint64 {
	return findContractIDByUnitDB(ctx, r.db, tenantID, unitID)
}

func findContractIDByUnitDB(ctx context.Context, db *gorm.DB, tenantID, unitID uint64) *uint64 {
	var id uint64
	err := db.WithContext(ctx).
		Table("sale_contracts").
		Select("id").
		Where("unit_id = ? AND tenant_id = ?", unitID, tenantID).
		Order("id DESC").
		Limit(1).
		Scan(&id).Error
	if err != nil || id == 0 {
		return nil
	}
	return &id
}

// ── ReceiptStore ──────────────────────────────────────────────────────────────

// FindReceiptByTermin mengembalikan receipt yang sudah ada untuk sebuah termin.
func (r *GORMRepository) FindReceiptByTermin(ctx context.Context, tenantID, terminID uint64) (*Receipt, error) {
	return findReceiptByTerminDB(ctx, r.db, tenantID, terminID)
}

func findReceiptByTerminDB(ctx context.Context, db *gorm.DB, tenantID, terminID uint64) (*Receipt, error) {
	var rec Receipt
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND termin_payment_id = ?", tenantID, terminID).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReceiptNotFound
		}
		return nil, fmt.Errorf("FindReceiptByTermin: %w", err)
	}
	return &rec, nil
}

func (r *GORMRepository) FindReceiptByID(ctx context.Context, tenantID, id uint64) (*Receipt, error) {
	var rec Receipt
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReceiptNotFound
		}
		return nil, fmt.Errorf("FindReceiptByID: %w", err)
	}
	return &rec, nil
}

// CreateReceiptForTermin membuat receipt untuk sebuah termin secara IDEMPOTEN.
// Jika receipt sudah ada (UNIQUE tenant_id+termin_payment_id), kembalikan yang
// ada — tidak membuat duplikat. Nomor receipt di-generate atomik dalam transaksi
// yang dibuka sendiri (pemakaian standalone, mis. endpoint /termins/{id}/receipt).
func (r *GORMRepository) CreateReceiptForTermin(ctx context.Context, tenantID, createdBy uint64, info *TerminInfo, notes string) (*Receipt, error) {
	var rec *Receipt
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created, cerr := createReceiptForTerminInTx(ctx, tx, tenantID, createdBy, info, notes)
		if cerr != nil {
			return cerr
		}
		rec = created
		return nil
	})
	if err != nil {
		// Race: receipt dibuat bersamaan oleh request lain → ambil yang ada.
		if existing, ferr := r.FindReceiptByTermin(ctx, tenantID, info.ID); ferr == nil {
			return existing, nil
		}
		return nil, fmt.Errorf("CreateReceiptForTermin: %w", err)
	}
	return rec, nil
}

// CreateReceiptForTerminInTx membuat receipt MEMAKAI transaksi yang diberikan —
// agar kwitansi ikut atomik dengan penerimaan pembayaran (FE-2, guard #2).
// Tidak membuka transaksi baru; kegagalan meng-rollback transaksi pemanggil.
func (r *GORMRepository) CreateReceiptForTerminInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy uint64, info *TerminInfo, notes string) (*Receipt, error) {
	return createReceiptForTerminInTx(ctx, tx, tenantID, createdBy, info, notes)
}

// createReceiptForTerminInTx adalah inti pembuatan receipt idempoten memakai
// handle DB (tx) yang diberikan.
func createReceiptForTerminInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy uint64, info *TerminInfo, notes string) (*Receipt, error) {
	// Fast path: sudah ada (dalam scope tx).
	if existing, err := findReceiptByTerminDB(ctx, tx, tenantID, info.ID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrReceiptNotFound) {
		return nil, err
	}

	contractID := findContractIDByUnitDB(ctx, tx, tenantID, info.UnitID)

	// UAT Batch 2 §1: tipe kwitansi DERIVED dari sumber termin (SoT tunggal —
	// payment_source), bukan parameter terpisah yang bisa menyimpang.
	// booking_fee → KWB; realization (Billing Batch 2) → KWR; lainnya → KWT.
	rtype := ReceiptTypeHousePayment
	var src struct {
		PaymentSource string  `gorm:"column:payment_source"`
		ChargeGroupID *uint64 `gorm:"column:charge_group_id"`
	}
	tx.WithContext(ctx).Raw(`SELECT payment_source, charge_group_id FROM termin_payments WHERE id = ? AND tenant_id = ?`,
		info.ID, tenantID).Scan(&src)
	switch src.PaymentSource {
	case "booking_fee":
		rtype = ReceiptTypeBooking
	case "realization":
		rtype = ReceiptTypeRealization
	case "kpr_disbursement":
		rtype = ReceiptTypeKPRDisbursement
	}

	rec := &Receipt{
		TenantID:        tenantID,
		TerminPaymentID: &info.ID,
		UnitID:          &info.UnitID,
		SaleContractID:  contractID,
		ChargeGroupID:   src.ChargeGroupID,
		ReceiptType:     rtype,
		Amount:          info.Amount,
		BankAccountCode: info.BankAccountCode,
		ReceivedAt:      info.Date,
		Notes:           notes,
		CreatedBy:       createdBy,
	}
	alloc, nerr := nextReceiptNumber(tx, tenantID, rtype, info.Date)
	if nerr != nil {
		return nil, nerr
	}
	rec.ReceiptNumber = alloc.Number
	if err := tx.WithContext(ctx).Create(rec).Error; err != nil {
		return nil, fmt.Errorf("createReceiptForTerminInTx: %w", err)
	}
	// Registry dokumen dicatat di transaksi yang SAMA — kwitansi tanpa jejak di
	// registry akan terbaca sebagai nomor hilang saat audit.
	var actor *uint64
	if createdBy != 0 {
		actor = &createdBy
	}
	if _, derr := document.Register(tx, tenantID, alloc,
		document.Source{Table: "receipts", ID: rec.ID}, rec.Amount, actor); derr != nil {
		return nil, derr
	}
	return rec, nil
}

// ── Kwitansi Piutang Proyek Lama (W-7 extension) ─────────────────────────────
//
// LegacyReceiptInput adalah data kwitansi diberikan LANGSUNG oleh
// internal/legacyar (bukan dibaca ulang dari DB di sini) — legacyar sudah
// mengimpor billing (searah, mengikuti pola sale→billing) untuk memanggil
// fungsi ini di TRANSAKSI YANG SAMA dengan pelunasan, jadi billing TIDAK
// BOLEH balik mengimpor legacyar (circular import). Bentuknya sengaja minimal
// — hanya kunci penghubung (LegacyReceivablePaymentID) + data transaksi kas —
// karena data ringkasan piutang (customer, source, sisa) dibaca LIVE dari
// legacy_receivables saat kwitansi DICETAK (LoadReceiptPrintData), bukan
// dibekukan di sini. Ini pola yang sama dengan TerminInfo di atas.
type LegacyReceiptInput struct {
	LegacyReceivablePaymentID uint64
	Amount                    domain.Money
	BankAccountCode           string
	Date                      time.Time
	Notes                     string
}

// findReceiptByLegacyPaymentDB mengembalikan receipt yang sudah ada untuk
// satu baris legacy_receivable_payments — analog findReceiptByTerminDB.
func findReceiptByLegacyPaymentDB(ctx context.Context, db *gorm.DB, tenantID, legacyPaymentID uint64) (*Receipt, error) {
	var rec Receipt
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND legacy_receivable_payment_id = ?", tenantID, legacyPaymentID).
		First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReceiptNotFound
		}
		return nil, fmt.Errorf("findReceiptByLegacyPaymentDB: %w", err)
	}
	return &rec, nil
}

// CreateLegacyReceiptInTx membuat kwitansi piutang proyek lama secara IDEMPOTEN
// MEMAKAI transaksi yang diberikan — dipanggil oleh legacyar.Service.ReceivePayment
// di transaksi pelunasan yang sama (requirement #4/#5: tidak ada baris
// pelunasan tanpa kwitansi, dan retry/klik-ganda tidak pernah menerbitkan dua
// kwitansi untuk satu baris pelunasan yang sama).
func (r *GORMRepository) CreateLegacyReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy uint64, in LegacyReceiptInput) (*Receipt, error) {
	return createLegacyReceiptInTx(ctx, tx, tenantID, createdBy, in)
}

func createLegacyReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy uint64, in LegacyReceiptInput) (*Receipt, error) {
	// Fast path: sudah ada (dalam scope tx) — sama pola dengan kwitansi termin.
	if existing, err := findReceiptByLegacyPaymentDB(ctx, tx, tenantID, in.LegacyReceivablePaymentID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrReceiptNotFound) {
		return nil, err
	}

	paymentID := in.LegacyReceivablePaymentID
	rec := &Receipt{
		TenantID:                  tenantID,
		LegacyReceivablePaymentID: &paymentID,
		ReceiptType:               ReceiptTypeLegacyAR,
		Amount:                    in.Amount,
		BankAccountCode:           in.BankAccountCode,
		ReceivedAt:                in.Date,
		Notes:                     in.Notes,
		CreatedBy:                 createdBy,
	}
	alloc, nerr := nextReceiptNumber(tx, tenantID, ReceiptTypeLegacyAR, in.Date)
	if nerr != nil {
		return nil, nerr
	}
	rec.ReceiptNumber = alloc.Number
	if err := tx.WithContext(ctx).Create(rec).Error; err != nil {
		return nil, fmt.Errorf("createLegacyReceiptInTx: %w", err)
	}
	var actor *uint64
	if createdBy != 0 {
		actor = &createdBy
	}
	if _, derr := document.Register(tx, tenantID, alloc,
		document.Source{Table: "receipts", ID: rec.ID}, rec.Amount, actor); derr != nil {
		return nil, derr
	}
	return rec, nil
}

// ── ReceiptPrintLoader ────────────────────────────────────────────────────────

func (r *GORMRepository) LoadReceiptPrintData(ctx context.Context, tenantID, receiptID uint64) (*ReceiptPrintData, error) {
	type row struct {
		ReceiptNumber        string       `gorm:"column:receipt_number"`
		Amount               domain.Money `gorm:"column:amount"`
		ReceivedAt           time.Time    `gorm:"column:received_at"`
		BankAccountCode      string       `gorm:"column:bank_account_code"`
		Notes                string       `gorm:"column:notes"`
		CompanyName          string       `gorm:"column:company_name"`
		BuyerName            string       `gorm:"column:buyer_name"`
		BuyerID              string       `gorm:"column:buyer_id"`
		PaymentType          string       `gorm:"column:payment_type"`
		ProjectName          string       `gorm:"column:project_name"`
		UnitCode             string       `gorm:"column:unit_code"`
		UnitType             string       `gorm:"column:unit_type"`
		ReceiptType          string       `gorm:"column:receipt_type"`
		BankName             string       `gorm:"column:disbursing_bank_name"`
		TerminKind           string       `gorm:"column:termin_kind"`
		InstallmentNo        *int         `gorm:"column:installment_no"`
		LegacySourceLabel    string       `gorm:"column:legacy_source_label"`
		LegacyOriginalAmount domain.Money `gorm:"column:legacy_original_amount"`
		LegacyOutstanding    domain.Money `gorm:"column:legacy_outstanding"`
	}
	var res row
	// Pembayar kwitansi diambil dari KONTRAK bila ada. Untuk KWB tidak ada
	// kontrak — booking justru terjadi SEBELUM PPJB — sehingga kwitansi booking
	// selama ini tercetak tanpa nama, hanya "Pembeli". Kwitansi tanpa nama
	// penyetor bukan bukti penerimaan yang sah, jadi identitas dijatuhkan ke
	// customer booking-nya (bookings → customers) saat kontrak belum ada.
	//
	// Tabel `bookings` dibaca langsung (bukan lewat package sale) mengikuti
	// preseden TerminInfo di receipt.go: billing tidak boleh mengimpor sale —
	// circular import. Yang dibaca hanya identitas, bukan angka uang.
	//
	// E-2 — nama bank penyalur untuk KWD, urutan sumber dari yang paling
	// spesifik: financing source TERMIN ITU SENDIRI (bank yang benar-benar
	// mencairkan transaksi ini) → financing source kontrak → kolom bank_kpr
	// kontrak. Tidak ada nama bank yang ditulis di kode.
	//
	// Unit yang dicetak: r.unit_id adalah snapshot BEKU saat kwitansi
	// diterbitkan (sengaja tidak diubah oleh transfer unit — dokumen historis
	// lain, mis. jurnal/termin, memang tetap merujuk konteks saat terbit).
	// Tapi kwitansi booking (KWB) memakai kunci idempoten termin_payment_id
	// yang TIDAK berubah lintas transfer, sehingga cetak-ulang KWB pasca
	// TransferBookingAtomic (unit_id booking sudah pindah) akan tetap
	// menampilkan unit lama jika hanya baca r.unit_id — bug. Maka unit
	// diresolusi dari bk.unit_id (unit booking TERKINI) bila baris booking
	// ditemukan, fallback ke r.unit_id untuk kwitansi lain (KWT/KWR/KWD)
	// yang tidak terhubung ke booking manapun.
	//
	// units/projects sengaja LEFT JOIN (bukan JOIN): kwitansi piutang proyek
	// lama (legacy_ar, migration 000105) tidak pernah punya unit_id sama
	// sekali — INNER JOIN akan membuat kwitansi itu GAGAL dimuat sepenuhnya.
	// legacy_receivable_payments/legacy_receivables dibaca LANGSUNG (bukan
	// lewat package legacyar — billing tidak boleh mengimpor legacyar,
	// circular import; sama pola dengan bookings/customers di atas).
	// legacy_outstanding SELALU dihitung dari saldo TERKINI (original_amount
	// − paid_amount SAAT INI di legacy_receivables), bukan angka beku per
	// kwitansi — requirement #4: kwitansi pembayaran parsial tidak boleh
	// mencetak ulang piutang awal sebagai sisa.
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			r.receipt_number, r.receipt_type, r.amount, r.received_at, r.bank_account_code, r.notes,
			t.name        AS company_name,
			COALESCE(NULLIF(c.buyer_name, ''), cu.name, NULLIF(lr.customer_name, ''), '')  AS buyer_name,
			COALESCE(NULLIF(c.buyer_id, ''), cu.id_number, lrc.id_number, '')               AS buyer_id,
			COALESCE(c.payment_type, '') AS payment_type,
			COALESCE(NULLIF(fst.name, ''), NULLIF(fsc.name, ''), NULLIF(c.bank_kpr, ''), '')
			              AS disbursing_bank_name,
			COALESCE(p.name, '') AS project_name,
			COALESCE(u.code, '') AS unit_code, COALESCE(u.unit_type, '') AS unit_type,
			COALESCE(tp.kind, '') AS termin_kind, tp.installment_no,
			COALESCE(lr.source_label, '')             AS legacy_source_label,
			COALESCE(lr.original_amount, 0)           AS legacy_original_amount,
			COALESCE(lr.original_amount - lr.paid_amount, 0) AS legacy_outstanding
		FROM receipts r
		LEFT JOIN bookings bk ON bk.termin_payment_id = r.termin_payment_id AND bk.tenant_id = r.tenant_id
		LEFT JOIN units u    ON u.id = COALESCE(bk.unit_id, r.unit_id) AND u.tenant_id = r.tenant_id
		LEFT JOIN projects p ON p.id = u.project_id
		JOIN tenants t  ON t.id = r.tenant_id
		LEFT JOIN sale_contracts c ON c.id = r.sale_contract_id AND c.tenant_id = r.tenant_id
		LEFT JOIN customers cu ON cu.id = bk.customer_id AND cu.tenant_id = r.tenant_id
		LEFT JOIN termin_payments tp   ON tp.id = r.termin_payment_id AND tp.tenant_id = r.tenant_id
		LEFT JOIN financing_sources fst ON fst.id = tp.financing_source_id AND fst.tenant_id = r.tenant_id
		LEFT JOIN financing_sources fsc ON fsc.id = c.financing_source_id  AND fsc.tenant_id = r.tenant_id
		LEFT JOIN legacy_receivable_payments lrp ON lrp.id = r.legacy_receivable_payment_id AND lrp.tenant_id = r.tenant_id
		LEFT JOIN legacy_receivables lr ON lr.id = lrp.legacy_receivable_id AND lr.tenant_id = r.tenant_id
		LEFT JOIN customers lrc ON lrc.id = lr.customer_id AND lrc.tenant_id = r.tenant_id
		WHERE r.id = ? AND r.tenant_id = ?
	`, receiptID, tenantID).Scan(&res).Error
	if err != nil {
		return nil, fmt.Errorf("LoadReceiptPrintData: %w", err)
	}
	if res.ReceiptNumber == "" {
		return nil, ErrReceiptNotFound
	}
	return &ReceiptPrintData{
		ReceiptType:          ReceiptType(res.ReceiptType),
		ReceiptNumber:        res.ReceiptNumber,
		Amount:               res.Amount,
		ReceivedAt:           res.ReceivedAt,
		BankAccountCode:      res.BankAccountCode,
		Notes:                res.Notes,
		KindLabel:            terminKindLabel(res.TerminKind, res.InstallmentNo),
		CompanyName:          res.CompanyName,
		BuyerName:            res.BuyerName,
		BuyerID:              res.BuyerID,
		PaymentType:          res.PaymentType,
		DisbursingBankName:   res.BankName,
		ProjectName:          res.ProjectName,
		UnitCode:             res.UnitCode,
		UnitType:             res.UnitType,
		HasLegacySummary:     ReceiptType(res.ReceiptType) == ReceiptTypeLegacyAR,
		LegacyCustomerName:   res.BuyerName,
		LegacySourceLabel:    res.LegacySourceLabel,
		LegacyOriginalAmount: res.LegacyOriginalAmount,
		LegacyOutstanding:    res.LegacyOutstanding,
	}, nil
}

// ── Penomoran kwitansi ───────────────────────────────────────────────────────
//
// W-2: seluruh penomoran dokumen dilayani SATU engine (package document).
// Sebelumnya billing punya generatornya sendiri di atas `receipt_sequences`,
// dan invoice punya generator kedua di atas `invoice_sequences` — dua mesin
// dengan aturan tahun yang sama, ditulis dua kali.
//
// `ReceiptType` sengaja bernilai sama persis dengan kode `document_types`
// (house_payment | booking | realization), sehingga prefix KWT/KWB/KWR menjadi
// KONFIGURASI master, bukan switch di sini. Mengubah prefix kwitansi kini
// pekerjaan admin, bukan pekerjaan programmer.

// nextReceiptNumber mengalokasikan satu nomor kwitansi sesuai tipenya.
func nextReceiptNumber(tx *gorm.DB, tenantID uint64, rtype ReceiptType, issuedAt time.Time) (*document.Allocation, error) {
	return document.Allocate(tx, tenantID, string(rtype), issuedAt)
}
