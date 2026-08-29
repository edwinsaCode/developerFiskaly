package sale_test

// Increment 7 — Booking: unit tests (in-memory). Jalur uang/atomik diuji di
// booking_integration_test.go (real MySQL).

import (
	"context"
	"errors"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// ── State machine (pure) ──────────────────────────────────────────────────────

func TestBookingStatus_Machine(t *testing.T) {
	terminals := []sale.BookingStatus{sale.BookingStatusConverted, sale.BookingStatusExpired, sale.BookingStatusCancelled}
	for _, next := range terminals {
		if !sale.BookingStatusActive.CanTransitionTo(next) {
			t.Errorf("active → %s harus sah", next)
		}
		// Terminal immutable.
		for _, other := range append(terminals, sale.BookingStatusActive) {
			if next.CanTransitionTo(other) {
				t.Errorf("%s → %s harus ilegal (terminal immutable)", next, other)
			}
		}
	}
	if sale.BookingStatusActive.CanTransitionTo(sale.BookingStatusActive) {
		t.Error("active → active harus ilegal")
	}
	if !sale.BookingStatus("cancelled").Terminal() || sale.BookingStatusActive.Terminal() {
		t.Error("Terminal() salah")
	}
	if sale.BookingStatus("draft").Valid() {
		t.Error("'draft' bukan status sah (blueprint §2 — tanpa draft)")
	}
}

// ── Validasi service (mock store minimal) ─────────────────────────────────────

type mockBookingStore struct {
	byID     map[uint64]*sale.Booking
	created  *sale.CreateBookingAtomicParams
	closed   []sale.CloseBookingInput
	expired  []uint64
	failWith error
}

func newMockBookingStore() *mockBookingStore {
	return &mockBookingStore{byID: map[uint64]*sale.Booking{}}
}

func (m *mockBookingStore) CreateBookingAtomic(_ context.Context, p sale.CreateBookingAtomicParams) (*sale.Booking, error) {
	if m.failWith != nil {
		return nil, m.failWith
	}
	m.created = &p
	p.Booking.ID = uint64(len(m.byID) + 1)
	m.byID[p.Booking.ID] = p.Booking
	return p.Booking, nil
}

func (m *mockBookingStore) ConvertWithContractAtomic(_ context.Context, _ uint64, bookingID uint64, c *sale.SaleContract, _ sale.ConvertBookingInput) (*sale.Booking, error) {
	b, ok := m.byID[bookingID]
	if !ok {
		return nil, sale.ErrBookingNotFound
	}
	if b.Status != sale.BookingStatusActive {
		return nil, sale.ErrBookingNotActive
	}
	c.ID = 900
	b.Status = sale.BookingStatusConverted
	b.FeeDisposition = sale.FeeTransferred
	b.ConvertedContractID = &c.ID
	return b, nil
}

func (m *mockBookingStore) DisposeConvertedFeeAtomic(_ context.Context, _ uint64, bookingID uint64, in sale.DisposeFeeInput) (*sale.Booking, error) {
	b, ok := m.byID[bookingID]
	if !ok {
		return nil, sale.ErrBookingNotFound
	}
	if b.Status != sale.BookingStatusConverted || b.FeeDisposition != sale.FeeHeld {
		return nil, sale.ErrFeeNotDisposable
	}
	if in.Action == sale.FeeDispositionActionForfeit {
		b.FeeDisposition = sale.FeeForfeited
	} else {
		b.FeeDisposition = sale.FeePendingRefund
	}
	return b, nil
}

func (m *mockBookingStore) CloseBookingAtomic(_ context.Context, _ uint64, bookingID uint64, in sale.CloseBookingInput) (*sale.Booking, error) {
	b, ok := m.byID[bookingID]
	if !ok {
		return nil, sale.ErrBookingNotFound
	}
	if b.Status != sale.BookingStatusActive {
		return nil, sale.ErrBookingNotActive
	}
	m.closed = append(m.closed, in)
	b.Status = in.NextStatus
	// Cermin repo produksi: recognized (rule klien) tidak berubah;
	// legacy held → pending_refund/forfeited.
	if b.FeeDisposition == sale.FeeHeld {
		if b.Refundable {
			b.FeeDisposition = sale.FeePendingRefund
		} else {
			b.FeeDisposition = sale.FeeForfeited
		}
	}
	return b, nil
}

func (m *mockBookingStore) FindBookingByID(_ context.Context, _ uint64, id uint64) (*sale.Booking, error) {
	b, ok := m.byID[id]
	if !ok {
		return nil, sale.ErrBookingNotFound
	}
	return b, nil
}

func (m *mockBookingStore) FindActiveBookingByUnit(_ context.Context, _ uint64, unitID uint64) (*sale.Booking, error) {
	for _, b := range m.byID {
		if b.UnitID == unitID && b.Status == sale.BookingStatusActive {
			return b, nil
		}
	}
	return nil, sale.ErrBookingNotFound
}

func (m *mockBookingStore) ListBookings(_ context.Context, _ uint64, status sale.BookingStatus) ([]*sale.Booking, error) {
	var out []*sale.Booking
	for _, b := range m.byID {
		if status == "" || b.Status == status {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *mockBookingStore) ListExpiredBookingIDs(_ context.Context, _ uint64, asOf time.Time) ([]uint64, error) {
	var ids []uint64
	for id, b := range m.byID {
		if b.Status == sale.BookingStatusActive && b.ExpiryDate.Before(asOf) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// bookingTestService: service dengan mock unit reader + accounts + booking store.
// Memakai helper fixture yang SUDAH ada di sale_test (newTestService pattern) —
// di sini cukup stub minimal.
type stubAccounts struct{ missing map[string]bool }

func (a *stubAccounts) FindAccountIDByCode(_ context.Context, _ uint64, code string) (uint64, error) {
	if a.missing[code] {
		return 0, sale.ErrPaymentAccountNotFound
	}
	// ID deterministik dari kode.
	return uint64(len(code)) + 100, nil
}
func (a *stubAccounts) ValidateCashBankAccount(_ context.Context, _ uint64, code string) error {
	if a.missing[code] {
		return sale.ErrPaymentAccountNotFound
	}
	return nil
}

type stubUnits struct{ status string }

func (u *stubUnits) FindUnitSaleInfo(_ context.Context, _ uint64, unitID uint64) (*sale.UnitSaleInfo, error) {
	if unitID == 0 {
		return nil, sale.ErrUnitNotFound
	}
	return &sale.UnitSaleInfo{ID: unitID, ProjectID: 7, Status: u.status}, nil
}

func newBookingTestService(store sale.BookingStore, unitStatus string, missing ...string) *sale.Service {
	acc := &stubAccounts{missing: map[string]bool{}}
	for _, m := range missing {
		acc.missing[m] = true
	}
	units := &stubUnits{status: unitStatus}
	return sale.NewService(acc, nil, units, nil, nil, nil,
		sale.WithBookingStore(store), sale.WithContractStore(stubContracts{}))
}

func validBookingReq() sale.CreateBookingRequest {
	return sale.CreateBookingRequest{
		UnitID:          10,
		CustomerID:      5,
		BookingFee:      domain.FromInt(5_000_000),
		BankAccountCode: "1-1300",
		BookingDate:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		ExpiryDate:      time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
	}
}

func TestCreateBooking_Valid(t *testing.T) {
	store := newMockBookingStore()
	svc := newBookingTestService(store, "available")
	b, err := svc.CreateBooking(context.Background(), 1, validBookingReq())
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	// Rule klien 2026-07-29: fee diakui Pendapatan Booking sejak diterima.
	if b.Status != sale.BookingStatusActive || b.FeeDisposition != sale.FeeRecognized {
		t.Errorf("status/disposisi awal salah: %s/%s (want active/recognized)", b.Status, b.FeeDisposition)
	}
	if b.ActiveKey == nil || *b.ActiveKey != "Y" {
		t.Error("ActiveKey harus 'Y' saat active")
	}
	if b.ProjectID != 7 {
		t.Errorf("ProjectID dari unit info: got %d", b.ProjectID)
	}
	// Jurnal: 2 baris, Dr bank = Cr titipan = fee (balanced by construction).
	if store.created == nil || len(store.created.JournalLines) != 2 {
		t.Fatalf("journal lines: %+v", store.created)
	}
	dr := store.created.JournalLines[0].Debit
	cr := store.created.JournalLines[1].Credit
	if dr.String() != "5000000" || cr.String() != "5000000" {
		t.Errorf("Dr/Cr = %s/%s, want 5000000/5000000", dr, cr)
	}
}

func TestCreateBooking_Validations(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*sale.CreateBookingRequest)
		unitSt  string
		missing []string
		wantErr error
	}{
		{"fee nol", func(r *sale.CreateBookingRequest) { r.BookingFee = domain.FromInt(0) }, "available", nil, sale.ErrBookingFeeInvalid},
		{"fee pecahan", func(r *sale.CreateBookingRequest) { r.BookingFee = domain.MustParse("100.5") }, "available", nil, sale.ErrBookingFeeInvalid},
		{"expiry <= booking date", func(r *sale.CreateBookingRequest) { r.ExpiryDate = r.BookingDate }, "available", nil, sale.ErrBookingExpiryInvalid},
		{"tanpa customer", func(r *sale.CreateBookingRequest) { r.CustomerID = 0 }, "available", nil, sale.ErrBookingCustomerRequired},
		{"unit tidak available", func(r *sale.CreateBookingRequest) {}, "reserved", nil, sale.ErrBookingUnitStateInvalid},
		{"akun pendapatan booking hilang", func(r *sale.CreateBookingRequest) {}, "available", []string{"4-2100"}, sale.ErrBookingRevenueAccountMissing},
	}
	for _, tc := range cases {
		store := newMockBookingStore()
		svc := newBookingTestService(store, tc.unitSt, tc.missing...)
		req := validBookingReq()
		tc.mutate(&req)
		_, err := svc.CreateBooking(context.Background(), 1, req)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.wantErr, err)
		}
		if store.created != nil {
			t.Errorf("%s: store tidak boleh tersentuh saat validasi gagal", tc.name)
		}
	}
}

func TestBooking_StoreNotConfigured(t *testing.T) {
	svc := sale.NewService(&stubAccounts{missing: map[string]bool{}}, nil, &stubUnits{status: "available"}, nil, nil, nil)
	if _, err := svc.CreateBooking(context.Background(), 1, validBookingReq()); !errors.Is(err, sale.ErrBookingStoreNotConfigured) {
		t.Fatalf("want ErrBookingStoreNotConfigured, got %v", err)
	}
}

func TestMarkExpiredBookings_SweepIdempotent(t *testing.T) {
	store := newMockBookingStore()
	svc := newBookingTestService(store, "available")
	// Dua booking: satu lewat expiry, satu belum.
	for i, exp := range []time.Time{
		time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	} {
		req := validBookingReq()
		req.UnitID = uint64(20 + i)
		req.ExpiryDate = exp
		if _, err := svc.CreateBooking(context.Background(), 1, req); err != nil {
			t.Fatalf("seed booking: %v", err)
		}
	}
	asOf := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	n, err := svc.MarkExpiredBookings(context.Background(), 1, asOf)
	if err != nil || n != 1 {
		t.Fatalf("sweep pertama: n=%d err=%v (want 1, nil)", n, err)
	}
	// Idempoten: sweep kedua tidak menutup apa pun.
	n, err = svc.MarkExpiredBookings(context.Background(), 1, asOf)
	if err != nil || n != 0 {
		t.Fatalf("sweep kedua: n=%d err=%v (want 0, nil)", n, err)
	}
}

// stubContracts: embed interface — method dipanggil = panic; pra-validasi
// booking harus gagal SEBELUM menyentuh ContractStore.
type stubContracts struct{ sale.ContractStore }

func TestConversion_Prevalidation(t *testing.T) {
	store := newMockBookingStore()
	// Unit status booked (pasca-booking) utk jalur konversi.
	svc := newBookingTestService(store, "booked")
	b, err := svc.CreateBooking(context.Background(), 1, func() sale.CreateBookingRequest {
		r := validBookingReq()
		return r
	}())
	// CreateBooking menolak unit 'booked' → buat langsung via store utk fixture.
	if err == nil {
		t.Fatalf("fixture: create harus gagal di unit booked, got booking %+v", b)
	}
	fixture := &sale.Booking{UnitID: 10, CustomerID: 5, Status: sale.BookingStatusActive, BookingFee: domain.FromInt(5_000_000)}
	store.byID[1] = fixture

	cust := uint64(5)
	wrongCust := uint64(6)
	cases := []struct {
		name    string
		req     sale.CreateContractRequest
		wantErr error
	}{
		{"unit mismatch", sale.CreateContractRequest{UnitID: 99, CustomerID: &cust}, sale.ErrBookingUnitMismatch},
		{"customer mismatch", sale.CreateContractRequest{UnitID: 10, CustomerID: &wrongCust}, sale.ErrBookingCustomerMismatch},
		{"customer nil", sale.CreateContractRequest{UnitID: 10}, sale.ErrBookingCustomerMismatch},
	}
	for _, tc := range cases {
		tc.req.PaymentType = sale.PaymentTypeTunai
		tc.req.TotalPrice = domain.FromInt(1_000_000_000)
		tc.req.ContractDate = time.Now()
		tc.req.BookingID = func() *uint64 { v := uint64(1); return &v }()
		_, err := svc.CreateContract(context.Background(), 1, tc.req)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.wantErr, err)
		}
	}
}
