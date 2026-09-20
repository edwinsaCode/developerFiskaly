package sale

// Increment 7 — Booking: business logic. Repo mengeksekusi atomik; service
// memvalidasi + me-resolve akun + membangun baris jurnal (pola preparePayment).

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// BookingStore adalah kontrak persistence booking (GORMRepository implementasinya).
type BookingStore interface {
	CreateBookingAtomic(ctx context.Context, p CreateBookingAtomicParams) (*Booking, error)
	ConvertWithContractAtomic(ctx context.Context, tenantID, bookingID uint64, c *SaleContract, in ConvertBookingInput) (*Booking, error)
	CloseBookingAtomic(ctx context.Context, tenantID, bookingID uint64, in CloseBookingInput) (*Booking, error)
	// DisposeConvertedFeeAtomic (R4 Opsi A): forfeit/refund fee outside-price
	// yang menetap di 2-2100 pasca-konversi (converted + held).
	DisposeConvertedFeeAtomic(ctx context.Context, tenantID, bookingID uint64, in DisposeFeeInput) (*Booking, error)
	// TransferBookingAtomic (Item 3): pindah booking active ke unit lain, TANPA
	// jurnal baru (lihat TransferBookingInput).
	TransferBookingAtomic(ctx context.Context, tenantID, bookingID, newUnitID uint64, in TransferBookingInput) (*Booking, error)
	FindBookingByID(ctx context.Context, tenantID, id uint64) (*Booking, error)
	FindActiveBookingByUnit(ctx context.Context, tenantID, unitID uint64) (*Booking, error)
	ListBookings(ctx context.Context, tenantID uint64, status BookingStatus) ([]*Booking, error)
	ListExpiredBookingIDs(ctx context.Context, tenantID uint64, asOf time.Time) ([]uint64, error)
}

// DisposeFeeInput: input disposisi fee outside-price (R4 Opsi A).
type DisposeFeeInput struct {
	Action               string // FeeDispositionActionForfeit | FeeDispositionActionRefund
	Reason               string
	EventDate            time.Time
	ActorID              *uint64
	TitipanAccountID     uint64
	OtherIncomeAccountID uint64
}

const (
	FeeDispositionActionForfeit = "forfeit"
	FeeDispositionActionRefund  = "refund"
)

// WithBookingStore mengaktifkan fitur Booking pada Service (wiring produksi).
// Tanpa opsi ini seluruh endpoint booking menolak dengan ErrBookingStoreNotConfigured
// (pola ContractStore) — unit test lama tidak terpengaruh.
func WithBookingStore(bs BookingStore) ServiceOption {
	return func(s *Service) { s.bookings = bs }
}

func (s *Service) requireBookingStore() error {
	if s.bookings == nil {
		return ErrBookingStoreNotConfigured
	}
	return nil
}

// CreateBooking membuat booking + penerimaan fee secara atomik.
func (s *Service) CreateBooking(ctx context.Context, tenantID uint64, req CreateBookingRequest) (*Booking, error) {
	if err := s.requireBookingStore(); err != nil {
		return nil, err
	}
	if req.UnitID == 0 {
		return nil, ErrUnitRequired
	}
	if req.CustomerID == 0 {
		return nil, ErrBookingCustomerRequired
	}
	// Client final note 2026-09-10: fee = 0 sah (murni reservasi, tanpa uang
	// booking) — hanya negatif/pecahan yang ditolak.
	if req.BookingFee.IsNeg() || !req.BookingFee.IsWholeRupiah() {
		return nil, ErrBookingFeeInvalid
	}
	if !req.ExpiryDate.After(req.BookingDate) {
		return nil, ErrBookingExpiryInvalid
	}
	// Validasi party (bila lookup ter-wire — pola scheme flow).
	if s.parties != nil {
		if err := s.parties.CustomerExists(ctx, tenantID, req.CustomerID); err != nil {
			return nil, err
		}
		if req.SalesPersonID != nil {
			if err := s.parties.SalesPersonExists(ctx, tenantID, *req.SalesPersonID); err != nil {
				return nil, err
			}
		}
	}
	// Unit harus available (matriks juga menegakkan di tx — ini utk error dini yang jelas).
	unit, err := s.units.FindUnitSaleInfo(ctx, tenantID, req.UnitID)
	if err != nil {
		return nil, err
	}
	if unit.Status != "available" {
		return nil, fmt.Errorf("%w: status unit %s (butuh available)", ErrBookingUnitStateInvalid, unit.Status)
	}
	feeZero := req.BookingFee.IsZero()

	// Akun: bank valid (COA-driven) + resolve ID bank — hanya relevan bila ADA
	// uang yang benar-benar berpindah (fee > 0). Fee 0 = tidak ada penerimaan
	// kas sama sekali, jadi bank tujuan tidak pernah dipakai/divalidasi.
	var bankID uint64
	if !feeZero {
		if err := s.accounts.ValidateCashBankAccount(ctx, tenantID, req.BankAccountCode); err != nil {
			return nil, err
		}
		var err error
		bankID, err = s.accounts.FindAccountIDByCode(ctx, tenantID, req.BankAccountCode)
		if err != nil {
			return nil, err
		}
	}

	// RULE KLIEN 2026-07-29 (default, mayoritas): fee = PENDAPATAN final saat
	// diterima (Cr 4-2100) — TIDAK BERUBAH oleh Item 3.
	//
	// Item 3 (2026-09, keputusan klien): booking yang ditandai Refundable SAAT
	// DIBUAT memakai jalur LEGACY held (Cr 2-2100 Titipan Booking) — SATU-
	// SATUNYA cara fee-nya bisa direfund nanti saat batal. Ini mengaktifkan
	// kembali mesin disposisi data-driven yang SUDAH ADA (CloseBookingAtomic
	// FeeHeld→pending_refund, cancellation.CreateBookingRefund/PayRefund) —
	// tanpa engine baru, hanya switch on/off di titik penerimaan fee.
	//
	// Client final note 2026-09-10: fee = 0 SELALU 'recognized', TANPA
	// memandang flag Refundable — tidak ada uang yang bisa dipegang/direfund,
	// jadi jalur held/reklas/forfeit tidak pernah tersentuh oleh booking Rp0.
	pid := unit.ProjectID
	uid := req.UnitID
	creditCode := ""
	disposition := FeeRecognized
	var journalLines []JournalLineInput
	if !feeZero {
		creditCode = accountCodePendapatanBooking
		creditDesc := "Pendapatan Booking (diakui saat diterima)"
		if req.Refundable {
			creditCode = accountCodeTitipanBooking
			disposition = FeeHeld
			creditDesc = "Titipan Booking (refundable — belum diakui pendapatan)"
		}
		creditID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, creditCode)
		if err != nil {
			if req.Refundable {
				return nil, ErrTitipanAccountMissing
			}
			return nil, ErrBookingRevenueAccountMissing
		}
		journalLines = []JournalLineInput{
			{AccountID: bankID, Debit: req.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Booking fee diterima"},
			{AccountID: creditID, Credit: req.BookingFee, ProjectID: &pid, UnitID: &uid, Description: creditDesc},
		}
	}

	b := &Booking{
		TenantID:       tenantID,
		ProjectID:      unit.ProjectID,
		UnitID:         req.UnitID,
		PhaseID:        unit.PhaseID,
		CustomerID:     req.CustomerID,
		SalesPersonID:  req.SalesPersonID,
		LeadID:         req.LeadID,
		BookingFee:     req.BookingFee,
		Refundable:     req.Refundable,
		BookingDate:    req.BookingDate,
		ExpiryDate:     req.ExpiryDate,
		Status:         BookingStatusActive,
		FeeDisposition: disposition,
		Notes:          req.Notes,
		CreatedBy:      req.CreatedBy,
		ActiveKey:      activeKeyY(),
	}
	return s.bookings.CreateBookingAtomic(ctx, CreateBookingAtomicParams{
		TenantID:          tenantID,
		Booking:           b,
		JournalLines:      journalLines,
		BankAccountCode:   req.BankAccountCode,
		CreditAccountCode: creditCode,
		GenerateReceipt:   !feeZero,
		ReceiptNotes:      "Booking fee",
		UnitCode:          unit.Code,
		ProjectName:       unit.ProjectName,
	})
}

func activeKeyY() *string { y := "Y"; return &y }

// CancelBooking membatalkan booking active (pra-PPJB).
func (s *Service) CancelBooking(ctx context.Context, tenantID, bookingID uint64, reason string, eventDate time.Time, actorID *uint64) (*Booking, error) {
	if err := s.requireBookingStore(); err != nil {
		return nil, err
	}
	in, err := s.buildCloseInput(ctx, tenantID, BookingStatusCancelled, reason, eventDate, actorID)
	if err != nil {
		return nil, err
	}
	return s.bookings.CloseBookingAtomic(ctx, tenantID, bookingID, in)
}

// TransferBooking (Item 3) memindahkan booking active ke unit lain TANPA
// jurnal baru — fee sudah diterima/diakui (recognized atau held) di unit
// ASAL, dan ledger append-only (invariant #5) melarang menulis-ulang jurnal
// atau kwitansi historisnya. Hanya baris booking + status kedua unit yang
// berpindah, sehingga TIDAK PERNAH terjadi penerimaan fee/pendapatan kedua
// kalinya (no double revenue by construction — tidak ada jalur jurnal sama
// sekali di operasi ini).
func (s *Service) TransferBooking(ctx context.Context, tenantID, bookingID, newUnitID uint64, reason string, eventDate time.Time, actorID *uint64) (*Booking, error) {
	if err := s.requireBookingStore(); err != nil {
		return nil, err
	}
	if newUnitID == 0 {
		return nil, ErrUnitRequired
	}
	b, err := s.bookings.FindBookingByID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	if b.Status != BookingStatusActive {
		return nil, ErrBookingNotActive
	}
	if newUnitID == b.UnitID {
		return nil, ErrBookingTransferSameUnit
	}
	unit, err := s.units.FindUnitSaleInfo(ctx, tenantID, newUnitID)
	if err != nil {
		return nil, err
	}
	if unit.Status != "available" {
		return nil, fmt.Errorf("%w: status unit tujuan %s (butuh available)", ErrBookingUnitStateInvalid, unit.Status)
	}
	if eventDate.IsZero() {
		eventDate = timeNow()
	}
	return s.bookings.TransferBookingAtomic(ctx, tenantID, bookingID, newUnitID, TransferBookingInput{
		NewUnitID: newUnitID,
		Reason:    reason,
		EventDate: eventDate,
		ActorID:   actorID,
	})
}

// MarkExpiredBookings: sweep idempoten (preseden mark-overdue) — menutup semua
// booking active yang lewat expiry per `asOf`. Per booking satu tx; kegagalan
// satu booking tidak menghentikan lainnya (partial progress aman — idempoten).
func (s *Service) MarkExpiredBookings(ctx context.Context, tenantID uint64, asOf time.Time) (int, error) {
	if err := s.requireBookingStore(); err != nil {
		return 0, err
	}
	ids, err := s.bookings.ListExpiredBookingIDs(ctx, tenantID, asOf)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	in, err := s.buildCloseInput(ctx, tenantID, BookingStatusExpired, "lewat masa berlaku booking", asOf, nil)
	if err != nil {
		return 0, err
	}
	count := 0
	var firstErr error
	for _, id := range ids {
		if _, cerr := s.bookings.CloseBookingAtomic(ctx, tenantID, id, in); cerr != nil {
			// Booking yang keburu converted/cancelled di antara list & close: lewati.
			if errors.Is(cerr, ErrBookingNotActive) || errors.Is(cerr, ErrBookingNotFound) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("booking %d: %w", id, cerr)
			}
			continue
		}
		count++
	}
	return count, firstErr
}

// DisposeConvertedBookingFee (R4 Opsi A): disposisi manual fee booking
// outside-price pasca-konversi. action: forfeit (jurnal Dr 2-2100/Cr 4-2000)
// atau refund (→ pending_refund; reklas + pembayaran via domain Refund existing).
func (s *Service) DisposeConvertedBookingFee(ctx context.Context, tenantID, bookingID uint64, action, reason string, eventDate time.Time, actorID *uint64) (*Booking, error) {
	if err := s.requireBookingStore(); err != nil {
		return nil, err
	}
	if action != FeeDispositionActionForfeit && action != FeeDispositionActionRefund {
		return nil, ErrInvalidFeeDispositionAction
	}
	titipanID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodeTitipanBooking)
	if err != nil {
		return nil, ErrTitipanAccountMissing
	}
	otherIncomeID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodePendapatanLain)
	if err != nil {
		return nil, fmt.Errorf("akun %s tidak ditemukan: %w", accountCodePendapatanLain, err)
	}
	if eventDate.IsZero() {
		eventDate = timeNow()
	}
	return s.bookings.DisposeConvertedFeeAtomic(ctx, tenantID, bookingID, DisposeFeeInput{
		Action:               action,
		Reason:               reason,
		EventDate:            eventDate,
		ActorID:              actorID,
		TitipanAccountID:     titipanID,
		OtherIncomeAccountID: otherIncomeID,
	})
}

// buildCloseInput me-resolve akun forfeit (dipakai bila ada booking non-refundable).
func (s *Service) buildCloseInput(ctx context.Context, tenantID uint64, next BookingStatus, reason string, eventDate time.Time, actorID *uint64) (CloseBookingInput, error) {
	titipanID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodeTitipanBooking)
	if err != nil {
		return CloseBookingInput{}, ErrTitipanAccountMissing
	}
	otherIncomeID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodePendapatanLain)
	if err != nil {
		return CloseBookingInput{}, fmt.Errorf("akun %s tidak ditemukan: %w", accountCodePendapatanLain, err)
	}
	return CloseBookingInput{
		NextStatus:           next,
		Reason:               reason,
		EventDate:            eventDate,
		ActorID:              actorID,
		TitipanAccountID:     titipanID,
		OtherIncomeAccountID: otherIncomeID,
	}, nil
}

// GetBooking / GetActiveBookingByUnit / ListBookings — reads.
func (s *Service) GetBooking(ctx context.Context, tenantID, id uint64) (*Booking, error) {
	if err := s.requireBookingStore(); err != nil {
		return nil, err
	}
	return s.bookings.FindBookingByID(ctx, tenantID, id)
}

func (s *Service) GetActiveBookingByUnit(ctx context.Context, tenantID, unitID uint64) (*Booking, error) {
	if err := s.requireBookingStore(); err != nil {
		return nil, err
	}
	return s.bookings.FindActiveBookingByUnit(ctx, tenantID, unitID)
}

func (s *Service) ListBookings(ctx context.Context, tenantID uint64, status BookingStatus) ([]*Booking, error) {
	if err := s.requireBookingStore(); err != nil {
		return nil, err
	}
	if status != "" && !status.Valid() {
		return nil, fmt.Errorf("status booking tidak dikenal: %s", status)
	}
	return s.bookings.ListBookings(ctx, tenantID, status)
}

// validateBookingForConversion: pra-validasi konversi di CreateContract
// (error dini yang jelas; repo tetap memvalidasi ulang di dalam tx).
func (s *Service) validateBookingForConversion(ctx context.Context, tenantID uint64, bookingID uint64, req CreateContractRequest) (*Booking, error) {
	if err := s.requireBookingStore(); err != nil {
		return nil, err
	}
	b, err := s.bookings.FindBookingByID(ctx, tenantID, bookingID)
	if err != nil {
		return nil, err
	}
	if b.Status != BookingStatusActive {
		return nil, ErrBookingNotActive
	}
	if b.UnitID != req.UnitID {
		return nil, ErrBookingUnitMismatch
	}
	// Kontrak produksi wajib customer (Increment 3); konversi wajib customer sama.
	if req.CustomerID == nil || *req.CustomerID != b.CustomerID {
		return nil, ErrBookingCustomerMismatch
	}
	// Unit harus masih booked (konversi mendorong booked → reserved).
	unit, err := s.units.FindUnitSaleInfo(ctx, tenantID, req.UnitID)
	if err != nil {
		return nil, err
	}
	if unit.Status != "booked" {
		return nil, fmt.Errorf("%w: status unit %s (butuh booked)", ErrBookingUnitStateInvalid, unit.Status)
	}
	return b, nil
}

// convertBookingWithContract mengeksekusi konversi (dipanggil CreateContract).
func (s *Service) convertBookingWithContract(ctx context.Context, tenantID, bookingID uint64, c *SaleContract, actorID *uint64) error {
	titipanID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodeTitipanBooking)
	if err != nil {
		return ErrTitipanAccountMissing
	}
	uangMukaID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodeUangMuka)
	if err != nil {
		return fmt.Errorf("akun %s tidak ditemukan: %w", accountCodeUangMuka, err)
	}
	_, err = s.bookings.ConvertWithContractAtomic(ctx, tenantID, bookingID, c, ConvertBookingInput{
		ActorID:           actorID,
		TitipanAccountID:  titipanID,
		UangMukaAccountID: uangMukaID,
		EventDate:         c.ContractDate,
	})
	return err
}
