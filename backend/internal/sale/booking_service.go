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
	if req.BookingFee.IsZero() || req.BookingFee.IsNeg() || !req.BookingFee.IsWholeRupiah() {
		return nil, ErrBookingFeeInvalid
	}
	if !req.ExpiryDate.After(req.BookingDate) {
		return nil, ErrBookingExpiryInvalid
	}
	if req.LandQuantityM2 != nil && !req.LandQuantityM2.IsPositive() {
		return nil, ErrLandQuantityInvalid
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
	// Akun: bank valid (COA-driven) + resolve ID bank & Pendapatan Booking.
	// RULE KLIEN 2026-07-29: fee = PENDAPATAN saat diterima (Cr 4-2100),
	// bukan titipan/kewajiban.
	if err := s.accounts.ValidateCashBankAccount(ctx, tenantID, req.BankAccountCode); err != nil {
		return nil, err
	}
	bankID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, req.BankAccountCode)
	if err != nil {
		return nil, err
	}
	revenueID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, accountCodePendapatanBooking)
	if err != nil {
		return nil, ErrBookingRevenueAccountMissing
	}

	pid := unit.ProjectID
	uid := req.UnitID
	b := &Booking{
		TenantID:      tenantID,
		ProjectID:     unit.ProjectID,
		UnitID:        req.UnitID,
		PhaseID:       unit.PhaseID,
		CustomerID:    req.CustomerID,
		SalesPersonID: req.SalesPersonID,
		LeadID:        req.LeadID,
		BookingFee:    req.BookingFee,
		// Refundable DIABAIKAN utk booking baru (rule klien: tidak ada refund —
		// pendapatan final). Field dipertahankan hanya utk baris legacy.
		Refundable:     false,
		BookingDate:    req.BookingDate,
		ExpiryDate:     req.ExpiryDate,
		Status:         BookingStatusActive,
		FeeDisposition: FeeRecognized,
		Notes:          req.Notes,
		CreatedBy:      req.CreatedBy,
		ActiveKey:      activeKeyY(),
		LandQuantityM2: req.LandQuantityM2,
	}
	return s.bookings.CreateBookingAtomic(ctx, CreateBookingAtomicParams{
		TenantID: tenantID,
		Booking:  b,
		JournalLines: []JournalLineInput{
			{AccountID: bankID, Debit: req.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Booking fee diterima"},
			{AccountID: revenueID, Credit: req.BookingFee, ProjectID: &pid, UnitID: &uid, Description: "Pendapatan Booking (diakui saat diterima)"},
		},
		BankAccountCode:   req.BankAccountCode,
		CreditAccountCode: accountCodePendapatanBooking,
		GenerateReceipt:   true,
		ReceiptNotes:      "Booking fee",
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
