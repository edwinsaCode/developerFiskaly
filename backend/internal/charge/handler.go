package charge

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/sale"
)

// Handler — HTTP untuk Billing Batch 2 (Charge Group).
type Handler struct {
	svc *Service
}

func NewHandler(db *gorm.DB) *Handler { return &Handler{svc: NewService(db)} }

// Svc mengekspos service untuk wiring (adapter di main).
func (h *Handler) Svc() *Service { return h.svc }

func (h *Handler) Mount(r chi.Router) {
	r.Route("/charges", func(r chi.Router) {
		r.Get("/groups", h.listGroups) // ?contract_id= | ?unit_id=
		r.Get("/groups/{groupID}", h.groupDetail)
		r.Get("/portfolio", h.portfolio)                          // KPI dashboard (terpisah dari KPI rumah)
		r.Get("/aging", h.aging)                                  // aging realisasi (mesin BuildARAging yang sama)
		r.Get("/policy/history", h.policyHistory)                 // R-A: audit perubahan kebijakan
		r.Get("/settlements/{settlementID}/memo", h.transferMemo) // T-1: memo transfer internal
		r.Get("/types", h.listChargeTypes)                        // W-1: master jenis biaya realisasi
		r.Get("/types/history", h.masterHistory)                  // TD-8: audit perubahan master

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireWrite())
			r.Post("/groups", h.createGroup)
			r.Post("/groups/{groupID}/items", h.addItems)
			r.Post("/groups/{groupID}/payments", h.receivePayment)
			r.Post("/groups/{groupID}/refund", h.refund)
			r.Post("/groups/{groupID}/transfer-house", h.transferHouse)
			r.Post("/groups/{groupID}/transfer-group", h.transferGroup)
			r.Post("/groups/{groupID}/settle", h.settle)
			r.Post("/groups/{groupID}/cancel", h.cancelGroup)
			r.Post("/groups/{groupID}/invoice", h.issueInvoice)
			r.Post("/items/{itemID}/payouts", h.recordPayout)
			r.Post("/items/{itemID}/cancel", h.cancelItem)
			r.Post("/payments/{terminID}/void", h.voidPayment)
			r.Post("/types", h.createChargeType) // W-1: admin kelola sendiri
			r.Patch("/types/{typeID}", h.updateChargeType)
		})
	})
}

// ── DTO ──────────────────────────────────────────────────────────────────────

type itemDTO struct {
	Label          string `json:"label"`
	ChargeTypeCode string `json:"charge_type_code,omitempty"` // W-1: wajib utk grup realization
	ProductCode    string `json:"product_code,omitempty"`     // W-13: wajib utk grup addon
	Amount         string `json:"amount"`
	DueDate        string `json:"due_date,omitempty"` // YYYY-MM-DD
}

func (d itemDTO) toInput() (NewItemInput, error) {
	amount, err := domain.NewMoney(d.Amount)
	if err != nil {
		return NewItemInput{}, err
	}
	var due *time.Time
	if d.DueDate != "" {
		t, terr := time.Parse("2006-01-02", d.DueDate)
		if terr != nil {
			return NewItemInput{}, terr
		}
		due = &t
	}
	return NewItemInput{
		Label:          d.Label,
		ChargeTypeCode: d.ChargeTypeCode,
		ProductCode:    d.ProductCode,
		Amount:         amount,
		DueDate:        due,
	}, nil
}

type createGroupDTO struct {
	SaleContractID uint64    `json:"sale_contract_id"`
	Kind           string    `json:"kind"`
	Label          string    `json:"label"`
	Notes          string    `json:"notes,omitempty"`
	Items          []itemDTO `json:"items"`
}

type allocationDTO struct {
	ChargeItemID uint64 `json:"charge_item_id"`
	Amount       string `json:"amount"`
}

func parseAllocations(in []allocationDTO) ([]AllocationInput, error) {
	out := make([]AllocationInput, 0, len(in))
	for _, a := range in {
		amount, err := domain.NewMoney(a.Amount)
		if err != nil {
			return nil, err
		}
		out = append(out, AllocationInput{ChargeItemID: a.ChargeItemID, Amount: amount})
	}
	return out, nil
}

type paymentDTO struct {
	Amount          string          `json:"amount"`
	Date            string          `json:"date,omitempty"`
	BankAccountCode string          `json:"bank_account_code"`
	Allocations     []allocationDTO `json:"allocations"`
	Reference       string          `json:"reference,omitempty"`
	Notes           string          `json:"notes,omitempty"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
}

type payoutDTO struct {
	Amount          string `json:"amount"`
	Date            string `json:"date,omitempty"`
	BankAccountCode string `json:"bank_account_code"`
	Vendor          string `json:"vendor"`
	Notes           string `json:"notes,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

type refundDTO struct {
	Amount          string `json:"amount"`
	Date            string `json:"date,omitempty"`
	BankAccountCode string `json:"bank_account_code"`
	Notes           string `json:"notes,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

type transferHouseDTO struct {
	Amount         string `json:"amount"`
	Date           string `json:"date,omitempty"`
	Notes          string `json:"notes,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type transferGroupDTO struct {
	TargetGroupID  uint64          `json:"target_group_id"`
	Amount         string          `json:"amount"`
	Date           string          `json:"date,omitempty"`
	Allocations    []allocationDTO `json:"allocations"`
	Notes          string          `json:"notes,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

type voidDTO struct {
	Reason string `json:"reason"`
}

type invoiceDTO struct {
	DueDate string `json:"due_date,omitempty"` // YYYY-MM-DD
	Notes   string `json:"notes,omitempty"`
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func ctxIDs(w http.ResponseWriter, r *http.Request) (tenantID uint64, actor *uint64, ok bool) {
	tenantID, ok = auth.TenantIDFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "missing authentication context")
		return 0, nil, false
	}
	if uid, uok := auth.UserIDFrom(r.Context()); uok {
		actor = &uid
	}
	return tenantID, actor, true
}

func urlID(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, name), 10, 64)
	if err != nil || id == 0 {
		writeErr(w, http.StatusBadRequest, name+" tidak valid")
		return 0, false
	}
	return id, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func parseMoney(w http.ResponseWriter, s string) (domain.Money, bool) {
	m, err := domain.NewMoney(s)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "amount tidak valid: "+err.Error())
		return domain.Zero, false
	}
	return m, true
}

func parseDateOpt(w http.ResponseWriter, s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, true
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "tanggal harus YYYY-MM-DD")
		return time.Time{}, false
	}
	return t, true
}

// ── Endpoints ────────────────────────────────────────────────────────────────

func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if cid := q.Get("contract_id"); cid != "" {
		id, err := strconv.ParseUint(cid, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "contract_id tidak valid")
			return
		}
		out, err := h.svc.ListGroupsByContract(r.Context(), tenantID, id)
		if err != nil {
			writeSvcErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if uid := q.Get("unit_id"); uid != "" {
		id, err := strconv.ParseUint(uid, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "unit_id tidak valid")
			return
		}
		out, err := h.svc.ListGroupsByUnit(r.Context(), tenantID, id)
		if err != nil {
			writeSvcErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeErr(w, http.StatusBadRequest, "wajib menyertakan contract_id atau unit_id")
}

func (h *Handler) groupDetail(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	id, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	out, err := h.svc.GroupDetail(r.Context(), tenantID, id)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) portfolio(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	out, err := h.svc.Portfolio(r.Context(), tenantID)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) aging(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	asOf := time.Now()
	if s := r.URL.Query().Get("as_of"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "as_of harus YYYY-MM-DD")
			return
		}
		asOf = t
	}
	out, err := h.svc.AgingReport(r.Context(), tenantID, asOf)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	var dto createGroupDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	items := make([]NewItemInput, 0, len(dto.Items))
	for _, it := range dto.Items {
		in, err := it.toInput()
		if err != nil {
			writeErr(w, http.StatusBadRequest, "item tidak valid: "+err.Error())
			return
		}
		items = append(items, in)
	}
	out, err := h.svc.CreateGroup(r.Context(), tenantID, CreateGroupRequest{
		SaleContractID: dto.SaleContractID,
		Kind:           GroupKind(dto.Kind),
		Label:          dto.Label,
		Notes:          dto.Notes,
		Items:          items,
		CreatedBy:      actor,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) addItems(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	groupID, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	var dto struct {
		Items []itemDTO `json:"items"`
	}
	if !decodeBody(w, r, &dto) {
		return
	}
	items := make([]NewItemInput, 0, len(dto.Items))
	for _, it := range dto.Items {
		in, err := it.toInput()
		if err != nil {
			writeErr(w, http.StatusBadRequest, "item tidak valid: "+err.Error())
			return
		}
		items = append(items, in)
	}
	out, err := h.svc.AddItems(r.Context(), tenantID, groupID, items, actor)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) receivePayment(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	groupID, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	var dto paymentDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	amount, ok := parseMoney(w, dto.Amount)
	if !ok {
		return
	}
	date, ok := parseDateOpt(w, dto.Date)
	if !ok {
		return
	}
	allocs, err := parseAllocations(dto.Allocations)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "alokasi tidak valid: "+err.Error())
		return
	}
	out, err := h.svc.ReceivePayment(r.Context(), tenantID, ReceivePaymentRequest{
		GroupID:         groupID,
		Amount:          amount,
		Date:            date,
		BankAccountCode: dto.BankAccountCode,
		Allocations:     allocs,
		Reference:       dto.Reference,
		Notes:           dto.Notes,
		CreatedBy:       actor,
		IdempotencyKey:  dto.IdempotencyKey,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) recordPayout(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	itemID, ok := urlID(w, r, "itemID")
	if !ok {
		return
	}
	var dto payoutDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	amount, ok := parseMoney(w, dto.Amount)
	if !ok {
		return
	}
	date, ok := parseDateOpt(w, dto.Date)
	if !ok {
		return
	}
	out, err := h.svc.RecordPayout(r.Context(), tenantID, PayoutRequest{
		ChargeItemID:    itemID,
		Amount:          amount,
		Date:            date,
		BankAccountCode: dto.BankAccountCode,
		Vendor:          dto.Vendor,
		Notes:           dto.Notes,
		CreatedBy:       actor,
		IdempotencyKey:  dto.IdempotencyKey,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) refund(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	groupID, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	var dto refundDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	amount, ok := parseMoney(w, dto.Amount)
	if !ok {
		return
	}
	date, ok := parseDateOpt(w, dto.Date)
	if !ok {
		return
	}
	out, err := h.svc.Refund(r.Context(), tenantID, RefundRequest{
		GroupID:         groupID,
		Amount:          amount,
		Date:            date,
		BankAccountCode: dto.BankAccountCode,
		Notes:           dto.Notes,
		CreatedBy:       actor,
		IdempotencyKey:  dto.IdempotencyKey,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) transferHouse(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	groupID, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	var dto transferHouseDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	amount, ok := parseMoney(w, dto.Amount)
	if !ok {
		return
	}
	date, ok := parseDateOpt(w, dto.Date)
	if !ok {
		return
	}
	out, err := h.svc.TransferToHouse(r.Context(), tenantID, TransferHouseRequest{
		GroupID:        groupID,
		Amount:         amount,
		Date:           date,
		Notes:          dto.Notes,
		CreatedBy:      actor,
		IdempotencyKey: dto.IdempotencyKey,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) transferGroup(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	groupID, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	var dto transferGroupDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	amount, ok := parseMoney(w, dto.Amount)
	if !ok {
		return
	}
	date, ok := parseDateOpt(w, dto.Date)
	if !ok {
		return
	}
	allocs, err := parseAllocations(dto.Allocations)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "alokasi tidak valid: "+err.Error())
		return
	}
	out, err := h.svc.TransferToGroup(r.Context(), tenantID, TransferGroupRequest{
		GroupID:        groupID,
		TargetGroupID:  dto.TargetGroupID,
		Amount:         amount,
		Date:           date,
		Allocations:    allocs,
		Notes:          dto.Notes,
		CreatedBy:      actor,
		IdempotencyKey: dto.IdempotencyKey,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) settle(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	groupID, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	out, err := h.svc.TrueUpAndSettle(r.Context(), tenantID, groupID, actor)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) cancelGroup(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	groupID, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	if err := h.svc.CancelGroup(r.Context(), tenantID, groupID); err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handler) cancelItem(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	itemID, ok := urlID(w, r, "itemID")
	if !ok {
		return
	}
	out, err := h.svc.CancelItem(r.Context(), tenantID, itemID)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) voidPayment(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	terminID, ok := urlID(w, r, "terminID")
	if !ok {
		return
	}
	var dto voidDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	if dto.Reason == "" {
		writeErr(w, http.StatusBadRequest, "alasan void wajib diisi")
		return
	}
	out, err := h.svc.VoidPayment(r.Context(), tenantID, VoidPaymentRequest{
		TerminID:  terminID,
		Reason:    dto.Reason,
		CreatedBy: actor,
	})
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) issueInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	groupID, ok := urlID(w, r, "groupID")
	if !ok {
		return
	}
	var dto invoiceDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	due, ok := parseDateOpt(w, dto.DueDate)
	if !ok {
		return
	}
	var actorID uint64
	if actor != nil {
		actorID = *actor
	}
	invoiceID, invoiceNumber, recognized, err := h.svc.IssueInvoice(r.Context(), tenantID, groupID, actorID, due, dto.Notes)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"invoice_id":        invoiceID,
		"invoice_number":    invoiceNumber,
		"recognized_amount": recognized,
	})
}

// Kebijakan gate BAST realisasi (GET/PUT /charges/policy) DIHAPUS di W-5:
// keputusan klien D-3 mencabutnya permanen, jadi tidak ada lagi yang bisa
// disetel. Riwayat perubahannya tetap dibuka di bawah — audit tidak ikut hilang
// hanya karena kebijakannya sudah tidak ada.

// policyHistory — riwayat perubahan kebijakan finansial tenant (R-A).
func (h *Handler) policyHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.svc.ListPolicyChanges(r.Context(), tenantID, limit)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

// ── Master Jenis Biaya Realisasi (W-1) ──────────────────────────────────────

type chargeTypeDTO struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	DepositAccountCode string `json:"deposit_account_code,omitempty"`
}

type chargeTypeUpdateDTO struct {
	Name               *string `json:"name,omitempty"`
	DepositAccountCode *string `json:"deposit_account_code,omitempty"`
	IsActive           *bool   `json:"is_active,omitempty"`
}

func (h *Handler) listChargeTypes(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListChargeTypes(r.Context(), tenantID)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

func (h *Handler) createChargeType(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	var dto chargeTypeDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	ct, err := h.svc.CreateChargeType(r.Context(), tenantID, ChargeTypeInput{
		Code: dto.Code, Name: dto.Name, DepositAccountCode: dto.DepositAccountCode,
	}, actor)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ct)
}

func (h *Handler) updateChargeType(w http.ResponseWriter, r *http.Request) {
	tenantID, actor, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "typeID"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id jenis biaya tidak valid")
		return
	}
	var dto chargeTypeUpdateDTO
	if !decodeBody(w, r, &dto) {
		return
	}
	ct, uerr := h.svc.UpdateChargeType(r.Context(), tenantID, id, ChargeTypeUpdate{
		Name: dto.Name, DepositAccountCode: dto.DepositAccountCode, IsActive: dto.IsActive,
	}, actor)
	if uerr != nil {
		writeSvcErr(w, uerr)
		return
	}
	writeJSON(w, http.StatusOK, ct)
}

// masterHistory — audit append-only perubahan master data finansial (TD-8).
func (h *Handler) masterHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.svc.ListMasterChanges(r.Context(), tenantID, limit)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

// transferMemo — dokumen MEMO TRANSFER INTERNAL satu settlement (T-1).
func (h *Handler) transferMemo(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := ctxIDs(w, r)
	if !ok {
		return
	}
	settlementID, ok := urlID(w, r, "settlementID")
	if !ok {
		return
	}
	memo, err := h.svc.TransferMemo(r.Context(), tenantID, settlementID)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memo)
}

// ── Error mapping ────────────────────────────────────────────────────────────

func writeSvcErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrGroupNotFound), errors.Is(err, ErrItemNotFound), errors.Is(err, ErrContractNotFound),
		errors.Is(err, ErrTenantNotFound), errors.Is(err, ErrSettlementNotFound),
		errors.Is(err, ErrMemoNotIssued), errors.Is(err, ErrChargeTypeNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrGroupNotOpen), errors.Is(err, ErrItemNotOpen),
		errors.Is(err, ErrGroupHasMoney), errors.Is(err, ErrItemHasMoney),
		errors.Is(err, ErrAlreadyVoided), errors.Is(err, ErrVoidBreaksFunds),
		errors.Is(err, ErrSettleBlocked), errors.Is(err, ErrIdempotencyConflict),
		errors.Is(err, ErrChargeTypeDuplicate):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrKindInvalid), errors.Is(err, ErrLabelRequired),
		errors.Is(err, ErrItemsRequired), errors.Is(err, ErrAmountInvalid),
		errors.Is(err, ErrAllocationsRequired), errors.Is(err, ErrAllocationMismatch),
		errors.Is(err, ErrAllocationDuplicate), errors.Is(err, ErrAllocationExceeds),
		errors.Is(err, ErrItemNotInGroup), errors.Is(err, ErrExceedsResidual),
		errors.Is(err, ErrTargetGroupSame), errors.Is(err, ErrTerminNotForGroup),
		errors.Is(err, ErrVendorRequired), errors.Is(err, ErrNotATransfer),
		errors.Is(err, ErrChargeTypeInvalid), errors.Is(err, ErrChargeTypeRequired),
		errors.Is(err, ErrChargeTypeUnknown), errors.Is(err, ErrChargeTypeInactive),
		errors.Is(err, ErrDepositAccountInvalid):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrDepositAccountMissing),
		// W-2: memo transfer internal butuh master jenis dokumen. Bukan 500 —
		// ini konfigurasi yang kurang, dan admin bisa memperbaikinya sendiri.
		errors.Is(err, document.ErrTypeUnknown), errors.Is(err, document.ErrTypeInactive):
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, sale.ErrPaymentExceedsReceivable):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
