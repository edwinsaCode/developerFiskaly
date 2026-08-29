package sale

// HTTP surface Increment 3 — Payment Scheme pada kontrak:
//   GET  /sale-contracts/{contractID}/schedule-plan    → usulan jadwal dari policy
//   POST /sale-contracts/{contractID}/scheme-events    → financing milestone manual
//   POST /sale-contracts/{contractID}/scheme-convert   → konversi scheme (bank_rejected)
//   POST /sale-contracts/{contractID}/schedule-regenerate → reschedule (supersede)
//   GET  /sale-contracts/{contractID}/payment-events   → audit trail lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"esaproperti/internal/customer"
	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/salesorg"
	"esaproperti/internal/scheme"
)

// ── PartyLookup adapter (customer + salesorg → sale, hindari import cycle) ────

type partyLookupAdapter struct {
	customers *customer.GORMRepository
	persons   *salesorg.GORMRepository
}

func (a *partyLookupAdapter) CustomerExists(ctx context.Context, tenantID, id uint64) error {
	_, err := a.customers.FindByID(ctx, tenantID, id)
	return err
}

func (a *partyLookupAdapter) SalesPersonExists(ctx context.Context, tenantID, id uint64) error {
	_, err := a.persons.FindPersonByID(ctx, tenantID, id)
	return err
}

// ── Error mapping bersama endpoint scheme ─────────────────────────────────────

func writeSchemeFlowError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrContractNotFound),
		errors.Is(err, scheme.ErrSchemeNotFound),
		errors.Is(err, scheme.ErrFinSourceNotFound):
		writeSaleError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, scheme.ErrInvalidTransition):
		writeSaleError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrContractHasNoScheme),
		errors.Is(err, ErrSchemeEventNotManual),
		errors.Is(err, ErrFinancingSourceRequiredForTakeover),
		errors.Is(err, ErrScheduleSumMismatch),
		errors.Is(err, ErrScheduleItemsRequired),
		errors.Is(err, ErrActiveScheduleExists),
		errors.Is(err, scheme.ErrInvalidParams),
		errors.Is(err, scheme.ErrSchemeInactive),
		errors.Is(err, scheme.ErrFinancingSourceRequired),
		errors.Is(err, scheme.ErrFinancingSourceNotAllowed):
		writeSaleError(w, http.StatusBadRequest, err.Error())
	default:
		writeSaleError(w, http.StatusInternalServerError, err.Error())
	}
}

func saleCreatedBy(r *http.Request) *uint64 {
	if uid, ok := auth.UserIDFrom(r.Context()); ok && uid != 0 {
		return &uid
	}
	return nil
}

// ── GET /sale-contracts/{contractID}/schedule-plan ────────────────────────────

func (h *Handler) getSchedulePlan(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	items, err := h.svc.GetSchedulePlan(r.Context(), tenantID, contractID)
	if err != nil {
		writeSchemeFlowError(w, err)
		return
	}
	type planRow struct {
		InstallmentNumber int    `json:"installment_number"`
		DueDate           string `json:"due_date"`
		Amount            string `json:"amount"`
		Type              string `json:"type"`
	}
	rows := make([]planRow, len(items))
	for i, it := range items {
		rows[i] = planRow{
			InstallmentNumber: it.InstallmentNumber,
			DueDate:           it.DueDate.Format(time.RFC3339),
			Amount:            it.Amount.String(),
			Type:              string(it.Type),
		}
	}
	writeSaleJSON(w, http.StatusOK, map[string]any{"items": rows})
}

// ── POST /sale-contracts/{contractID}/scheme-events ───────────────────────────

type schemeEventBody struct {
	Event             string  `json:"event"` // submitted_to_bank|bank_approved|bank_rejected|akad|disbursed|takeover
	EventDate         string  `json:"event_date,omitempty"` // RFC3339; kosong = sekarang (tanggal kejadian bisnis)
	FinancingSourceID *uint64 `json:"financing_source_id,omitempty"`
	Notes             string  `json:"notes,omitempty"`
}

// parseOptionalEventDate mem-parse tanggal kejadian bisnis (RFC3339, opsional).
func parseOptionalEventDate(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, true
	}
	d, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}

func (h *Handler) applySchemeEvent(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	var body schemeEventBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	eventDate, ok := parseOptionalEventDate(body.EventDate)
	if !ok {
		writeSaleError(w, http.StatusBadRequest, "event_date tidak valid (gunakan RFC3339)")
		return
	}
	ev, err := h.svc.ApplySchemeEvent(r.Context(), tenantID, ApplySchemeEventRequest{
		ContractID:        contractID,
		Event:             scheme.Event(body.Event),
		EventDate:         eventDate,
		FinancingSourceID: body.FinancingSourceID,
		Notes:             body.Notes,
		CreatedBy:         saleCreatedBy(r),
	})
	if err != nil {
		writeSchemeFlowError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusCreated, ev)
}

// ── POST /sale-contracts/{contractID}/scheme-convert ──────────────────────────

type schemeConvertBody struct {
	NewSchemeID       uint64  `json:"new_scheme_id"`
	EventDate         string  `json:"event_date,omitempty"` // RFC3339; kosong = sekarang
	FinancingSourceID *uint64 `json:"financing_source_id,omitempty"`
	Notes             string  `json:"notes,omitempty"`
}

func (h *Handler) convertScheme(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	var body schemeConvertBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	convDate, ok := parseOptionalEventDate(body.EventDate)
	if !ok {
		writeSaleError(w, http.StatusBadRequest, "event_date tidak valid (gunakan RFC3339)")
		return
	}
	ev, err := h.svc.ConvertScheme(r.Context(), tenantID, ConvertSchemeRequest{
		ContractID:        contractID,
		NewSchemeID:       body.NewSchemeID,
		EventDate:         convDate,
		FinancingSourceID: body.FinancingSourceID,
		Notes:             body.Notes,
		CreatedBy:         saleCreatedBy(r),
	})
	if err != nil {
		writeSchemeFlowError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusCreated, ev)
}

// ── POST /sale-contracts/{contractID}/schedule-regenerate ─────────────────────

type regenerateScheduleBody struct {
	Items     []scheduleItemBody `json:"items"`
	EventDate string             `json:"event_date,omitempty"` // RFC3339; kosong = sekarang
	Notes     string             `json:"notes,omitempty"`
}

func (h *Handler) regenerateSchedule(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	var body regenerateScheduleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeSaleError(w, http.StatusBadRequest, "request body tidak valid")
		return
	}
	items := make([]ScheduleItem, 0, len(body.Items))
	for i, b := range body.Items {
		amount, err := domain.NewMoney(b.Amount)
		if err != nil {
			writeSaleError(w, http.StatusBadRequest, fmt.Sprintf("item[%d].amount tidak valid: %v", i, err))
			return
		}
		dueDate, err := time.Parse(time.RFC3339, b.DueDate)
		if err != nil {
			writeSaleError(w, http.StatusBadRequest, fmt.Sprintf("item[%d].due_date tidak valid: %v", i, err))
			return
		}
		items = append(items, ScheduleItem{
			InstallmentNumber: b.InstallmentNumber,
			DueDate:           dueDate,
			Amount:            amount,
			Type:              ScheduleType(b.Type),
		})
	}
	reschedDate, ok := parseOptionalEventDate(body.EventDate)
	if !ok {
		writeSaleError(w, http.StatusBadRequest, "event_date tidak valid (gunakan RFC3339)")
		return
	}
	rows, err := h.svc.RegenerateSchedule(r.Context(), tenantID, RegenerateScheduleRequest{
		ContractID: contractID,
		Items:      items,
		EventDate:  reschedDate,
		Notes:      body.Notes,
		CreatedBy:  saleCreatedBy(r),
	})
	if err != nil {
		writeSchemeFlowError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusCreated, rows)
}

// ── GET /sale-contracts/{contractID}/payment-events ───────────────────────────

func (h *Handler) listPaymentEvents(w http.ResponseWriter, r *http.Request) {
	tenantID, err := saleTenantID(r)
	if err != nil {
		writeSaleError(w, http.StatusUnauthorized, err.Error())
		return
	}
	contractID, err := parseSaleUintParam(r, "contractID")
	if err != nil {
		writeSaleError(w, http.StatusBadRequest, "contractID tidak valid")
		return
	}
	events, err := h.svc.ListPaymentEvents(r.Context(), tenantID, contractID)
	if err != nil {
		writeSchemeFlowError(w, err)
		return
	}
	writeSaleJSON(w, http.StatusOK, map[string]any{"data": events})
}
