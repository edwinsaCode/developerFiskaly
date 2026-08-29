package cost

// HTTP W-10 Transaksi Pengeluaran.
//
// Handler ini TIPIS di atas Service dan tabel cost_entries yang sudah ada —
// bukan mesin kedua. Yang dikerjakannya hanya tiga hal:
//  1. menerjemahkan "scope" yang dipahami user (operasional / proyek) menjadi
//     bentuk request yang sudah dimengerti domain biaya,
//  2. menolak metode pembayaran yang belum punya jalur pelunasan (§9), dan
//  3. mengembalikan bukti akuntansi (nomor dokumen + referensi jurnal) supaya
//     user tidak perlu membuka modul lain untuk percaya transaksinya tercatat.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"esaproperti/internal/domain"
	"esaproperti/internal/fixedasset"
	"esaproperti/internal/platform/auth"
)

// Scope transaksi pengeluaran sebagaimana dipilih user di UI.
const (
	expenseScopeOperasional = "operasional"
	expenseScopeProyek      = "proyek"
)

// Jenis Pembelian — toggle di form Pengeluaran (SATU pintu masuk, bukan
// sistem terpisah): "expense" (default, jalur lama tak berubah) atau
// "fixed_asset" (Dr Aset Tetap / Cr Kas-Bank, lalu masuk Fixed Asset
// Register — lihat internal/fixedasset).
const (
	purchaseTypeExpense    = "expense"
	purchaseTypeFixedAsset = "fixed_asset"
)

func (h *Handler) mountExpenses(r chi.Router) {
	r.Route("/expenses", func(r chi.Router) {
		r.Get("/", h.listExpenses)
		r.Post("/preview", h.previewExpense)
		r.With(auth.RequireWrite()).Post("/", h.createExpense)
		r.Get("/{id}", h.getExpense)
	})

	r.Route("/expense-types", func(r chi.Router) {
		r.Get("/", h.listExpenseTypes)
		r.Get("/changes", h.listExpenseTypeChanges)
		r.With(auth.RequireWrite()).Post("/", h.createExpenseType)
		r.With(auth.RequireWrite()).Patch("/{id}", h.updateExpenseType)
	})
}

// ── DTO ─────────────────────────────────────────────────────────────────────

type expenseDTO struct {
	// PurchaseType: "expense" (default, kosong = expense) | "fixed_asset".
	// Menentukan apakah body diterjemahkan ke CreateCostEntryRequest (jalur
	// lama, tidak berubah) atau fixedasset.AcquisitionInput.
	PurchaseType string `json:"purchase_type,omitempty"`

	Scope string `json:"scope"` // "operasional" | "proyek"

	Date            string `json:"date"`
	Amount          string `json:"amount"`
	PaymentMethod   string `json:"payment_method"` // W-10: hanya "bank" (kas/bank)
	BankAccountCode string `json:"bank_account_code"`
	Vendor          string `json:"vendor"`
	Description     string `json:"description"`

	// Scope operasional
	ExpenseTypeID *uint64 `json:"expense_type_id,omitempty"`

	// Scope proyek (dan tag proyek opsional untuk operasional)
	ProjectID    *uint64 `json:"project_id,omitempty"`
	UnitID       *uint64 `json:"unit_id,omitempty"`
	PhaseID      *uint64 `json:"phase_id,omitempty"`
	BudgetItemID *uint64 `json:"budget_item_id,omitempty"`
	Category     string  `json:"category,omitempty"`
	CostTier     string  `json:"cost_tier,omitempty"`

	// PurchaseType = fixed_asset
	FixedAssetCategoryID *uint64 `json:"fixed_asset_category_id,omitempty"`
	AssetName            string  `json:"asset_name,omitempty"`
	ResidualValue        string  `json:"residual_value,omitempty"`
	UsefulLifeMonths     uint    `json:"useful_life_months,omitempty"`
	DepreciationMethod   string  `json:"depreciation_method,omitempty"`
}

// toRequest menerjemahkan scope UI menjadi CreateCostEntryRequest.
//
// Perhatikan apa yang TIDAK dilakukan di sini: tidak ada kode akun, tidak ada
// aturan RAB, tidak ada keputusan tier untuk jalur proyek. Semua itu milik
// service — handler hanya memindahkan bentuk.
func (dto expenseDTO) toRequest(amount domain.Money, date time.Time) (CreateCostEntryRequest, error) {
	req := CreateCostEntryRequest{
		Amount:          amount,
		Date:            date,
		PaymentMethod:   PaymentMethod(dto.PaymentMethod),
		BankAccountCode: dto.BankAccountCode,
		Vendor:          dto.Vendor,
		Description:     dto.Description,
		UnitID:          dto.UnitID,
		PhaseID:         dto.PhaseID,
		BudgetItemID:    dto.BudgetItemID,
	}
	if dto.ProjectID != nil {
		req.ProjectID = *dto.ProjectID
	}

	switch dto.Scope {
	case expenseScopeOperasional:
		if dto.ExpenseTypeID == nil || *dto.ExpenseTypeID == 0 {
			return CreateCostEntryRequest{}, ErrExpenseTypeRequired
		}
		req.ExpenseTypeID = dto.ExpenseTypeID
		// Beban periode: selalu overhead. Kategori diisi `other` semata-mata
		// agar matrix tier × kategori tetap sah — akun debit yang sebenarnya
		// datang dari master jenis pengeluaran, bukan dari kategori ini.
		req.CostTier = domain.CostTierOverhead
		req.Category = domain.CostCategoryOther

	case expenseScopeProyek:
		if req.ProjectID == 0 {
			return CreateCostEntryRequest{}, ErrProjectRequired
		}
		if dto.ExpenseTypeID != nil {
			return CreateCostEntryRequest{}, ErrExpenseTypeNotForProjectCost
		}
		req.Category = domain.CostCategory(dto.Category)
		req.CostTier = domain.CostTier(dto.CostTier)

	default:
		return CreateCostEntryRequest{}, errors.New("scope tidak valid: gunakan operasional atau proyek")
	}

	// §9 (T-9): hutang usaha belum punya jalur pelunasan yang lengkap. Membuka
	// opsi ini di sini akan melahirkan saldo hutang yang tidak pernah bisa
	// dilunasi dari UI mana pun — hutang hantu di neraca. Ditolak terang-terangan,
	// bukan diterima diam-diam.
	if req.PaymentMethod == PaymentMethodPayable {
		return CreateCostEntryRequest{}, ErrPayableNotSupported
	}
	return req, nil
}

// decodeExpenseBody membaca body mentah tanpa menerjemahkannya — dipakai
// bersama oleh jalur expense dan jalur fixed_asset supaya body hanya dibaca
// SEKALI sebelum bercabang pada PurchaseType.
func decodeExpenseBody(w http.ResponseWriter, r *http.Request) (expenseDTO, bool) {
	var dto expenseDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return expenseDTO{}, false
	}
	return dto, true
}

func decodeExpenseDTO(w http.ResponseWriter, r *http.Request) (CreateCostEntryRequest, bool) {
	dto, ok := decodeExpenseBody(w, r)
	if !ok {
		return CreateCostEntryRequest{}, false
	}
	return finishExpenseDTO(w, dto)
}

func finishExpenseDTO(w http.ResponseWriter, dto expenseDTO) (CreateCostEntryRequest, bool) {
	amount, err := domain.NewMoney(dto.Amount)
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "nominal tidak valid: "+err.Error())
		return CreateCostEntryRequest{}, false
	}
	date, err := time.Parse("2006-01-02", dto.Date)
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "tanggal harus YYYY-MM-DD")
		return CreateCostEntryRequest{}, false
	}
	req, err := dto.toRequest(amount, date)
	if err != nil {
		writeCostError(w, http.StatusBadRequest, err.Error())
		return CreateCostEntryRequest{}, false
	}
	return req, true
}

// toFixedAssetInput menerjemahkan body Pengeluaran ke fixedasset.AcquisitionInput
// saat PurchaseType == fixed_asset. Field yang tidak relevan (scope,
// expense_type_id, category, cost_tier) diabaikan — bukan galat, karena FE
// tidak mengirimkannya untuk jalur ini.
func (dto expenseDTO) toFixedAssetInput() (fixedasset.AcquisitionInput, error) {
	amount, err := domain.NewMoney(dto.Amount)
	if err != nil {
		return fixedasset.AcquisitionInput{}, err
	}
	date, err := time.Parse("2006-01-02", dto.Date)
	if err != nil {
		return fixedasset.AcquisitionInput{}, errors.New("tanggal harus YYYY-MM-DD")
	}
	residual := domain.Zero
	if dto.ResidualValue != "" {
		residual, err = domain.NewMoney(dto.ResidualValue)
		if err != nil {
			return fixedasset.AcquisitionInput{}, err
		}
	}
	if dto.FixedAssetCategoryID == nil || *dto.FixedAssetCategoryID == 0 {
		return fixedasset.AcquisitionInput{}, fixedasset.ErrCategoryNotFound
	}
	return fixedasset.AcquisitionInput{
		CategoryID:         *dto.FixedAssetCategoryID,
		ProjectID:          dto.ProjectID,
		AssetName:          dto.AssetName,
		AcquisitionDate:    date,
		AcquisitionCost:    amount,
		ResidualValue:      residual,
		UsefulLifeMonths:   dto.UsefulLifeMonths,
		DepreciationMethod: fixedasset.DepreciationMethod(orDefault(dto.DepreciationMethod, string(fixedasset.DepreciationMethodStraightLine))),
		PaymentMethod:      fixedasset.PaymentMethod(orDefault(dto.PaymentMethod, string(fixedasset.PaymentMethodBank))),
		BankAccountCode:    dto.BankAccountCode,
		Vendor:             dto.Vendor,
		Description:        dto.Description,
	}, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ── Transaksi ───────────────────────────────────────────────────────────────

// previewExpense: dry-run. Menampilkan jurnal PERSIS yang akan terbit —
// user melihat akun sebelum menyetujui, bukan sesudah.
func (h *Handler) previewExpense(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	dto, ok := decodeExpenseBody(w, r)
	if !ok {
		return
	}
	if dto.PurchaseType == purchaseTypeFixedAsset {
		h.previewFixedAssetExpense(w, r, tenantID, dto)
		return
	}
	req, ok := finishExpenseDTO(w, dto)
	if !ok {
		return
	}
	lines, err := h.svc.PreviewCostEntry(r.Context(), tenantID, req)
	if err != nil {
		writeExpenseErr(w, err)
		return
	}
	writeCostJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// createExpense mencatat DAN memposting dalam satu transaksi (§7).
// Tidak ada tahap draft: uangnya memang sudah keluar.
func (h *Handler) createExpense(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	dto, ok := decodeExpenseBody(w, r)
	if !ok {
		return
	}
	if dto.PurchaseType == purchaseTypeFixedAsset {
		h.createFixedAssetExpense(w, r, tenantID, dto)
		return
	}
	req, ok := finishExpenseDTO(w, dto)
	if !ok {
		return
	}
	entry, err := h.svc.CreateAndPostCostEntry(r.Context(), tenantID, req)
	if err != nil {
		writeExpenseErr(w, err)
		return
	}
	// Kembalikan bentuk yang sama dengan daftar: nomor BKK dan referensi jurnal
	// ikut, supaya layar sukses bisa menunjukkan buktinya langsung.
	item, err := h.svc.GetExpense(r.Context(), tenantID, entry.ID)
	if err != nil {
		writeCostJSON(w, http.StatusCreated, entry)
		return
	}
	writeCostJSON(w, http.StatusCreated, item)
}

// ── Jenis Pembelian: Fixed Asset ────────────────────────────────────────────
//
// SATU pintu masuk yang sama (/expenses) — toggle "Jenis Pembelian" saja,
// bukan endpoint baru. Delegasi penuh ke internal/fixedasset: package ini
// tidak tahu apa pun soal jurnal atau akun aset, hanya menerjemahkan bentuk.

func (h *Handler) previewFixedAssetExpense(w http.ResponseWriter, r *http.Request, tenantID uint64, dto expenseDTO) {
	if h.fixedAssets == nil {
		writeCostError(w, http.StatusServiceUnavailable, "modul aset tetap belum aktif")
		return
	}
	in, err := dto.toFixedAssetInput()
	if err != nil {
		writeCostError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview, err := h.fixedAssets.PreviewAcquisition(r.Context(), tenantID, in)
	if err != nil {
		writeFixedAssetErr(w, err)
		return
	}
	writeCostJSON(w, http.StatusOK, preview)
}

func (h *Handler) createFixedAssetExpense(w http.ResponseWriter, r *http.Request, tenantID uint64, dto expenseDTO) {
	if h.fixedAssets == nil {
		writeCostError(w, http.StatusServiceUnavailable, "modul aset tetap belum aktif")
		return
	}
	in, err := dto.toFixedAssetInput()
	if err != nil {
		writeCostError(w, http.StatusBadRequest, err.Error())
		return
	}
	asset, err := h.fixedAssets.AcquireAsset(r.Context(), tenantID, in)
	if err != nil {
		writeFixedAssetErr(w, err)
		return
	}
	writeCostJSON(w, http.StatusCreated, asset)
}

// writeFixedAssetErr memetakan error internal/fixedasset ke status HTTP,
// mengikuti pola writeExpenseErr persis (validasi → 400, tidak ada → 404,
// duplikat → 409, sisanya → 500 — tidak ada silent failure).
func writeFixedAssetErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fixedasset.ErrCategoryNotFound), errors.Is(err, fixedasset.ErrAssetNotFound):
		writeCostError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, fixedasset.ErrCategoryDuplicate), errors.Is(err, fixedasset.ErrDepreciationAlreadyPosted):
		writeCostError(w, http.StatusConflict, err.Error())
	case isFixedAssetDomainError(err):
		writeCostError(w, http.StatusBadRequest, err.Error())
	default:
		writeCostError(w, http.StatusInternalServerError, err.Error())
	}
}

func isFixedAssetDomainError(err error) bool {
	return errors.Is(err, fixedasset.ErrCategoryInactive) ||
		errors.Is(err, fixedasset.ErrCategoryInvalid) ||
		errors.Is(err, fixedasset.ErrAssetAccountInvalid) ||
		errors.Is(err, fixedasset.ErrAccumulatedDepreciationAccountInvalid) ||
		errors.Is(err, fixedasset.ErrDepreciationExpenseAccountInvalid) ||
		errors.Is(err, fixedasset.ErrAssetNameRequired) ||
		errors.Is(err, fixedasset.ErrAmountZeroOrNeg) ||
		errors.Is(err, fixedasset.ErrAmountFractional) ||
		errors.Is(err, fixedasset.ErrResidualNegative) ||
		errors.Is(err, fixedasset.ErrResidualExceedsCost) ||
		errors.Is(err, fixedasset.ErrInvalidUsefulLife) ||
		errors.Is(err, fixedasset.ErrUnsupportedDepreciationMethod) ||
		errors.Is(err, fixedasset.ErrInvalidPaymentMethod) ||
		errors.Is(err, fixedasset.ErrBankAccountCodeRequired) ||
		errors.Is(err, fixedasset.ErrNotCashBankAccount) ||
		errors.Is(err, fixedasset.ErrPayableNotSupported) ||
		errors.Is(err, fixedasset.ErrAssetDisposed) ||
		errors.Is(err, fixedasset.ErrInvalidPeriod) ||
		errors.Is(err, fixedasset.ErrAccountNotFound)
}

func (h *Handler) getExpense(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseCostUintParam(r, "id")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	item, err := h.svc.GetExpense(r.Context(), tenantID, id)
	if errors.Is(err, ErrCostEntryNotFound) {
		writeCostError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusOK, item)
}

func (h *Handler) listExpenses(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	q := r.URL.Query()
	f := ExpenseFilter{Scope: q.Get("scope")}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.To = &t
		}
	}
	if v := q.Get("project_id"); v != "" {
		if id, err := parseUint(v); err == nil {
			f.ProjectID = &id
		}
	}
	items, err := h.svc.ListExpenses(r.Context(), tenantID, f)
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusOK, items)
}

// ── Master jenis pengeluaran ────────────────────────────────────────────────

func (h *Handler) listExpenseTypes(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	types, err := h.types.List(r.Context(), tenantID)
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// INV-EXP-2 diberitahukan ke UI sebagai DATA, bukan disimpulkan frontend
	// dari daftar kode akun yang disalin ke sana.
	type typeView struct {
		*ExpenseType
		AllowsProjectTag bool `json:"allows_project_tag"`
	}
	out := make([]typeView, 0, len(types))
	for _, t := range types {
		out = append(out, typeView{ExpenseType: t, AllowsProjectTag: !IsCostTaxonomyAccount(t.ExpenseAccountCode)})
	}
	writeCostJSON(w, http.StatusOK, out)
}

func (h *Handler) listExpenseTypeChanges(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	changes, err := h.types.ListMasterChanges(r.Context(), tenantID, 100)
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type changeView struct {
		ID        uint64    `json:"id"`
		Code      string    `json:"code"`
		Field     string    `json:"field"`
		OldValue  string    `json:"old_value"`
		NewValue  string    `json:"new_value"`
		ChangedBy *uint64   `json:"changed_by,omitempty"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := make([]changeView, 0, len(changes))
	for _, c := range changes {
		out = append(out, changeView{c.ID, c.EntityCode, c.Field, c.OldValue, c.NewValue, c.ChangedBy, c.CreatedAt})
	}
	writeCostJSON(w, http.StatusOK, out)
}

type expenseTypeDTO struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	ExpenseAccountCode string `json:"expense_account_code"`
}

func (h *Handler) createExpenseType(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var dto expenseTypeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	actor := actorID(r)
	et, err := h.types.Create(r.Context(), tenantID, ExpenseTypeInput{
		Code: dto.Code, Name: dto.Name, ExpenseAccountCode: dto.ExpenseAccountCode,
	}, actor)
	if err != nil {
		writeExpenseErr(w, err)
		return
	}
	writeCostJSON(w, http.StatusCreated, et)
}

func (h *Handler) updateExpenseType(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseCostUintParam(r, "id")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var body struct {
		Name               *string `json:"name"`
		ExpenseAccountCode *string `json:"expense_account_code"`
		IsActive           *bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	et, err := h.types.Update(r.Context(), tenantID, id, ExpenseTypeUpdate{
		Name: body.Name, ExpenseAccountCode: body.ExpenseAccountCode, IsActive: body.IsActive,
	}, actorID(r))
	if err != nil {
		writeExpenseErr(w, err)
		return
	}
	writeCostJSON(w, http.StatusOK, et)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func writeExpenseErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrExpenseTypeNotFound), errors.Is(err, ErrCostEntryNotFound):
		writeCostError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrExpenseTypeDuplicate):
		writeCostError(w, http.StatusConflict, err.Error())
	case isCostDomainError(err):
		writeCostError(w, http.StatusBadRequest, err.Error())
	default:
		writeCostError(w, http.StatusInternalServerError, err.Error())
	}
}

func actorID(r *http.Request) *uint64 {
	if uid, ok := auth.UserIDFrom(r.Context()); ok {
		return &uid
	}
	return nil
}

func parseUint(s string) (uint64, error) { return strconv.ParseUint(s, 10, 64) }
