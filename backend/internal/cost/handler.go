package cost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
	"esaproperti/internal/fixedasset"
	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/auth"
	"esaproperti/internal/project"
)

// Handler wires the cost entry HTTP routes.
type Handler struct {
	svc *Service
	// types (W-10): master jenis pengeluaran — CRUD admin, terpisah dari jalur
	// transaksi supaya perubahan master tidak pernah menumpang transaksi biaya.
	types *ExpenseTypeService
	// fixedAssets: toggle "Jenis Pembelian: Fixed Asset" di /expenses.
	// Diinjeksi lewat SetFixedAssetService (bukan dibangun di sini) supaya
	// cost package tidak perlu tahu cara merangkai kolaborator fixedasset —
	// cost hanya memegang instance jadi, sama seperti pola budgetItemAdapter.
	fixedAssets *fixedasset.Service
	// printRepo: nama tenant/proyek untuk header cetak (Riwayat Biaya, BKK).
	printRepo *GORMRepository
}

// SetFixedAssetService menyuntikkan layanan aset tetap ke toggle "Jenis
// Pembelian" di /expenses. nil (default) berarti toggle itu menolak dengan
// 503 — jangan sampai request nyasar ke jalur expense biasa secara diam-diam.
func (h *Handler) SetFixedAssetService(svc *fixedasset.Service) {
	h.fixedAssets = svc
}

// NewHandler constructs the production cost handler backed by GORM.
func NewHandler(db *gorm.DB) *Handler {
	repo := NewGORMRepository(db)
	ledgerRepo := ledger.NewGORMRepository(db)
	postingSvc := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	adapter := NewLedgerJournalAdapter(postingSvc)
	budgetRepo := budget.NewGORMRepository(db)
	budgetLookup := &budgetItemAdapter{repo: budgetRepo}
	svc := NewService(repo, adapter, repo, repo, budgetLookup)
	// Product Catalog: tolak kapitalisasi biaya ke unit non-properti.
	svc.SetUnitPolicyResolver(project.NewGORMRepository(db))
	// W-3.0: CreateCostEntry/PostCostEntry atomik (jalur kas keluar O-1).
	svc.SetTxRunner(NewGORMTxRunner(db, postingSvc))
	// W-10: master jenis pengeluaran, pembuktian akun kas/bank (T-5),
	// pembuktian unit ↔ proyek, dan pembaca daftar pengeluaran.
	//
	// Validator kas/bank sengaja dipasang untuk SELURUH jalur biaya — bukan
	// hanya endpoint baru. Pengetatan yang hanya berlaku di satu pintu bukan
	// invariant, hanya sopan santun.
	svc.SetExpenseTypeResolver(repo)
	svc.SetPaymentAccountValidator(repo)
	svc.SetUnitProjectResolver(repo)
	svc.SetExpenseReader(repo)
	return &Handler{svc: svc, types: NewExpenseTypeService(db), printRepo: repo}
}

// budgetItemAdapter menjembatani budget.GORMRepository ke cost.BudgetItemLookup.
// Ini mencegah budget package mengimport cost package (menghindari import cycle).
// Error dari budget package diterjemahkan ke cost package errors di sini.
type budgetItemAdapter struct {
	repo *budget.GORMRepository
}

func (a *budgetItemAdapter) FindItemForCostValidation(
	ctx context.Context, tenantID, projectID, itemID uint64,
) (domain.CostCategory, error) {
	foundProjectID, cc, isMappable, err := a.repo.FindItemForCostLookup(ctx, tenantID, itemID)
	if errors.Is(err, budget.ErrItemNotFound) {
		return "", ErrBudgetItemNotFound
	}
	if err != nil {
		return "", err
	}
	if foundProjectID != projectID {
		return "", ErrBudgetItemProjectMismatch
	}
	if !isMappable {
		return "", ErrBudgetItemCategoryMismatch
	}
	return cc, nil
}

// Mount registers all cost entry routes on the provided router.
// Caller must have applied auth.Middleware upstream.
func (h *Handler) Mount(r chi.Router) {
	// Per-project listing + creation
	r.Route("/projects/{projectID}/cost-entries", func(r chi.Router) {
		r.Get("/", h.listByProject)
		r.With(auth.RequireWrite()).Post("/", h.createCostEntry)
		// Dry-run: validates + resolves accounts via same logic as create, returns
		// journal lines without writing anything. Used by frontend preview.
		r.Post("/preview", h.previewCostEntry)
	})

	// Per-project accumulated cost (derived from ledger)
	r.Get("/projects/{projectID}/accumulated-cost", h.accumulatedByProject)
	r.Get("/projects/{projectID}/accumulated-cost/project-wide", h.accumulatedProjectWide)

	// Per-unit listing + accumulated cost
	r.Get("/units/{unitID}/cost-entries", h.listByUnit)
	r.Route("/projects/{projectID}/units/{unitID}/accumulated-cost", func(r chi.Router) {
		r.Get("/", h.accumulatedByUnit)
	})

	// Tenant-level cost entries (Increment 2): jalur pencatatan biaya OVERHEAD —
	// project_id opsional di body (cost center) atau kosong (biaya Tenant-level).
	// Juga menampung akses + lifecycle per entry.
	r.Route("/cost-entries", func(r chi.Router) {
		r.With(auth.RequireWrite()).Post("/", h.createTenantCostEntry)
		r.Post("/preview", h.previewTenantCostEntry)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.getCostEntry)
			r.With(auth.RequireWrite()).Post("/post", h.postCostEntry)
		})
	})

	// W-10 Transaksi Pengeluaran: satu pintu masuk untuk pengeluaran
	// operasional maupun proyek. Rute di file expense_handler.go.
	h.mountExpenses(r)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func costTenantID(r *http.Request) (uint64, error) {
	id, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, errors.New("missing authentication context")
	}
	return id, nil
}

func writeCostJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeCostError(w http.ResponseWriter, status int, msg string) {
	writeCostJSON(w, status, map[string]string{"error": msg})
}

func parseCostUintParam(r *http.Request, name string) (uint64, error) {
	return strconv.ParseUint(chi.URLParam(r, name), 10, 64)
}

func isCostDomainError(err error) bool {
	return errors.Is(err, ErrCostAmountFractional) ||
		errors.Is(err, ErrCostAmountZeroOrNeg) ||
		errors.Is(err, ErrInvalidCategory) ||
		errors.Is(err, ErrInvalidCostTier) ||
		errors.Is(err, ErrTierCategoryMismatch) ||
		errors.Is(err, ErrUnitRequiredForDirect) ||
		errors.Is(err, ErrUnitNotAllowedForTier) ||
		errors.Is(err, ErrPhaseRequiresProject) ||
		errors.Is(err, ErrBudgetLinkRequiresProject) ||
		errors.Is(err, ErrInvalidPaymentMethod) ||
		errors.Is(err, ErrBankAccountCodeRequired) ||
		errors.Is(err, ErrProjectRequired) ||
		errors.Is(err, ErrAccountNotFound) ||
		errors.Is(err, ErrCostEntryAlreadyPosted) ||
		errors.Is(err, ErrBudgetItemNotFound) ||
		errors.Is(err, ErrBudgetItemProjectMismatch) ||
		errors.Is(err, ErrBudgetItemCategoryMismatch) ||
		// W-10
		errors.Is(err, ErrExpenseTypeUnknown) ||
		errors.Is(err, ErrExpenseTypeInactive) ||
		errors.Is(err, ErrExpenseTypeNotFound) ||
		errors.Is(err, ErrExpenseTypeInvalid) ||
		errors.Is(err, ErrExpenseTypeDuplicate) ||
		errors.Is(err, ErrExpenseAccountInvalid) ||
		errors.Is(err, ErrExpenseTypeRequired) ||
		errors.Is(err, ErrExpenseTypeNotForProjectCost) ||
		errors.Is(err, ErrExpenseTypeWithBudgetItem) ||
		errors.Is(err, ErrExpenseTypeTaxonomyAccount) ||
		errors.Is(err, ErrPayableNotSupported) ||
		errors.Is(err, ErrNotCashBankAccount) ||
		errors.Is(err, ErrUnitNotInProject) ||
		errors.Is(err, ErrUnitNotHPPEligible) ||
		errors.Is(err, ErrUnitProductPolicyUnresolved) ||
		errors.Is(err, ErrHardSubcategoryRequired) ||
		errors.Is(err, ErrInvalidHardSubcategory) ||
		errors.Is(err, ErrHardSubcategoryNotAllowed) ||
		// Periode tertutup adalah penolakan yang SAH dan bisa dikoreksi user
		// (ubah tanggal, atau buka periode) — bukan kegagalan server.
		errors.Is(err, ledger.ErrPeriodClosed)
}

// ── Request DTO ───────────────────────────────────────────────────────────────

type createCostEntryDTO struct {
	Category        string  `json:"category"`
	// HardSubcategory (UAT 2026-09-07): produksi_subsidi|produksi_komersial|
	// sarana_prasarana|perizinan — wajib saat category=hard tanpa unit_id.
	HardSubcategory string  `json:"hard_subcategory,omitempty"`
	CostTier        string  `json:"cost_tier"` // direct|shared|overhead; kosong = infer (backward compat)
	Amount          string  `json:"amount"`
	PaymentMethod   string  `json:"payment_method"`
	BankAccountCode string  `json:"bank_account_code"`
	Date            string  `json:"date"`
	Vendor          string  `json:"vendor"`
	Description     string  `json:"description"`
	ProjectID       *uint64 `json:"project_id,omitempty"` // hanya dibaca route tenant-level; route project memakai path param
	UnitID          *uint64 `json:"unit_id,omitempty"`
	PhaseID         *uint64 `json:"phase_id,omitempty"`
	BudgetItemID    *uint64 `json:"budget_item_id,omitempty"` // opsional: tautan ke RAB
}

// toRequest mengonversi DTO menjadi CreateCostEntryRequest untuk projectID tertentu
// (0 = tanpa project — hanya sah untuk tier overhead; divalidasi service).
func (dto createCostEntryDTO) toRequest(projectID uint64, amount domain.Money, date time.Time) CreateCostEntryRequest {
	return CreateCostEntryRequest{
		ProjectID:       projectID,
		UnitID:          dto.UnitID,
		PhaseID:         dto.PhaseID,
		Category:        domain.CostCategory(dto.Category),
		HardSubcategory: domain.ConstructionSubcategory(dto.HardSubcategory),
		CostTier:        domain.CostTier(dto.CostTier),
		Amount:          amount,
		PaymentMethod:   PaymentMethod(dto.PaymentMethod),
		BankAccountCode: dto.BankAccountCode,
		Date:            date,
		Vendor:          dto.Vendor,
		Description:     dto.Description,
		BudgetItemID:    dto.BudgetItemID,
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (h *Handler) createCostEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseCostUintParam(r, "projectID")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	dto, amount, date, ok := decodeCostDTO(w, r)
	if !ok {
		return
	}

	entry, err := h.svc.CreateCostEntry(r.Context(), tenantID, dto.toRequest(projectID, amount, date))
	if err != nil {
		if isCostDomainError(err) {
			writeCostError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusCreated, entry)
}

// createTenantCostEntry mencatat biaya di level Tenant (tanpa project di path).
// Dipakai untuk tier OVERHEAD: project_id opsional di body (cost center reporting)
// atau kosong sama sekali (biaya perusahaan Tenant-level). Tier direct/shared juga
// bisa lewat sini asal project_id diisi — aturan sebenarnya ada di service.validate.
func (h *Handler) createTenantCostEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	dto, amount, date, ok := decodeCostDTO(w, r)
	if !ok {
		return
	}
	var projectID uint64
	if dto.ProjectID != nil {
		projectID = *dto.ProjectID
	}
	entry, err := h.svc.CreateCostEntry(r.Context(), tenantID, dto.toRequest(projectID, amount, date))
	if err != nil {
		if isCostDomainError(err) {
			writeCostError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusCreated, entry)
}

// previewTenantCostEntry adalah dry-run pasangan createTenantCostEntry.
func (h *Handler) previewTenantCostEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	dto, amount, date, ok := decodeCostDTO(w, r)
	if !ok {
		return
	}
	var projectID uint64
	if dto.ProjectID != nil {
		projectID = *dto.ProjectID
	}
	lines, err := h.svc.PreviewCostEntry(r.Context(), tenantID, dto.toRequest(projectID, amount, date))
	if err != nil {
		if isCostDomainError(err) {
			writeCostError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type previewResponse struct {
		Lines []JournalPreviewLine `json:"lines"`
	}
	writeCostJSON(w, http.StatusOK, previewResponse{Lines: lines})
}

// decodeCostDTO membaca body createCostEntryDTO + parse amount/date.
// Menulis respons error sendiri; ok=false berarti respons sudah dikirim.
func decodeCostDTO(w http.ResponseWriter, r *http.Request) (createCostEntryDTO, domain.Money, time.Time, bool) {
	var dto createCostEntryDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return dto, domain.Zero, time.Time{}, false
	}
	// T-9: hutang usaha belum punya jalur pelunasan. Pemetaan domainnya SUDAH
	// benar (payable → kredit 2-1000, lihat resolveDebitCreditCodes) dan sengaja
	// dipertahankan utuh untuk W-11 — yang belum ada adalah sisi debitnya:
	// tidak satu pun kode di repo ini yang pernah mendebit 2-1000. Saldo yang
	// lahir di sini karena itu tidak bisa dilunasi dari mana pun, dan karena
	// ledger append-only, satu-satunya jalan keluarnya adalah jurnal pembalik.
	//
	// Jalur /expenses sudah menolak sejak W-10 (expense_handler.go). Penolakan
	// yang hanya berlaku di satu pintu bukan invariant, hanya sopan santun —
	// jadi penjaga yang sama dipasang di gerbang biaya proyek/tenant. Dipasang
	// di transport, BUKAN di service.validate, supaya W-11 cukup menghapus blok
	// ini tanpa menyentuh domain atau satu pun test pemetaan akun.
	if PaymentMethod(dto.PaymentMethod) == PaymentMethodPayable {
		writeCostError(w, http.StatusBadRequest, ErrPayableNotSupported.Error())
		return dto, domain.Zero, time.Time{}, false
	}
	amount, err := domain.NewMoney(dto.Amount)
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "amount tidak valid: "+err.Error())
		return dto, domain.Zero, time.Time{}, false
	}
	date, err := time.Parse("2006-01-02", dto.Date)
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "date harus YYYY-MM-DD")
		return dto, domain.Zero, time.Time{}, false
	}
	return dto, amount, date, true
}

func (h *Handler) getCostEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseCostUintParam(r, "id")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid id")
		return
	}
	entry, err := h.svc.GetCostEntry(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, ErrCostEntryNotFound) {
			writeCostError(w, http.StatusNotFound, err.Error())
			return
		}
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusOK, entry)
}

func (h *Handler) postCostEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := parseCostUintParam(r, "id")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.PostCostEntry(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, ErrCostEntryNotFound) {
			writeCostError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, ErrCostEntryAlreadyPosted) {
			writeCostError(w, http.StatusConflict, err.Error())
			return
		}
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// previewCostEntry is a dry-run: validates + resolves accounts exactly as createCostEntry,
// but does NOT write any journal entry or CostEntry. Returns the journal lines that
// createCostEntry WOULD produce for the same payload.
// Read-only; tenant-scoped; no RequireWrite guard needed.
func (h *Handler) previewCostEntry(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseCostUintParam(r, "projectID")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	dto, amount, date, ok := decodeCostDTO(w, r)
	if !ok {
		return
	}

	lines, err := h.svc.PreviewCostEntry(r.Context(), tenantID, dto.toRequest(projectID, amount, date))
	if err != nil {
		if isCostDomainError(err) {
			writeCostError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type previewResponse struct {
		Lines []JournalPreviewLine `json:"lines"`
	}
	writeCostJSON(w, http.StatusOK, previewResponse{Lines: lines})
}

func (h *Handler) listByProject(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseCostUintParam(r, "projectID")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	entries, err := h.svc.ListByProject(r.Context(), tenantID, projectID)
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusOK, entries)
}

func (h *Handler) listByUnit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	unitID, err := parseCostUintParam(r, "unitID")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid unit id")
		return
	}
	entries, err := h.svc.ListByUnit(r.Context(), tenantID, unitID)
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusOK, entries)
}

// ── Accumulated cost response DTO ─────────────────────────────────────────────

type accumulatedCostResponse struct {
	Land      string `json:"land"`
	Hard      string `json:"hard"`
	Soft      string `json:"soft"`
	Financing string `json:"financing"`
	Total     string `json:"total"`
}

func breakdownToDTO(b domain.UnitCostBreakdown) accumulatedCostResponse {
	return accumulatedCostResponse{
		Land:      b.Land.String(),
		Hard:      b.Hard.String(),
		Soft:      b.Soft.String(),
		Financing: b.Financing.String(),
		Total:     b.Total().String(),
	}
}

func (h *Handler) accumulatedByProject(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseCostUintParam(r, "projectID")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	b, err := h.svc.AccumulatedByProject(r.Context(), tenantID, projectID)
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusOK, breakdownToDTO(b))
}

func (h *Handler) accumulatedByUnit(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseCostUintParam(r, "projectID")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	unitID, err := parseCostUintParam(r, "unitID")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid unit id")
		return
	}
	b, err := h.svc.AccumulatedByUnit(r.Context(), tenantID, projectID, unitID)
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusOK, breakdownToDTO(b))
}

func (h *Handler) accumulatedProjectWide(w http.ResponseWriter, r *http.Request) {
	tenantID, err := costTenantID(r)
	if err != nil {
		writeCostError(w, http.StatusUnauthorized, err.Error())
		return
	}
	projectID, err := parseCostUintParam(r, "projectID")
	if err != nil {
		writeCostError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	b, err := h.svc.AccumulatedProjectWide(r.Context(), tenantID, projectID)
	if err != nil {
		writeCostError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeCostJSON(w, http.StatusOK, breakdownToDTO(b))
}
