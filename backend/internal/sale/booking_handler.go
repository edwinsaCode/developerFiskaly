package sale

// Increment 7 — Booking: HTTP handlers. Tanpa business logic (konvensi package).

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/platform/auth"
)

type createBookingDTO struct {
	CustomerID      uint64  `json:"customer_id"`
	SalesPersonID   *uint64 `json:"sales_person_id,omitempty"`
	LeadID          *uint64 `json:"lead_id,omitempty"`
	BookingFee      string  `json:"booking_fee"`
	Refundable      bool    `json:"refundable"`
	BankAccountCode string  `json:"bank_account_code"`
	BookingDate     string  `json:"booking_date"` // YYYY-MM-DD; kosong = hari ini
	ExpiryDate      string  `json:"expiry_date"`  // YYYY-MM-DD (wajib)
	Notes           string  `json:"notes,omitempty"`

	// LandQuantityM2 (kelebihan-tanah-booking-integration-2026-08): komponen
	// opsional Produk Tambahan Kelebihan Tanah — salesperson HANYA mengisi
	// quantity (m²); harga dan reservasi diselesaikan server-side. String
	// kosong/absent = booking tanpa komponen tanah.
	LandQuantityM2 string `json:"land_quantity_m2,omitempty"`
}

func (h *Handler) createBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseSaleUintParam(r, "unitID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}
	var dto createBookingDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeSaleError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	fee, err := domain.NewMoney(dto.BookingFee)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "booking_fee tidak valid: "+err.Error())
		return
	}
	bookingDate := time.Now()
	if dto.BookingDate != "" {
		t, perr := time.Parse("2006-01-02", dto.BookingDate)
		if perr != nil {
			writeSaleError(w, http.StatusBadRequest, "booking_date harus YYYY-MM-DD")
			return
		}
		bookingDate = t
	}
	if dto.ExpiryDate == "" {
		writeSaleError(w, http.StatusBadRequest, "expiry_date wajib diisi (YYYY-MM-DD)")
		return
	}
	expiry, err := time.Parse("2006-01-02", dto.ExpiryDate)
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "expiry_date harus YYYY-MM-DD")
		return
	}

	req := CreateBookingRequest{
		UnitID:          unitID,
		CustomerID:      dto.CustomerID,
		SalesPersonID:   dto.SalesPersonID,
		LeadID:          dto.LeadID,
		BookingFee:      fee,
		Refundable:      dto.Refundable,
		BankAccountCode: dto.BankAccountCode,
		BookingDate:     bookingDate,
		ExpiryDate:      expiry,
		Notes:           dto.Notes,
	}
	if dto.LandQuantityM2 != "" {
		qty, qerr := decimal.NewFromString(dto.LandQuantityM2)
		if qerr != nil {
			writeSaleError(w, http.StatusBadRequest, "land_quantity_m2 tidak valid: "+qerr.Error())
			return
		}
		req.LandQuantityM2 = &qty
	}
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		req.CreatedBy = &uid
	}

	b, err := h.svc.CreateBooking(r.Context(), tenantID, req)
	if err != nil {
		writeBookingError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusCreated, b)
}

func (h *Handler) getBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSaleUintParam(r, "bookingID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "bookingID tidak valid")
		return
	}
	b, err := h.svc.GetBooking(r.Context(), tenantID, id)
	if err != nil {
		writeBookingError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusOK, b)
}

func (h *Handler) getActiveBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseSaleUintParam(r, "unitID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "unitID tidak valid")
		return
	}
	b, err := h.svc.GetActiveBookingByUnit(r.Context(), tenantID, unitID)
	if err != nil {
		writeBookingError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusOK, b)
}

func (h *Handler) listBookings(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	status := BookingStatus(r.URL.Query().Get("status"))
	bs, err := h.svc.ListBookings(r.Context(), tenantID, status)
	if err != nil {
		writeBookingError(w, err)
		return
	}
	if bs == nil {
		bs = []*Booking{}
	}
	writeSaleJSON(w, http.StatusOK, bs)
}

type cancelBookingDTO struct {
	Reason    string `json:"reason"`
	EventDate string `json:"event_date,omitempty"` // YYYY-MM-DD; kosong = hari ini
}

func (h *Handler) cancelBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSaleUintParam(r, "bookingID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "bookingID tidak valid")
		return
	}
	var dto cancelBookingDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeSaleError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	eventDate := time.Now()
	if dto.EventDate != "" {
		t, perr := time.Parse("2006-01-02", dto.EventDate)
		if perr != nil {
			writeSaleError(w, http.StatusBadRequest, "event_date harus YYYY-MM-DD")
			return
		}
		eventDate = t
	}
	var actor *uint64
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		actor = &uid
	}
	b, err := h.svc.CancelBooking(r.Context(), tenantID, id, dto.Reason, eventDate, actor)
	if err != nil {
		writeBookingError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusOK, b)
}

func (h *Handler) markExpiredBookings(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	asOf := time.Now()
	if q := r.URL.Query().Get("as_of"); q != "" {
		t, perr := time.Parse("2006-01-02", q)
		if perr != nil {
			writeSaleError(w, http.StatusBadRequest, "as_of harus YYYY-MM-DD")
			return
		}
		asOf = t
	}
	count, err := h.svc.MarkExpiredBookings(r.Context(), tenantID, asOf)
	if err != nil {
		writeBookingError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusOK, map[string]int{"expired": count})
}

// feeDispositionDTO (R4 Opsi A): aksi disposisi fee outside-price.
type feeDispositionDTO struct {
	Action    string `json:"action"` // forfeit | refund
	Reason    string `json:"reason,omitempty"`
	EventDate string `json:"event_date,omitempty"` // YYYY-MM-DD; kosong = hari ini
}

// disposeBookingFee — POST /bookings/{bookingID}/fee-disposition.
func (h *Handler) disposeBookingFee(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSaleUintParam(r, "bookingID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "bookingID tidak valid")
		return
	}
	var dto feeDispositionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeSaleError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	eventDate := time.Now()
	if dto.EventDate != "" {
		t, perr := time.Parse("2006-01-02", dto.EventDate)
		if perr != nil {
			writeSaleError(w, http.StatusBadRequest, "event_date harus YYYY-MM-DD")
			return
		}
		eventDate = t
	}
	var actor *uint64
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		actor = &uid
	}
	b, err := h.svc.DisposeConvertedBookingFee(r.Context(), tenantID, id, dto.Action, dto.Reason, eventDate, actor)
	if err != nil {
		writeBookingError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusOK, b)
}

// writeBookingError memetakan error booking ke status HTTP.
func writeBookingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBookingNotFound), errors.Is(err, ErrUnitNotFound):
		writeSaleError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrActiveBookingExists), errors.Is(err, ErrBookingNotActive),
		errors.Is(err, ErrFeeNotDisposable):
		writeSaleError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidFeeDispositionAction):
		writeSaleError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrBookingFeeInvalid), errors.Is(err, ErrBookingExpiryInvalid),
		errors.Is(err, ErrBookingCustomerRequired), errors.Is(err, ErrUnitRequired),
		errors.Is(err, ErrInvalidBankAccount), errors.Is(err, ErrPaymentAccountNotFound),
		errors.Is(err, ErrPaymentAccountInactive), errors.Is(err, ErrLandQuantityInvalid):
		writeSaleError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrBookingUnitStateInvalid), errors.Is(err, ErrBookingUnitMismatch),
		errors.Is(err, ErrBookingCustomerMismatch), errors.Is(err, ErrTitipanAccountMissing):
		writeSaleError(w, http.StatusUnprocessableEntity, err.Error())
	// kelebihan-tanah-booking-integration-2026-08: reservasi Produk Tambahan
	// Kelebihan Tanah gagal — proyek belum punya pool, atau kuantitas melebihi
	// yang tersedia (race dengan booking lain, ditangkap oleh row lock
	// land.ReserveTx). Keduanya kegagalan permintaan, bukan bug server.
	case errors.Is(err, land.ErrLandStockNotFound):
		writeSaleError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, land.ErrCapacityExceeded):
		writeSaleError(w, http.StatusConflict, err.Error())
	default:
		writeSaleError(w, http.StatusInternalServerError, err.Error())
	}
}
