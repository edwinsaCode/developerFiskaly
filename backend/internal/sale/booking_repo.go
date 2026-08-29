package sale

// Increment 7 — Booking: atomic writers (pola BASTAtomicWriter / CommitPayment).
// Setiap operasi uang + status + log lifecycle unit = SATU transaksi DB.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/document"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
)

// CreateBookingAtomicParams: seluruh input tervalidasi + akun ter-resolve
// (service yang menyiapkan; repo hanya mengeksekusi atomik).
type CreateBookingAtomicParams struct {
	TenantID     uint64
	Booking      *Booking // pre-filled: active, recognized, ActiveKey='Y'
	JournalLines []JournalLineInput
	// CreditAccountCode: akun kredit penerimaan fee — 4-2100 Pendapatan Booking
	// (rule klien 2026-07-29). Dicatat di termin sebagai audit trail.
	CreditAccountCode string
	BankAccountCode   string
	GenerateReceipt   bool
	ReceiptNotes      string
}

// transitionUnitPinnedTx meng-update status unit dengan predikat DI-PIN ke
// `from` + menulis log lifecycle DALAM tx pemanggil (Increment 6 pattern).
// 0 baris = status berubah konkuren / unit tak di state itu → konflik.
func transitionUnitPinnedTx(ctx context.Context, tx *gorm.DB, tenantID, unitID uint64, from, to project.UnitStatus, log *project.UnitStatusTransition) error {
	res := tx.WithContext(ctx).Model(&project.Unit{}).
		Where("id = ? AND tenant_id = ? AND status = ?", unitID, tenantID, string(from)).
		Update("status", string(to))
	if res.Error != nil {
		return fmt.Errorf("update status unit %d: %w", unitID, res.Error)
	}
	if res.RowsAffected == 0 {
		return project.ErrUnitTransitionConflict
	}
	return project.LogTransitionTx(tx, log)
}

// isDuplicateKey mendeteksi pelanggaran UNIQUE MySQL (error 1062).
func isDuplicateKey(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}

// CreateBookingAtomic: jurnal fee (Dr Bank / Cr Pendapatan Booking — rule
// klien 2026-07-29) → termin (flag FALSE, audit) → kwitansi → baris booking →
// unit available→booked + log. SATU transaksi; gagal = nihil.
// TANPA baris alokasi buyer_credit — fee adalah PENDAPATAN final, tidak pernah
// menjadi kredit buyer / bagian harga.
func (r *GORMRepository) CreateBookingAtomic(ctx context.Context, p CreateBookingAtomicParams) (*Booking, error) {
	b := p.Booking
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Jurnal penerimaan fee.
		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
		entry, err := txPosting.Create(ctx, ledger.CreateJournalRequest{
			TenantID:    p.TenantID,
			Date:        b.BookingDate,
			Description: fmt.Sprintf("Booking fee unit %d", b.UnitID),
			Lines:       toledgerLines(p.JournalLines),
		})
		if err != nil {
			return fmt.Errorf("buat jurnal booking fee: %w", err)
		}
		// Jurnal tetap DRAFT sampai KWB-nya terbit dan tertaut (langkah 3b):
		// W-3.5 menolak jurnal kas yang selesai diposting tanpa dokumen, dan
		// KWB baru bisa lahir sesudah termin — yang butuh id jurnal ini.

		// 2. Termin (audit penerimaan; TANPA alokasi — lihat komentar atas).
		termin := &TerminPayment{
			TenantID:          p.TenantID,
			UnitID:            b.UnitID,
			ProjectID:         b.ProjectID,
			PhaseID:           b.PhaseID,
			Amount:            b.BookingFee,
			BankAccountCode:   p.BankAccountCode,
			Date:              b.BookingDate,
			Description:       "Booking fee",
			JournalEntryID:    entry.ID,
			CreditAccountCode: p.CreditAccountCode,
			CreatedBy:         b.CreatedBy,
			PaymentSource:     PaymentSourceBookingFee,
			Kind:              TerminKindOther,
			// Booking fee di LUAR harga unit — TIDAK pernah mengurangi
			// outstanding (flag = SoT tunggal "mengurangi harga atau tidak").
			CountsTowardPrice: false,
		}
		if err := tx.WithContext(ctx).Create(termin).Error; err != nil {
			return fmt.Errorf("simpan termin booking fee: %w", err)
		}

		// 3. Kwitansi KWB (idempoten, seam billing) — WAJIB, bukan opsional.
		//
		// W-3.2 (I-2): booking memang sudah menerbitkan KWB lewat Document
		// Engine W-2, jadi tidak ada jalur kwitansi baru yang dibuat di sini.
		// Yang diperbaiki adalah fail-open-nya: `p.GenerateReceipt` bernilai
		// false atau seam yang belum terpasang membuat langkah 1 memposting
		// penerimaan kas tanpa bukti apa pun.
		if r.receiptTx == nil {
			return fmt.Errorf("wiring tidak lengkap: penerimaan booking fee tanpa generator kwitansi")
		}
		var cb uint64
		if b.CreatedBy != nil {
			cb = *b.CreatedBy
		}
		receiptNumber, receiptID, rerr := r.receiptTx.GenerateReceiptInTx(ctx, tx, p.TenantID, cb,
			termin.ID, b.UnitID, b.BookingFee, p.BankAccountCode, b.BookingDate, p.ReceiptNotes)
		if rerr != nil {
			return fmt.Errorf("buat kwitansi booking fee: %w", rerr)
		}
		// Nomor kwitansi ikut pulang bersama booking: admin yang baru saja
		// menerima uang bisa langsung menyebut/mencetak lembarannya.
		rid := receiptID
		b.ReceiptID = &rid
		b.ReceiptNumber = receiptNumber
		// 3b. Tautkan KWB ke jurnal kasnya (INV-DOC-1).
		if _, derr := document.LinkJournalBySource(tx.WithContext(ctx), p.TenantID, "receipts", receiptID, entry.ID); derr != nil {
			return fmt.Errorf("tautkan kwitansi booking ke jurnal: %w", derr)
		}
		// 3c. Buktinya ada — kas baru boleh bergerak (W-3.5).
		if _, err := txPosting.PostDraft(ctx, p.TenantID, entry.ID, ledger.DocumentSpec{}); err != nil {
			return fmt.Errorf("posting jurnal booking fee: %w", err)
		}

		// 3d. Produk Tambahan: Kelebihan Tanah (opsional, kelebihan-tanah-booking-
		// integration-2026-08). Salesperson HANYA input quantity di form Booking
		// (b.LandQuantityM2, diisi service.go); pool, harga, dan reservasi
		// diresolve+dikunci DI SINI, atomik dengan booking — gagal salah satu,
		// batal keduanya.
		if b.LandQuantityM2 != nil {
			pool, perr := land.FindPoolByProjectTx(ctx, tx, p.TenantID, b.ProjectID)
			if perr != nil {
				return fmt.Errorf("resolusi pool Kelebihan Tanah: %w", perr)
			}
			expiry := b.ExpiryDate
			res, rerr := land.ReserveTx(ctx, tx, p.TenantID, pool.ID, land.ReserveInput{
				ProjectID:     b.ProjectID,
				CustomerID:    b.CustomerID,
				SalesPersonID: b.SalesPersonID,
				QuantityM2:    *b.LandQuantityM2,
				ReservedAt:    b.BookingDate,
				ExpiryDate:    &expiry,
				CreatedBy:     b.CreatedBy,
			})
			if rerr != nil {
				return fmt.Errorf("reservasi Kelebihan Tanah: %w", rerr)
			}
			poolID, resID, price := pool.ID, res.ID, res.UnitPriceSnapshot
			b.LandStockID = &poolID
			b.LandReservationID = &resID
			b.LandUnitPriceSnapshot = &price
		}

		// 4. Baris booking (satu aktif per unit — UNIQUE active_key).
		b.TerminPaymentID = termin.ID
		if err := tx.WithContext(ctx).Create(b).Error; err != nil {
			if isDuplicateKey(err) {
				return ErrActiveBookingExists
			}
			return fmt.Errorf("simpan booking: %w", err)
		}

		// 5. Unit available → booked + log (event booking_created, ref booking).
		refID := b.ID
		return transitionUnitPinnedTx(ctx, tx, p.TenantID, b.UnitID,
			project.UnitStatusAvailable, project.UnitStatusBooked,
			&project.UnitStatusTransition{
				TenantID:      p.TenantID,
				UnitID:        b.UnitID,
				FromStatus:    project.UnitStatusAvailable,
				ToStatus:      project.UnitStatusBooked,
				Event:         project.EventBookingCreated,
				EventDate:     b.BookingDate,
				ReferenceType: project.RefTypeBooking,
				ReferenceID:   &refID,
				ActorID:       b.CreatedBy,
			})
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ConvertWithContractAtomic: kontrak + konversi booking dalam SATU transaksi —
// tx.Create(kontrak) → BRANCH R4 pada flag counts_toward_price fee termin:
// jalur lama (TRUE): jurnal reklas (Dr Titipan / Cr Uang Muka) + buyer_credit;
// jalur baru (FALSE): fee menetap di 2-2100 sebagai kewajiban terpisah (Opsi A)
// → booking converted → unit booked→reserved + log. D3 existing (reserved→ppjb)
// menyusul best-effort di CreateContract setelah tx ini commit.
func (r *GORMRepository) ConvertWithContractAtomic(ctx context.Context, tenantID, bookingID uint64, c *SaleContract, in ConvertBookingInput) (*Booking, error) {
	var b Booking
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Lock booking; wajib active.
		err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", bookingID, tenantID).
			First(&b).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBookingNotFound
			}
			return fmt.Errorf("lock booking %d: %w", bookingID, err)
		}
		if b.Status != BookingStatusActive {
			return ErrBookingNotActive
		}
		if c.UnitID != b.UnitID {
			return ErrBookingUnitMismatch
		}

		// 2. Kontrak DI DALAM tx yang sama (atomik penuh dengan konversi).
		if err := tx.WithContext(ctx).Create(c).Error; err != nil {
			return fmt.Errorf("simpan sale contract (konversi booking): %w", err)
		}

		// 3. Branch DATA-DRIVEN pada flag fee termin (SATU SoT: counts_toward_price):
		//    - TRUE  (booking histori pra-R4, in-flight) → jalur LAMA: reklas
		//      Dr Titipan / Cr Uang Muka + buyer_credit (fee bagian harga).
		//    - FALSE → fee di LUAR harga: TANPA jurnal reklas, TANPA
		//      buyer_credit; disposisi TIDAK berubah:
		//        recognized (rule klien) → tetap recognized (pendapatan final);
		//        held (legacy R4)        → tetap held (menunggu disposisi manual).
		//      Outstanding kontrak = gross penuh.
		var feeTermin TerminPayment
		if err := tx.WithContext(ctx).
			Where("id = ? AND tenant_id = ?", b.TerminPaymentID, tenantID).
			First(&feeTermin).Error; err != nil {
			return fmt.Errorf("baca termin fee booking %d: %w", b.ID, err)
		}

		disposition := b.FeeDisposition
		var reclassJournalID *uint64
		if feeTermin.CountsTowardPrice {
			// Jalur LAMA — fee dikonversi menjadi bagian pembayaran harga.
			txLedgerRepo := ledger.NewGORMRepository(tx)
			txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
			pid, uid := b.ProjectID, b.UnitID
			entry, err := txPosting.Create(ctx, ledger.CreateJournalRequest{
				TenantID:    tenantID,
				Date:        in.EventDate,
				Description: fmt.Sprintf("Konversi booking #%d → kontrak: reklas titipan ke uang muka", b.ID),
				Lines: toledgerLines([]JournalLineInput{
					{AccountID: in.TitipanAccountID, Debit: b.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Reklas titipan booking"},
					{AccountID: in.UangMukaAccountID, Credit: b.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Uang muka dari booking fee"},
				}),
			})
			if err != nil {
				return fmt.Errorf("buat jurnal reklas booking: %w", err)
			}
			if _, err := txPosting.Post(ctx, tenantID, entry.ID); err != nil {
				return fmt.Errorf("posting jurnal reklas booking: %w", err)
			}
			reclassJournalID = &entry.ID
			disposition = FeeTransferred

			// Fee menjadi saldo kredit buyer (sub-ledger; rekonsiliasi FE-2/FE-3).
			if err := insertBuyerCreditTx(ctx, tx, tenantID, BuyerCreditAllocationInput{
				TerminPaymentID: b.TerminPaymentID,
				Amount:          b.BookingFee,
				CreatedBy:       in.ActorID,
			}); err != nil {
				return err
			}
		}

		// 5. Booking → converted (pinned ke active; terminal immutable).
		now := time.Now()
		res := tx.WithContext(ctx).Model(&Booking{}).
			Where("id = ? AND tenant_id = ? AND status = ?", b.ID, tenantID, string(BookingStatusActive)).
			Updates(map[string]interface{}{
				"status":                string(BookingStatusConverted),
				"fee_disposition":       string(disposition),
				"converted_contract_id": c.ID,
				"reclass_journal_id":    reclassJournalID,
				"closed_at":             &now,
				"closed_event_date":     &in.EventDate,
				"closed_by":             in.ActorID,
				"active_key":            nil,
			})
		if res.Error != nil {
			return fmt.Errorf("update booking converted: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrBookingNotActive
		}

		// 6. Unit booked → reserved + log (blueprint §9: fee menjadi komitmen DP).
		//    D3 (reserved → ppjb, ref kontrak) menyusul setelah commit.
		refID := b.ID
		if err := transitionUnitPinnedTx(ctx, tx, tenantID, b.UnitID,
			project.UnitStatusBooked, project.UnitStatusReserved,
			&project.UnitStatusTransition{
				TenantID:      tenantID,
				UnitID:        b.UnitID,
				FromStatus:    project.UnitStatusBooked,
				ToStatus:      project.UnitStatusReserved,
				Event:         project.EventReservationConfirmed,
				EventDate:     in.EventDate,
				ReferenceType: project.RefTypeBooking,
				ReferenceID:   &refID,
				ActorID:       in.ActorID,
			}); err != nil {
			return err
		}

		b.Status = BookingStatusConverted
		b.FeeDisposition = disposition
		b.ConvertedContractID = &c.ID
		b.ReclassJournalID = reclassJournalID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// CloseBookingAtomic menutup satu booking (cancelled|expired) atomik:
// booking terminal + unit booked→available + log. Disposisi fee data-driven:
// recognized (rule klien) → TANPA jurnal & TANPA refund (pendapatan final);
// legacy held → forfeit (jurnal 4-2000) / pending_refund seperti dulu.
//
// Resilien: bila unit sudah TIDAK di status booked (dipindah manual — kasus
// stranded), transisi unit DILEWATI (log jujur: tidak terjadi transisi) dan
// penutupan booking tetap berjalan — disposisi uang tidak boleh tersandera.
func (r *GORMRepository) CloseBookingAtomic(ctx context.Context, tenantID, bookingID uint64, in CloseBookingInput) (*Booking, error) {
	if in.NextStatus != BookingStatusCancelled && in.NextStatus != BookingStatusExpired {
		return nil, fmt.Errorf("%w: %s", ErrBookingNotActive, in.NextStatus)
	}
	var b Booking
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", bookingID, tenantID).
			First(&b).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBookingNotFound
			}
			return fmt.Errorf("lock booking %d: %w", bookingID, err)
		}
		if b.Status != BookingStatusActive {
			return ErrBookingNotActive
		}

		// Disposisi fee — BRANCH DATA-DRIVEN:
		//   recognized (rule klien 2026-07-29): pendapatan SUDAH final saat
		//   diterima → tutup TANPA jurnal, TANPA refund; disposisi tetap.
		//   Legacy held (baris pra-rule): jalur lama (forfeit / pending_refund).
		disposition := b.FeeDisposition
		var forfeitJournalID *uint64
		if b.FeeDisposition == FeeHeld {
			disposition = FeePendingRefund
			if !b.Refundable {
				disposition = FeeForfeited
				// Jurnal forfeit LEGACY: Dr Titipan Booking / Cr Pendapatan Lain-lain.
				txLedgerRepo := ledger.NewGORMRepository(tx)
				txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
				pid, uid := b.ProjectID, b.UnitID
				entry, jerr := txPosting.Create(ctx, ledger.CreateJournalRequest{
					TenantID:    tenantID,
					Date:        in.EventDate,
					Description: fmt.Sprintf("Booking #%d %s: forfeit booking fee", b.ID, in.NextStatus),
					Lines: toledgerLines([]JournalLineInput{
						{AccountID: in.TitipanAccountID, Debit: b.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Titipan booking hangus"},
						{AccountID: in.OtherIncomeAccountID, Credit: b.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Pendapatan lain-lain (forfeit)"},
					}),
				})
				if jerr != nil {
					return fmt.Errorf("buat jurnal forfeit: %w", jerr)
				}
				if _, jerr := txPosting.Post(ctx, tenantID, entry.ID); jerr != nil {
					return fmt.Errorf("posting jurnal forfeit: %w", jerr)
				}
				forfeitJournalID = &entry.ID
			}
		}

		now := time.Now()
		res := tx.WithContext(ctx).Model(&Booking{}).
			Where("id = ? AND tenant_id = ? AND status = ?", b.ID, tenantID, string(BookingStatusActive)).
			Updates(map[string]interface{}{
				"status":             string(in.NextStatus),
				"fee_disposition":    string(disposition),
				"forfeit_journal_id": forfeitJournalID,
				"close_reason":       in.Reason,
				"closed_at":          &now,
				"closed_event_date":  &in.EventDate,
				"closed_by":          in.ActorID,
				"active_key":         nil,
			})
		if res.Error != nil {
			return fmt.Errorf("update booking %s: %w", in.NextStatus, res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrBookingNotActive
		}

		// Unit booked → available (best-effort pinned; skip bila sudah dipindah).
		event := project.EventBookingCancelled
		if in.NextStatus == BookingStatusExpired {
			event = project.EventBookingExpired
		}
		refID := b.ID
		uerr := transitionUnitPinnedTx(ctx, tx, tenantID, b.UnitID,
			project.UnitStatusBooked, project.UnitStatusAvailable,
			&project.UnitStatusTransition{
				TenantID:      tenantID,
				UnitID:        b.UnitID,
				FromStatus:    project.UnitStatusBooked,
				ToStatus:      project.UnitStatusAvailable,
				Event:         event,
				EventDate:     in.EventDate,
				ReferenceType: project.RefTypeBooking,
				ReferenceID:   &refID,
				ActorID:       in.ActorID,
				Notes:         in.Reason,
			})
		if uerr != nil && !errors.Is(uerr, project.ErrUnitTransitionConflict) {
			return uerr
		}

		// Produk Tambahan: Kelebihan Tanah — lepas reservasi bersama penutupan
		// booking (kelebihan-tanah-booking-integration-2026-08). Booking batal/
		// expire tanpa konversi → reservasi tanahnya juga batal/expire, atomik
		// (INV-LAND-1: reserved_quantity_m2 turun di transaksi yang sama).
		if b.LandReservationID != nil {
			landStatus := land.ReservationStatusExpired
			if in.NextStatus == BookingStatusCancelled {
				landStatus = land.ReservationStatusCancelled
			}
			if _, lerr := land.CloseReservationTx(ctx, tx, tenantID, *b.LandReservationID, landStatus, in.Reason); lerr != nil {
				return fmt.Errorf("lepas reservasi Kelebihan Tanah: %w", lerr)
			}
		}

		b.Status = in.NextStatus
		b.FeeDisposition = disposition
		b.ForfeitJournalID = forfeitJournalID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// DisposeConvertedFeeAtomic (R4 Opsi A): disposisi manual fee booking
// outside-price yang MENETAP di 2-2100 setelah konversi (converted + held).
//
//	forfeit → jurnal Dr Titipan / Cr Pendapatan Lain-lain; disposisi 'forfeited'.
//	refund  → disposisi 'pending_refund' TANPA jurnal — reklas ke Hutang Refund
//	          terjadi di cancellation.CreateBookingRefund (jalur existing).
//
// Hanya sah untuk booking converted ber-disposisi held (fee termin
// counts_toward_price=FALSE — dijaga ganda di sini).
func (r *GORMRepository) DisposeConvertedFeeAtomic(ctx context.Context, tenantID, bookingID uint64, in DisposeFeeInput) (*Booking, error) {
	var b Booking
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", bookingID, tenantID).
			First(&b).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBookingNotFound
			}
			return fmt.Errorf("lock booking %d: %w", bookingID, err)
		}
		if b.Status != BookingStatusConverted || b.FeeDisposition != FeeHeld {
			return ErrFeeNotDisposable
		}
		// Jaga ganda: hanya fee outside-price (flag FALSE) yang menetap di 2-2100.
		var counts bool
		if err := tx.WithContext(ctx).
			Table("termin_payments").
			Select("counts_toward_price").
			Where("id = ? AND tenant_id = ?", b.TerminPaymentID, tenantID).
			Scan(&counts).Error; err != nil {
			return fmt.Errorf("baca termin fee: %w", err)
		}
		if counts {
			return ErrFeeNotDisposable
		}

		disposition := FeePendingRefund
		var forfeitJournalID *uint64
		if in.Action == FeeDispositionActionForfeit {
			disposition = FeeForfeited
			txLedgerRepo := ledger.NewGORMRepository(tx)
			txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
			pid, uid := b.ProjectID, b.UnitID
			entry, jerr := txPosting.Create(ctx, ledger.CreateJournalRequest{
				TenantID:    tenantID,
				Date:        in.EventDate,
				Description: fmt.Sprintf("Disposisi fee booking #%d (outside price): forfeit", b.ID),
				Lines: toledgerLines([]JournalLineInput{
					{AccountID: in.TitipanAccountID, Debit: b.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Titipan booking hangus"},
					{AccountID: in.OtherIncomeAccountID, Credit: b.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Pendapatan lain-lain (forfeit)"},
				}),
			})
			if jerr != nil {
				return fmt.Errorf("buat jurnal forfeit fee: %w", jerr)
			}
			if _, jerr := txPosting.Post(ctx, tenantID, entry.ID); jerr != nil {
				return fmt.Errorf("posting jurnal forfeit fee: %w", jerr)
			}
			forfeitJournalID = &entry.ID
		}

		res := tx.WithContext(ctx).Model(&Booking{}).
			Where("id = ? AND tenant_id = ? AND status = ? AND fee_disposition = ?",
				b.ID, tenantID, string(BookingStatusConverted), string(FeeHeld)).
			Updates(map[string]interface{}{
				"fee_disposition":    string(disposition),
				"forfeit_journal_id": forfeitJournalID,
			})
		if res.Error != nil {
			return fmt.Errorf("update disposisi fee: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrFeeNotDisposable
		}
		b.FeeDisposition = disposition
		b.ForfeitJournalID = forfeitJournalID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ── Reads ─────────────────────────────────────────────────────────────────────

// attachBookingReceipts mengisi ReceiptID/ReceiptNumber (kwitansi KWB) untuk
// setiap booking dari tabel `receipts`, satu query untuk semua baris.
//
// Kenapa membaca tabelnya langsung, bukan lewat package billing: arahnya sama
// dengan yang sudah ada di sisi seberang (billing membaca `termin_payments`
// mentah, lihat komentar TerminInfo) — mengimpor satu sama lain berarti
// circular import. Yang dibaca hanya NOMOR DOKUMEN, bukan angka uang, jadi
// tidak ada sumber kebenaran kedua yang lahir di sini.
//
// Kegagalan di sini TIDAK menggagalkan pembacaan booking: nomor kwitansi
// adalah pelengkap tampilan, bukan syarat sah booking — bookingnya sendiri
// tidak mungkin ada tanpa kwitansi (dijamin atomik saat dibuat).
func (r *GORMRepository) attachBookingReceipts(ctx context.Context, tenantID uint64, bs []*Booking) {
	ids := make([]uint64, 0, len(bs))
	for _, b := range bs {
		if b != nil && b.TerminPaymentID != 0 {
			ids = append(ids, b.TerminPaymentID)
		}
	}
	if len(ids) == 0 {
		return
	}
	type recRow struct {
		ID              uint64 `gorm:"column:id"`
		TerminPaymentID uint64 `gorm:"column:termin_payment_id"`
		ReceiptNumber   string `gorm:"column:receipt_number"`
	}
	var rows []recRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT id, termin_payment_id, receipt_number
		  FROM receipts
		 WHERE tenant_id = ? AND termin_payment_id IN ?
	`, tenantID, ids).Scan(&rows).Error; err != nil {
		return
	}
	byTermin := make(map[uint64]recRow, len(rows))
	for _, row := range rows {
		byTermin[row.TerminPaymentID] = row
	}
	for _, b := range bs {
		if row, ok := byTermin[b.TerminPaymentID]; ok {
			id := row.ID
			b.ReceiptID = &id
			b.ReceiptNumber = row.ReceiptNumber
		}
	}
}

func (r *GORMRepository) FindBookingByID(ctx context.Context, tenantID, id uint64) (*Booking, error) {
	var b Booking
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&b).Error
	if err != nil {
		return nil, ErrBookingNotFound
	}
	r.attachBookingReceipts(ctx, tenantID, []*Booking{&b})
	return &b, nil
}

func (r *GORMRepository) FindActiveBookingByUnit(ctx context.Context, tenantID, unitID uint64) (*Booking, error) {
	var b Booking
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND unit_id = ? AND status = ?", tenantID, unitID, string(BookingStatusActive)).
		First(&b).Error
	if err != nil {
		return nil, ErrBookingNotFound
	}
	r.attachBookingReceipts(ctx, tenantID, []*Booking{&b})
	return &b, nil
}

func (r *GORMRepository) ListBookings(ctx context.Context, tenantID uint64, status BookingStatus) ([]*Booking, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	var out []*Booking
	if err := q.Order("id DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListBookings: %w", err)
	}
	r.attachBookingReceipts(ctx, tenantID, out)
	return out, nil
}

// ListExpiredBookingIDs mengembalikan ID booking ACTIVE yang expiry_date < asOf
// (kandidat sweep; penutupan per booking via CloseBookingAtomic — idempoten).
func (r *GORMRepository) ListExpiredBookingIDs(ctx context.Context, tenantID uint64, asOf time.Time) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).Model(&Booking{}).
		Where("tenant_id = ? AND status = ? AND expiry_date < ?", tenantID, string(BookingStatusActive), asOf).
		Order("id ASC").
		Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("ListExpiredBookingIDs: %w", err)
	}
	return ids, nil
}
