package land

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// Handler wires the land_stock HTTP routes.
type Handler struct {
	svc *Service
}

// NewHandler membangun production handler yang terhubung ke GORM. HPP resolver
// = PurchasePriceLandHPPResolver (koreksi klien 2026-08-31): tarif HPP per m²
// langsung dari land_stock.purchase_price, bukan lagi alokasi RAB/actual —
// lihat hpp_resolver.go.
//
// pph adalah *tax.Service (satu-satunya tax engine — tidak ada engine kedua),
// sudah memenuhi LandPPhResolver langsung karena signature ResolveAccrualPlan
// identik. nil-safe: nil berarti tidak ada akrual PPh Final otomatis saat
// Akad (mundur-kompatibel dengan land_sales historis). Caller (cmd/api/main.go)
// mengoper taxHandler.Svc() yang sama dipakai unit/BAST — bukan instance baru.
func NewHandler(db *gorm.DB, pph LandPPhResolver) *Handler {
	repo := NewGORMRepository(db)
	resolver := NewPurchasePriceLandHPPResolver(repo)
	opts := []ServiceOption{WithHPPResolver(resolver)}
	if pph != nil {
		opts = append(opts, WithPPhResolver(pph))
	}
	return &Handler{svc: NewService(repo, opts...)}
}

// Svc exposes the underlying Service — wiring seams (e.g.
// reporting.SetLandReceivable) need it without importing HTTP routes.
func (h *Handler) Svc() *Service {
	return h.svc
}

// Mount registers land_stock + land_stock_reservations routes under
// /projects/{id}/land-stock. Same authorization tier as the pool itself
// (owner/accountant, RequireWrite) — Kelebihan Tanah is not exposed to
// marketing (tabs.ts: "tanah" is absent from MARKETING_TABS).
// The router must already have auth.Middleware applied upstream.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/projects/{id}/land-stock", func(r chi.Router) {
		r.Get("/", h.getPool)
		r.With(auth.RequireWrite()).Post("/", h.createPool)
		r.With(auth.RequireWrite()).Patch("/", h.updatePool)

		r.Route("/reservations", func(r chi.Router) {
			r.Get("/", h.listReservations)
			r.With(auth.RequireWrite()).Post("/", h.createReservation)
			r.Route("/{reservationID}", func(r chi.Router) {
				r.Get("/", h.getReservation)
				r.With(auth.RequireWrite()).Post("/cancel", h.cancelReservation)
			})
		})

		// LT-5 — Akad: pengakuan pendapatan+HPP satu penjualan Kelebihan
		// Tanah, atomik.
		r.With(auth.RequireWrite()).Post("/akad", h.recordAkad)
		r.Route("/land-sales", func(r chi.Router) {
			r.Get("/", h.listLandSales)
			r.Route("/{saleID}", func(r chi.Router) {
				r.Get("/", h.getLandSale)
				r.With(auth.RequireWrite()).Post("/cancel", h.cancelLandSale)
			})
		})
	})
	r.With(auth.RequireWrite()).Post("/land-stock/reservations/mark-expired", h.markExpiredReservations)
}

func writeLandJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeLandError(w http.ResponseWriter, status int, msg string) {
	writeLandJSON(w, status, map[string]string{"error": msg})
}

func tenantIDFrom(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func parseProjectIDParam(r *http.Request) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
}

func isDomainLandError(err error) bool {
	return errors.Is(err, ErrQuantityNegative) ||
		errors.Is(err, ErrCapacityExceeded) ||
		errors.Is(err, ErrLandStockAlreadyExists) ||
		errors.Is(err, ErrQuantityMustBePositive) ||
		errors.Is(err, ErrCustomerRequired) ||
		errors.Is(err, ErrReservationNotActive) ||
		errors.Is(err, ErrReservationQuantityMismatch) ||
		errors.Is(err, ErrReservationCustomerMismatch) ||
		errors.Is(err, ErrVATRateRequired) ||
		errors.Is(err, ErrPaymentAccountRequired) ||
		errors.Is(err, ErrRecognitionDateRequired) ||
		errors.Is(err, ErrInvalidBankAccount) ||
		errors.Is(err, ErrPaymentAccountInactive) ||
		errors.Is(err, ErrLandSaleNotAkad) ||
		errors.Is(err, ErrCancelReasonRequired) ||
		errors.Is(err, ErrLandSaleHasReceivedPayment)
}

func isNotFoundLandError(err error) bool {
	return errors.Is(err, ErrLandStockNotFound) ||
		errors.Is(err, ErrProjectNotFound) ||
		errors.Is(err, ErrCustomerNotFound) ||
		errors.Is(err, ErrReservationNotFound) ||
		errors.Is(err, ErrLandSaleNotFound) ||
		errors.Is(err, ErrPaymentAccountNotFound)
}

type poolDTO struct {
	TotalQuantityM2 string `json:"total_quantity_m2"`
	UnitPrice       string `json:"unit_price"`
	PurchasePrice   string `json:"purchase_price,omitempty"`
}

func (h *Handler) getPool(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	pool, err := h.svc.GetPool(r.Context(), tenantID, projectID)
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, pool)
}

func (h *Handler) createPool(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var dto poolDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	qty, price, purchasePrice, err := parsePoolDTO(dto)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, err.Error())
		return
	}
	pool, err := h.svc.CreatePool(r.Context(), tenantID, CreatePoolRequest{
		ProjectID: projectID, TotalQuantityM2: qty, UnitPrice: price, PurchasePrice: purchasePrice,
	})
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainLandError(err) {
			writeLandError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusCreated, pool)
}

func (h *Handler) updatePool(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var dto poolDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	qty, price, purchasePrice, err := parsePoolDTO(dto)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, err.Error())
		return
	}
	pool, err := h.svc.UpdatePool(r.Context(), tenantID, projectID, UpdatePoolRequest{
		TotalQuantityM2: qty, UnitPrice: price, PurchasePrice: purchasePrice,
	})
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainLandError(err) {
			writeLandError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, pool)
}

// ── Reservasi (LT-4) ────────────────────────────────────────────────────────

type reservationDTO struct {
	CustomerID    uint64  `json:"customer_id"`
	SalesPersonID *uint64 `json:"sales_person_id,omitempty"`
	QuantityM2    string  `json:"quantity_m2"`
	ReservedAt    string  `json:"reserved_at"`           // RFC3339
	ExpiryDate    *string `json:"expiry_date,omitempty"` // RFC3339
}

func parseReservationID(r *http.Request) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, "reservationID"), 10, 64)
}

func (h *Handler) createReservation(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var dto reservationDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	qty, err := decimal.NewFromString(dto.QuantityM2)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "quantity_m2 harus angka")
		return
	}
	reservedAt, err := time.Parse(time.RFC3339, dto.ReservedAt)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "reserved_at format tidak valid (gunakan RFC3339)")
		return
	}
	var expiry *time.Time
	if dto.ExpiryDate != nil && *dto.ExpiryDate != "" {
		t, err := time.Parse(time.RFC3339, *dto.ExpiryDate)
		if err != nil {
			writeLandError(w, http.StatusBadRequest, "expiry_date format tidak valid (gunakan RFC3339)")
			return
		}
		expiry = &t
	}
	var createdBy *uint64
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		createdBy = &uid
	}

	res, err := h.svc.Reserve(r.Context(), tenantID, ReserveRequest{
		ProjectID:     projectID,
		CustomerID:    dto.CustomerID,
		SalesPersonID: dto.SalesPersonID,
		QuantityM2:    qty,
		ReservedAt:    reservedAt,
		ExpiryDate:    expiry,
		CreatedBy:     createdBy,
	})
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainLandError(err) {
			writeLandError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusCreated, res)
}

func (h *Handler) listReservations(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	list, err := h.svc.ListReservations(r.Context(), tenantID, projectID)
	if err != nil {
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, list)
}

func (h *Handler) getReservation(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseReservationID(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}
	res, err := h.svc.GetReservation(r.Context(), tenantID, id)
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, res)
}

func (h *Handler) cancelReservation(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseReservationID(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid reservation id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	res, err := h.svc.CancelReservation(r.Context(), tenantID, id, body.Reason)
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainLandError(err) {
			writeLandError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, res)
}

func (h *Handler) markExpiredReservations(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	count, err := h.svc.MarkExpiredReservations(r.Context(), tenantID, time.Now())
	if err != nil {
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, map[string]int{"expired": count})
}

// ── Akad (LT-5) ──────────────────────────────────────────────────────────

type akadDTO struct {
	ReservationID      *uint64 `json:"reservation_id,omitempty"`
	CustomerID         uint64  `json:"customer_id"`
	SalesPersonID      *uint64 `json:"sales_person_id,omitempty"`
	QuantityM2         string  `json:"quantity_m2"`
	UnitPriceSnapshot  string  `json:"unit_price_snapshot,omitempty"`
	DPPAmount          string  `json:"dpp_amount"`
	IsPKP              bool    `json:"is_pkp"`
	VATRateSnapshot    string  `json:"vat_rate_snapshot,omitempty"`
	PaymentAccountCode string  `json:"payment_account_code"`
	RecognitionDate    string  `json:"recognition_date"` // RFC3339
}

func parseSaleIDParam(r *http.Request) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, "saleID"), 10, 64)
}

func (h *Handler) recordAkad(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var dto akadDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	qty, err := decimal.NewFromString(dto.QuantityM2)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "quantity_m2 harus angka")
		return
	}
	dpp, err := domain.NewMoney(dto.DPPAmount)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "dpp_amount tidak valid: "+err.Error())
		return
	}
	var unitPrice domain.Money
	if dto.UnitPriceSnapshot != "" {
		unitPrice, err = domain.NewMoney(dto.UnitPriceSnapshot)
		if err != nil {
			writeLandError(w, http.StatusBadRequest, "unit_price_snapshot tidak valid: "+err.Error())
			return
		}
	}
	var vatRate decimal.Decimal
	if dto.VATRateSnapshot != "" {
		vatRate, err = decimal.NewFromString(dto.VATRateSnapshot)
		if err != nil {
			writeLandError(w, http.StatusBadRequest, "vat_rate_snapshot harus angka")
			return
		}
	}
	recognitionDate, err := time.Parse(time.RFC3339, dto.RecognitionDate)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "recognition_date format tidak valid (gunakan RFC3339)")
		return
	}
	var createdBy *uint64
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		createdBy = &uid
	}

	sale, err := h.svc.RecordAkad(r.Context(), tenantID, RecordAkadRequest{
		ProjectID:          projectID,
		ReservationID:      dto.ReservationID,
		CustomerID:         dto.CustomerID,
		SalesPersonID:      dto.SalesPersonID,
		QuantityM2:         qty,
		UnitPriceSnapshot:  unitPrice,
		DPPAmount:          dpp,
		IsPKP:              dto.IsPKP,
		VATRateSnapshot:    vatRate,
		PaymentAccountCode: dto.PaymentAccountCode,
		RecognitionDate:    recognitionDate,
		CreatedBy:          createdBy,
	})
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainLandError(err) {
			writeLandError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusCreated, sale)
}

func (h *Handler) listLandSales(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseProjectIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	list, err := h.svc.ListLandSales(r.Context(), tenantID, projectID)
	if err != nil {
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, list)
}

func (h *Handler) getLandSale(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSaleIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid land_sale id")
		return
	}
	sale, err := h.svc.GetLandSale(r.Context(), tenantID, id)
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, sale)
}

func (h *Handler) cancelLandSale(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantIDFrom(r)
	if err != nil {
		writeLandError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseSaleIDParam(r)
	if err != nil {
		writeLandError(w, http.StatusBadRequest, "invalid land_sale id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	var cancelledBy *uint64
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		cancelledBy = &uid
	}

	sale, err := h.svc.CancelLandSale(r.Context(), tenantID, id, CancelLandSaleRequest{
		Reason:      body.Reason,
		CancelledBy: cancelledBy,
		CancelDate:  time.Now(),
	})
	if err != nil {
		if isNotFoundLandError(err) {
			writeLandError(w, http.StatusNotFound, err.Error())
			return
		}
		if isDomainLandError(err) {
			writeLandError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeLandError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLandJSON(w, http.StatusOK, sale)
}

func parsePoolDTO(dto poolDTO) (qty decimal.Decimal, price, purchasePrice domain.Money, err error) {
	if dto.TotalQuantityM2 != "" {
		qty, err = decimal.NewFromString(dto.TotalQuantityM2)
		if err != nil {
			return qty, price, purchasePrice, errors.New("total_quantity_m2 harus angka")
		}
	}
	if dto.UnitPrice != "" {
		price, err = domain.NewMoney(dto.UnitPrice)
		if err != nil {
			return qty, price, purchasePrice, errors.New("unit_price tidak valid: " + err.Error())
		}
	}
	if dto.PurchasePrice != "" {
		purchasePrice, err = domain.NewMoney(dto.PurchasePrice)
		if err != nil {
			return qty, price, purchasePrice, errors.New("purchase_price tidak valid: " + err.Error())
		}
	}
	return qty, price, purchasePrice, nil
}
