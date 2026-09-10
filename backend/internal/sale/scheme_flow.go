package sale

// Increment 3 — integrasi Payment Scheme (strategy seam) ke alur penjualan.
// Desain: docs/increment-3-payment-scheme-design.md (§3–§6, §11).
//
// Prinsip yang dipegang file ini:
//   - TIDAK ada `if policy_type == "kpr"` — semua perilaku lewat scheme.PaymentSchemePolicy.
//   - Kontrak berjalan membaca SNAPSHOT params-nya sendiri (approval note #2),
//     tidak pernah membaca master aktif.
//   - Semua milestone tercatat append-only di contract_payment_events (note #3);
//     scheme_state hanyalah proyeksi event terakhir.
//   - Ledger: satu-satunya jurnal baru adalah REKLAS piutang saat akad terjadi
//     SETELAH BAST (Dr piutang-bank / Cr piutang-buyer) — balanced, tanpa P&L.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/scheme"
)

// ── Model: ContractPaymentEvent (append-only — approval note #3) ──────────────

// ContractPaymentEvent adalah satu kejadian lifecycle scheme pada kontrak.
// APPEND-ONLY: tidak ada jalur update/delete dari aplikasi.
type ContractPaymentEvent struct {
	ID             uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID       uint64 `gorm:"not null;index"           json:"-"`
	SaleContractID uint64 `gorm:"not null;index"           json:"sale_contract_id"`
	Event          string `gorm:"not null;size:30"         json:"event"`
	FromState      string `gorm:"not null;size:30;default:''" json:"from_state"`
	ToState        string `gorm:"not null;size:30;default:''" json:"to_state"`
	// EventDate: tanggal KEJADIAN BISNIS (akad/pencairan/BAST/pembayaran) —
	// terpisah dari CreatedAt (tanggal input). Audit memakai EventDate.
	EventDate         time.Time `gorm:"not null"                 json:"event_date"`
	FinancingSourceID *uint64   `gorm:"index"                    json:"financing_source_id,omitempty"`
	JournalEntryID    *uint64   `json:"journal_entry_id,omitempty"` // terisi bila event memicu jurnal (reklas akad)
	Notes             string    `gorm:"size:500"                 json:"notes,omitempty"`
	CreatedBy         *uint64   `json:"created_by,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (ContractPaymentEvent) TableName() string { return "contract_payment_events" }

// ── Store & lookup interfaces ─────────────────────────────────────────────────

// SchemeFlowStore adalah kebutuhan persistensi alur scheme (di luar ContractStore
// agar mock ContractStore existing tidak berubah — additive).
type SchemeFlowStore interface {
	FindSchemeByID(ctx context.Context, tenantID, id uint64) (*scheme.PaymentScheme, error)
	FindFinancingSourceByID(ctx context.Context, tenantID, id uint64) (*scheme.FinancingSource, error)
	// UpdateContractSchemeFields memperbarui scheme_state (wajib) dan
	// financing_source_id (bila non-nil) sebuah kontrak.
	UpdateContractSchemeFields(ctx context.Context, tenantID, contractID uint64, state string, financingSourceID *uint64) error
	// RebindContractScheme mengganti scheme kontrak (konversi): scheme_id +
	// snapshot baru + state baru + financing source (boleh nil = hapus).
	RebindContractScheme(ctx context.Context, tenantID, contractID, schemeID uint64, snapshot, state string, financingSourceID *uint64) error
	// UpdateContractLoanAmount menyimpan Nilai Persetujuan KPR Bank (7A, UAT
	// 2026-09-07) — diisi SAAT AKAD, bukan saat pembuatan kontrak. Dasar
	// pemisahan Dana Jaminan Bank vs Piutang Usaha di Event 3.
	UpdateContractLoanAmount(ctx context.Context, tenantID, contractID uint64, amount domain.Money) error
	// AppendPaymentEvent menulis satu baris event (append-only).
	AppendPaymentEvent(ctx context.Context, ev *ContractPaymentEvent) error
	ListPaymentEvents(ctx context.Context, tenantID, contractID uint64) ([]*ContractPaymentEvent, error)
	// SupersedeUnpaidSchedules menandai baris jadwal paid_amount=0 (scheduled/
	// overdue) menjadi superseded. Baris terbayar TIDAK disentuh (BS-5).
	SupersedeUnpaidSchedules(ctx context.Context, tenantID, contractID uint64) error
	// MaxScheduleVersion mengembalikan versi jadwal tertinggi kontrak (0 = belum ada).
	MaxScheduleVersion(ctx context.Context, tenantID, contractID uint64) (int, error)
}

// PartyLookup memvalidasi keberadaan Customer & Sales Person (tenant-scoped) —
// enforcement kebijakan LOCKED Increment 1 (kontrak baru wajib keduanya).
type PartyLookup interface {
	CustomerExists(ctx context.Context, tenantID, id uint64) error
	SalesPersonExists(ctx context.Context, tenantID, id uint64) error
}

// WithSchemeFlow mengaktifkan alur Payment Scheme pada Service (wiring produksi).
// Tanpa opsi ini Service berperilaku legacy penuh (unit test lama tetap valid).
func WithSchemeFlow(store SchemeFlowStore, registry *scheme.Registry, parties PartyLookup) ServiceOption {
	return func(s *Service) {
		s.schemeFlow = store
		s.schemeRegistry = registry
		s.parties = parties
	}
}

func (s *Service) schemeEnabled() bool {
	return s.schemeFlow != nil && s.schemeRegistry != nil
}

// ── Scheme context (dibaca dari SNAPSHOT kontrak — bukan master) ──────────────

type schemeContext struct {
	policy scheme.PaymentSchemePolicy
	params scheme.Params
	state  scheme.State
}

// schemeContextFor memuat policy+params+state dari SNAPSHOT kontrak.
// nil (tanpa error) = kontrak legacy (snapshot NULL) → perilaku lama.
func (s *Service) schemeContextFor(ctx context.Context, c *SaleContract) (*schemeContext, error) {
	if !s.schemeEnabled() || c == nil || c.SchemeParamsSnapshot == nil || c.PaymentSchemeID == nil {
		return nil, nil
	}
	snap, err := scheme.ParseTermsSnapshot(*c.SchemeParamsSnapshot)
	if err != nil {
		return nil, fmt.Errorf("snapshot terms kontrak %d: %w", c.ID, err)
	}
	// Policy dari AMPLOP SNAPSHOT (self-contained; policy_type + policy_version
	// beku — hardening pasca-review). Snapshot format awal (tanpa amplop):
	// fallback baca master row (jalur legacy).
	policyType := snap.PolicyType
	if policyType == "" {
		m, merr := s.schemeFlow.FindSchemeByID(ctx, c.TenantID, *c.PaymentSchemeID)
		if merr != nil {
			return nil, merr
		}
		policyType = m.PolicyType
	}
	pol, err := s.schemeRegistry.Policy(policyType)
	if err != nil {
		return nil, err
	}
	params := snap.Params
	st := scheme.StateSigned
	if c.SchemeState != nil && *c.SchemeState != "" {
		st = scheme.State(*c.SchemeState)
	}
	return &schemeContext{policy: pol, params: params, state: st}, nil
}

// contractFacts membangun fakta kontrak untuk policy.
func contractFactsOf(c *SaleContract) scheme.ContractFacts {
	return scheme.ContractFacts{
		GrossAmount:        c.GrossAmount,
		ContractDate:       c.ContractDate,
		HasFinancingSource: c.FinancingSourceID != nil,
		LoanAmount:         c.LoanAmount,
	}
}

// ── Validasi & pembekuan scheme saat kontrak dibuat ───────────────────────────

// applySchemeToNewContract memvalidasi request kontrak baru terhadap scheme +
// kebijakan party, lalu mengisi field seam & snapshot pada kontrak (belum save).
// Mengembalikan error bila enforcement dilanggar. Dipanggil dari CreateContract
// HANYA saat schemeEnabled() (wiring produksi).
func (s *Service) applySchemeToNewContract(ctx context.Context, tenantID uint64, req CreateContractRequest, c *SaleContract) error {
	// Enforcement kebijakan LOCKED Increment 1 + approval note #4.
	if req.PaymentSchemeID == nil {
		return ErrPaymentSchemeRequired
	}
	if req.CustomerID == nil {
		return ErrCustomerRequiredForContract
	}
	if req.SalesPersonID == nil {
		return ErrSalesPersonRequiredForContract
	}
	if s.parties != nil {
		if err := s.parties.CustomerExists(ctx, tenantID, *req.CustomerID); err != nil {
			return err
		}
		if err := s.parties.SalesPersonExists(ctx, tenantID, *req.SalesPersonID); err != nil {
			return err
		}
		// AdminMarketingPersonID opsional (P1 — Sales ≠ Admin Marketing): kalau
		// diisi, tetap harus personel valid di tenant ini. Reuse master yang sama
		// dengan Sales — perannya dibedakan oleh KOLOM, bukan tabel.
		if req.AdminMarketingPersonID != nil {
			if err := s.parties.SalesPersonExists(ctx, tenantID, *req.AdminMarketingPersonID); err != nil {
				return err
			}
		}
	}

	m, err := s.schemeFlow.FindSchemeByID(ctx, tenantID, *req.PaymentSchemeID)
	if err != nil {
		return err
	}
	if !m.IsActive {
		return scheme.ErrSchemeInactive
	}
	params, err := scheme.ParseParams(m.Params)
	if err != nil {
		return err
	}
	pol, err := s.schemeRegistry.Policy(m.PolicyType)
	if err != nil {
		return err
	}
	if req.FinancingSourceID != nil {
		if _, err := s.schemeFlow.FindFinancingSourceByID(ctx, tenantID, *req.FinancingSourceID); err != nil {
			return err
		}
	}

	c.CustomerID = req.CustomerID
	c.SalesPersonID = req.SalesPersonID
	c.AdminMarketingPersonID = req.AdminMarketingPersonID
	c.PaymentSchemeID = &m.ID
	c.FinancingSourceID = req.FinancingSourceID
	// payment_type legacy diisi dari mapping terpusat (reader lama tetap jalan).
	if c.PaymentType == "" {
		c.PaymentType = PaymentType(m.PolicyType.LegacyPaymentType())
	}

	if err := pol.ValidateParams(params); err != nil {
		return err
	}
	if err := pol.ValidateContract(params, contractFactsOf(c)); err != nil {
		return err
	}

	// Contract Payment Terms Snapshot (approval note #2 + hardening): bekukan
	// SELURUH params BESERTA identitas & versi strategy (amplop TermsSnapshot).
	snap, err := scheme.NewTermsSnapshot(pol, params).JSON()
	if err != nil {
		return fmt.Errorf("bekukan terms snapshot: %w", err)
	}
	st := string(scheme.StateSigned)
	c.SchemeParamsSnapshot = &snap
	c.SchemeState = &st
	return nil
}

// recordContractSignedEvent menulis event pembuka lifecycle.
func (s *Service) recordContractSignedEvent(ctx context.Context, c *SaleContract, createdBy *uint64) error {
	if !s.schemeEnabled() || c.SchemeState == nil {
		return nil
	}
	return s.schemeFlow.AppendPaymentEvent(ctx, &ContractPaymentEvent{
		TenantID:          c.TenantID,
		SaleContractID:    c.ID,
		Event:             string(scheme.EventContractSigned),
		FromState:         "",
		ToState:           *c.SchemeState,
		EventDate:         c.ContractDate, // tanggal bisnis = tanggal kontrak
		FinancingSourceID: c.FinancingSourceID,
		CreatedBy:         createdBy,
	})
}

// ── Schedule plan (usulan dari policy — §5 desain) ────────────────────────────

// GetSchedulePlan mengembalikan USULAN jadwal dari policy (snapshot kontrak).
// User boleh mengubahnya; CreatePaymentSchedule memvalidasi Σ hasil akhir.
func (s *Service) GetSchedulePlan(ctx context.Context, tenantID, contractID uint64) ([]ScheduleItem, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	c, err := s.contracts.FindContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil {
		return nil, err
	}
	if sctx == nil {
		return nil, ErrContractHasNoScheme
	}
	plan, err := sctx.policy.BuildSchedulePlan(sctx.params, contractFactsOf(c))
	if err != nil {
		return nil, err
	}
	items := make([]ScheduleItem, len(plan))
	for i, p := range plan {
		items[i] = ScheduleItem{
			InstallmentNumber: p.InstallmentNumber,
			DueDate:           p.DueDate,
			Amount:            p.Amount,
			Type:              ScheduleType(p.Type),
		}
	}
	return items, nil
}

// validateScheduleSum menegakkan Σ(items) == target persis (Invariant #3 spirit).
func validateScheduleSum(items []ScheduleItem, target domain.Money) error {
	var sum domain.Money
	for _, it := range items {
		sum = sum.Add(it.Amount)
	}
	if !sum.Equal(target) {
		return fmt.Errorf("%w: Σ jadwal %s ≠ %s", ErrScheduleSumMismatch, sum, target)
	}
	return nil
}

// ── Scheme events (manual — financing milestones) ─────────────────────────────

// manualSchemeEvents adalah event yang boleh diposting via endpoint scheme-events.
// Event otomatis (dp_paid/fully_paid/installment_paid/handed_over/contract_signed)
// dan event ber-alur khusus (converted/rescheduled/cancelled) TIDAK boleh manual.
// `cancelled` menunggu domain Cancellation (jurnal pembalik) — increment lain.
var manualSchemeEvents = map[scheme.Event]bool{
	scheme.EventSubmittedToBank: true,
	scheme.EventBankApproved:    true,
	scheme.EventBankRejected:    true,
	scheme.EventAkad:            true,
	scheme.EventDisbursed:       true,
	scheme.EventTakeover:        true,
}

// ApplySchemeEventRequest adalah input transisi manual.
type ApplySchemeEventRequest struct {
	ContractID        uint64
	Event             scheme.Event
	EventDate         time.Time // tanggal kejadian bisnis; zero = sekarang
	FinancingSourceID *uint64   // takeover/ganti bank: bank baru
	Notes             string
	CreatedBy         *uint64
	// BankApprovedAmount (Item 7A, UAT 2026-09-07): Nilai Persetujuan KPR
	// Bank — WAJIB untuk event `akad` pada kontrak KPR-financed yang BAST-nya
	// sudah terjadi lebih dulu (Akad pasca-BAST — lihat reclassOnStateChange /
	// reclassToFinancingIfBAST). Diabaikan untuk event lain.
	BankApprovedAmount *domain.Money
}

// ApplySchemeEvent memproses satu financing milestone: validasi transisi via
// policy, jurnal reklas bila akad terjadi PASCA-BAST, catat event, proyeksikan
// state. Reklas: Dr piutang-baru / Cr piutang-lama sebesar outstanding unit
// (balanced, tanpa P&L — BS-2).
func (s *Service) ApplySchemeEvent(ctx context.Context, tenantID uint64, req ApplySchemeEventRequest) (*ContractPaymentEvent, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	if !s.schemeEnabled() {
		return nil, ErrSchemeFlowNotConfigured
	}
	if !manualSchemeEvents[req.Event] {
		return nil, fmt.Errorf("%w: %q", ErrSchemeEventNotManual, req.Event)
	}
	c, err := s.contracts.FindContractByID(ctx, tenantID, req.ContractID)
	if err != nil {
		return nil, err
	}
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil {
		return nil, err
	}
	if sctx == nil {
		return nil, ErrContractHasNoScheme
	}

	// Transisi via state machine policy (bukan if-else per scheme).
	tr, ok := sctx.policy.AllowedEvents()[req.Event]
	if !ok || !stateIn(sctx.state, tr.From) {
		return nil, fmt.Errorf("%w: event %q dari state %q", scheme.ErrInvalidTransition, req.Event, sctx.state)
	}
	newState := sctx.state
	if !tr.KeepState {
		newState = tr.To
	}

	// Financing source: takeover wajib bank baru; event lain opsional (validasi ada).
	var updateFinSource *uint64
	if req.FinancingSourceID != nil {
		if _, err := s.schemeFlow.FindFinancingSourceByID(ctx, tenantID, *req.FinancingSourceID); err != nil {
			return nil, err
		}
		updateFinSource = req.FinancingSourceID
	}
	if req.Event == scheme.EventTakeover && updateFinSource == nil {
		return nil, ErrFinancingSourceRequiredForTakeover
	}

	eventDate := req.EventDate
	if eventDate.IsZero() {
		eventDate = timeNow()
	}

	// Reklas piutang bila akun receivable BERPINDAH karena event ini dan unit
	// sudah BAST (pra-BAST tidak ada AR di ledger — BS-2).
	journalID, jerr := s.reclassOnStateChange(ctx, tenantID, c, sctx.state, newState, req.Event, eventDate, req.BankApprovedAmount)
	if jerr != nil {
		return nil, jerr
	}

	ev := &ContractPaymentEvent{
		TenantID:          tenantID,
		SaleContractID:    c.ID,
		Event:             string(req.Event),
		FromState:         string(sctx.state),
		ToState:           string(newState),
		EventDate:         eventDate,
		FinancingSourceID: req.FinancingSourceID,
		JournalEntryID:    journalID,
		Notes:             req.Notes,
		CreatedBy:         req.CreatedBy,
	}
	if err := s.schemeFlow.AppendPaymentEvent(ctx, ev); err != nil {
		return nil, err
	}
	if err := s.schemeFlow.UpdateContractSchemeFields(ctx, tenantID, c.ID, string(newState), updateFinSource); err != nil {
		return nil, err
	}
	return ev, nil
}

func stateIn(s scheme.State, list []scheme.State) bool {
	for _, x := range list {
		if s == x {
			return true
		}
	}
	return false
}

// reclassOnStateChange memposting jurnal reklas bila — dan hanya bila — akun
// piutang efektif BERBEDA antara state lama dan state baru. Satu pintu untuk
// kedua jalur transisi (ApplySchemeEvent eksplisit dan proyeksi milestone
// pembayaran) supaya tidak ada jalur yang memindah state tanpa memindah saldo.
func (s *Service) reclassOnStateChange(ctx context.Context, tenantID uint64, c *SaleContract, from, to scheme.State, ev scheme.Event, eventDate time.Time, bankApprovedAmount *domain.Money) (*uint64, error) {
	// 7C fix (UAT 2026-09-07): `disbursed` TIDAK LAGI memindahkan seluruh sisa
	// tagihan sekaligus dari Dana Jaminan Bank ke Piutang Usaha. Sejak 7A,
	// kedua akun sudah dipisah dan diposting benar SAAT AKAD (Dana Jaminan
	// Bank = Nilai Persetujuan KPR Bank, Piutang Usaha = sisanya); tiap
	// pencairan KPR sesudahnya mengurangi Dana Jaminan Bank secara langsung
	// (schemeCreditAccountForPayment). Reklas lump-sum lama adalah BUG: ia
	// menghapus seluruh saldo Dana Jaminan Bank pada pencairan PERTAMA
	// (walau baru cair sebagian), membuat pencairan ke-2/ke-3 salah sasaran
	// ke Piutang Usaha. Tidak ada aturan bisnis yang meminta reklas otomatis
	// di titik ini lagi — biarkan kedua akun turun sendiri-sendiri.
	if ev == scheme.EventDisbursed {
		return nil, nil
	}
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil || sctx == nil {
		return nil, err
	}
	fromCode := sctx.policy.ResolveReceivableAccount(sctx.params, from)
	toCode := sctx.policy.ResolveReceivableAccount(sctx.params, to)
	if fromCode == toCode {
		return nil, nil
	}
	// Item 7A (UAT 2026-09-07): Akad yang menaikkan state ke financing (Dana
	// Jaminan Bank) adalah TITIK KEDUA (selain RecordAkad langsung) di mana
	// Nilai Persetujuan KPR Bank diperlukan — kasus "Akad terjadi SETELAH
	// BAST". Reklas di sini DIBATASI ke min(approved, outstanding), bukan
	// seluruh outstanding (lihat reclassToFinancingIfBAST).
	if ev == scheme.EventAkad && sctx.params.FinancingReceivableAccount != "" && toCode == sctx.params.FinancingReceivableAccount {
		return s.reclassToFinancingIfBAST(ctx, tenantID, c, fromCode, toCode, bankApprovedAmount, ev, eventDate)
	}
	return s.reclassReceivableIfBAST(ctx, tenantID, c, fromCode, toCode, ev, eventDate)
}

// reclassReceivableIfBAST memposting jurnal reklas piutang unit kontrak bila
// unit sudah BAST dan masih ada outstanding. Mengembalikan (nil, nil) bila unit
// belum BAST atau outstanding nol — tidak ada jurnal (Invariant #1: no zero lines).
func (s *Service) reclassReceivableIfBAST(ctx context.Context, tenantID uint64, c *SaleContract, fromCode, toCode string, ev scheme.Event, eventDate time.Time) (*uint64, error) {
	saleRec, err := s.store.FindSaleRecord(ctx, tenantID, c.UnitID)
	if err != nil {
		if errors.Is(err, ErrSaleRecordNotFound) {
			return nil, nil // pra-BAST: AR belum lahir di ledger (BS-2)
		}
		return nil, fmt.Errorf("cek status BAST: %w", err)
	}
	outstanding, err := s.unitOutstanding(ctx, tenantID, saleRec)
	if err != nil {
		return nil, err
	}
	if outstanding.IsZero() || outstanding.IsNeg() {
		return nil, nil
	}

	fromID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, fromCode)
	if err != nil {
		return nil, fmt.Errorf("akun piutang asal %s: %w", fromCode, err)
	}
	toID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, toCode)
	if err != nil {
		return nil, fmt.Errorf("akun piutang tujuan %s: %w", toCode, err)
	}

	pid := saleRec.ProjectID
	uid := c.UnitID
	desc := fmt.Sprintf("Reklas piutang %s → %s (event scheme: %s)", fromCode, toCode, ev)
	lines := []JournalLineInput{
		{AccountID: toID, Debit: outstanding, ProjectID: &pid, PhaseID: saleRec.PhaseID, UnitID: &uid, Description: desc},
		{AccountID: fromID, Credit: outstanding, ProjectID: &pid, PhaseID: saleRec.PhaseID, UnitID: &uid, Description: desc},
	}
	jid, err := s.journals.CreateJournal(ctx, tenantID, eventDate, desc, lines)
	if err != nil {
		return nil, fmt.Errorf("buat jurnal reklas: %w", err)
	}
	if err := s.journals.PostJournal(ctx, tenantID, jid); err != nil {
		return nil, fmt.Errorf("posting jurnal reklas: %w", err)
	}
	return &jid, nil
}

// timeNow diisolasi agar test bisa deterministic bila perlu.
var timeNow = func() time.Time { return time.Now() }

// ListPaymentEvents mengembalikan audit trail lifecycle sebuah kontrak.
func (s *Service) ListPaymentEvents(ctx context.Context, tenantID, contractID uint64) ([]*ContractPaymentEvent, error) {
	if !s.schemeEnabled() {
		return nil, ErrSchemeFlowNotConfigured
	}
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	if _, err := s.contracts.FindContractByID(ctx, tenantID, contractID); err != nil {
		return nil, err
	}
	return s.schemeFlow.ListPaymentEvents(ctx, tenantID, contractID)
}

// ── Konversi scheme (KPR ditolak → inhouse/cash — D3) ─────────────────────────

// ConvertSchemeRequest adalah input konversi scheme kontrak.
type ConvertSchemeRequest struct {
	ContractID        uint64
	NewSchemeID       uint64
	EventDate         time.Time // tanggal kejadian bisnis; zero = sekarang
	FinancingSourceID *uint64   // untuk konversi ke scheme berpembiayaan (jarang)
	Notes             string
	CreatedBy         *uint64
}

// ConvertScheme mengganti scheme kontrak: sah hanya bila policy lama mengizinkan
// EventConverted dari state saat ini (mis. KPR bank_rejected). Jadwal belum
// terbayar di-supersede; snapshot params baru dibekukan; state kembali `signed`
// (gate BAST berbasis FAKTA pembayaran sehingga state signed selalu aman;
// milestone dp_paid/fully_paid terproyeksi ulang pada pembayaran berikutnya).
// Approval Workflow men-gate operasi ini saat engine §10 dibangun (seam:
// approval_request_id).
func (s *Service) ConvertScheme(ctx context.Context, tenantID uint64, req ConvertSchemeRequest) (*ContractPaymentEvent, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	if !s.schemeEnabled() {
		return nil, ErrSchemeFlowNotConfigured
	}
	c, err := s.contracts.FindContractByID(ctx, tenantID, req.ContractID)
	if err != nil {
		return nil, err
	}
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil {
		return nil, err
	}
	if sctx == nil {
		return nil, ErrContractHasNoScheme
	}
	tr, ok := sctx.policy.AllowedEvents()[scheme.EventConverted]
	if !ok || !stateIn(sctx.state, tr.From) {
		return nil, fmt.Errorf("%w: konversi dari state %q tidak diizinkan policy %q",
			scheme.ErrInvalidTransition, sctx.state, sctx.policy.PolicyType())
	}

	// Scheme baru: aktif + params valid + fakta kontrak valid menurut policy baru.
	m, err := s.schemeFlow.FindSchemeByID(ctx, tenantID, req.NewSchemeID)
	if err != nil {
		return nil, err
	}
	if !m.IsActive {
		return nil, scheme.ErrSchemeInactive
	}
	newParams, err := scheme.ParseParams(m.Params)
	if err != nil {
		return nil, err
	}
	newPol, err := s.schemeRegistry.Policy(m.PolicyType)
	if err != nil {
		return nil, err
	}
	if req.FinancingSourceID != nil {
		if _, err := s.schemeFlow.FindFinancingSourceByID(ctx, tenantID, *req.FinancingSourceID); err != nil {
			return nil, err
		}
	}
	facts := contractFactsOf(c)
	facts.HasFinancingSource = req.FinancingSourceID != nil
	if err := newPol.ValidateParams(newParams); err != nil {
		return nil, err
	}
	if err := newPol.ValidateContract(newParams, facts); err != nil {
		return nil, err
	}

	// Supersede jadwal belum terbayar (baris terbayar tidak disentuh — BS-5).
	if err := s.schemeFlow.SupersedeUnpaidSchedules(ctx, tenantID, c.ID); err != nil {
		return nil, err
	}

	snap, err := scheme.NewTermsSnapshot(newPol, newParams).JSON()
	if err != nil {
		return nil, err
	}
	newState := string(scheme.StateSigned)
	if err := s.schemeFlow.RebindContractScheme(ctx, tenantID, c.ID, m.ID, snap, newState, req.FinancingSourceID); err != nil {
		return nil, err
	}

	convDate := req.EventDate
	if convDate.IsZero() {
		convDate = timeNow()
	}
	ev := &ContractPaymentEvent{
		TenantID:          tenantID,
		SaleContractID:    c.ID,
		Event:             string(scheme.EventConverted),
		FromState:         string(sctx.state),
		ToState:           newState,
		EventDate:         convDate,
		FinancingSourceID: req.FinancingSourceID,
		Notes:             fmt.Sprintf("konversi ke scheme %s (%s). %s", m.Code, m.PolicyType, req.Notes),
		CreatedBy:         req.CreatedBy,
	}
	if err := s.schemeFlow.AppendPaymentEvent(ctx, ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// ── Regenerate schedule (reschedule — BS-5) ───────────────────────────────────

// RegenerateScheduleRequest: jadwal baru menggantikan baris BELUM terbayar.
type RegenerateScheduleRequest struct {
	ContractID uint64
	Items      []ScheduleItem
	EventDate  time.Time // tanggal kejadian bisnis; zero = sekarang
	Notes      string
	CreatedBy  *uint64
}

// RegenerateSchedule men-supersede baris jadwal paid_amount=0 dan membuat versi
// baru. Validasi: Σ(baris dipertahankan) + Σ(items baru) == GrossAmount persis.
// Baris terbayar (paid_amount>0) TIDAK disentuh — alokasinya adalah fakta.
func (s *Service) RegenerateSchedule(ctx context.Context, tenantID uint64, req RegenerateScheduleRequest) ([]*PaymentSchedule, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	if !s.schemeEnabled() {
		return nil, ErrSchemeFlowNotConfigured
	}
	if len(req.Items) == 0 {
		return nil, ErrScheduleItemsRequired
	}
	c, err := s.contracts.FindContractByID(ctx, tenantID, req.ContractID)
	if err != nil {
		return nil, err
	}
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil {
		return nil, err
	}
	if sctx == nil {
		return nil, ErrContractHasNoScheme
	}

	// Σ target = gross − Σ amount baris yang DIPERTAHANKAN (paid_amount > 0,
	// status != superseded).
	existing, err := s.contracts.ListSchedulesByContract(ctx, tenantID, c.ID)
	if err != nil {
		return nil, err
	}
	retained := domain.Zero
	for _, row := range existing {
		if row.Status != ScheduleStatusSuperseded && !row.PaidAmount.IsZero() {
			retained = retained.Add(row.Amount)
		}
	}
	if err := validateScheduleSum(req.Items, c.GrossAmount.Sub(retained)); err != nil {
		return nil, err
	}

	if err := s.schemeFlow.SupersedeUnpaidSchedules(ctx, tenantID, c.ID); err != nil {
		return nil, err
	}
	maxVer, err := s.schemeFlow.MaxScheduleVersion(ctx, tenantID, c.ID)
	if err != nil {
		return nil, err
	}
	rows := make([]*PaymentSchedule, len(req.Items))
	for i, item := range req.Items {
		rows[i] = &PaymentSchedule{
			TenantID:          tenantID,
			SaleContractID:    c.ID,
			UnitID:            c.UnitID,
			InstallmentNumber: item.InstallmentNumber,
			DueDate:           item.DueDate,
			Amount:            item.Amount,
			Type:              item.Type,
			Status:            ScheduleStatusScheduled,
			ScheduleVersion:   maxVer + 1,
		}
	}
	if err := s.contracts.SaveScheduleItems(ctx, rows); err != nil {
		return nil, fmt.Errorf("simpan jadwal versi baru: %w", err)
	}
	reschedDate := req.EventDate
	if reschedDate.IsZero() {
		reschedDate = timeNow()
	}
	ev := &ContractPaymentEvent{
		TenantID:       tenantID,
		SaleContractID: c.ID,
		Event:          string(scheme.EventRescheduled),
		FromState:      string(sctx.state),
		ToState:        string(sctx.state),
		EventDate:      reschedDate,
		Notes:          req.Notes,
		CreatedBy:      req.CreatedBy,
	}
	if err := s.schemeFlow.AppendPaymentEvent(ctx, ev); err != nil {
		return nil, err
	}
	return rows, nil
}

// ── Auto milestone dari pembayaran (proyeksi — best effort) ───────────────────

// applyPaymentMilestones memproyeksikan milestone scheme setelah sebuah
// penerimaan: dp_paid / installment_paid / fully_paid. BEST-EFFORT: kegagalan
// proyeksi TIDAK membatalkan pembayaran (SoT = ledger + termin + alokasi;
// event hanyalah proyeksi audit). Transisi yang tidak sah dilewati diam-diam
// (mis. pembayaran tambahan saat KPR sudah submitted_to_bank).
func (s *Service) applyPaymentMilestones(ctx context.Context, tenantID uint64, contract *SaleContract, createdBy *uint64, eventDate time.Time, source PaymentSource, financingSourceID *uint64) {
	if contract == nil || !s.schemeEnabled() {
		return
	}
	sctx, err := s.schemeContextFor(ctx, contract)
	if err != nil || sctx == nil {
		return
	}

	// R1 KPR Realization: pencairan bank yang tercatat sebagai pembayaran
	// (source kpr_disbursement) OTOMATIS memajukan state akad → disbursed —
	// satu aksi staf keuangan = uang + milestone (atomik secara bisnis; state
	// adalah proyeksi audit, bukan jurnal — retry aman/idempoten).
	if source == PaymentSourceKPRDisbursement && sctx.state == scheme.StateAkad {
		if tr, ok := sctx.policy.AllowedEvents()[scheme.EventDisbursed]; ok && stateIn(sctx.state, tr.From) {
			// T-3: `disbursed` memindahkan sisa tagihan dari Piutang Bank ke
			// Piutang Customer. Jurnal reklas HARUS lebih dulu — kalau gagal,
			// state TIDAK dimajukan. Lebih baik milestone tertinggal (bisa
			// diulang lewat endpoint event) daripada state berkata "piutang
			// customer" sementara ledger masih menahan sisanya di piutang bank.
			jid, rerr := s.reclassOnStateChange(ctx, tenantID, contract, sctx.state, tr.To, scheme.EventDisbursed, eventDate, nil)
			if rerr != nil {
				return // proyeksi dilewati; pembayaran tetap sah (SoT = ledger)
			}
			_ = s.schemeFlow.AppendPaymentEvent(ctx, &ContractPaymentEvent{
				TenantID:          tenantID,
				SaleContractID:    contract.ID,
				Event:             string(scheme.EventDisbursed),
				FromState:         string(sctx.state),
				ToState:           string(tr.To),
				EventDate:         eventDate,
				JournalEntryID:    jid,
				CreatedBy:         createdBy,
				FinancingSourceID: financingSourceID,
			})
			_ = s.schemeFlow.UpdateContractSchemeFields(ctx, tenantID, contract.ID, string(tr.To), nil)
			sctx.state = tr.To // lanjutkan evaluasi milestone dari state baru
		}
	}
	total, err := s.store.SumTerminsByUnit(ctx, tenantID, contract.UnitID)
	if err != nil {
		return
	}
	dp, err := sctx.params.DPAmount(contract.GrossAmount)
	if err != nil {
		return
	}

	var ev scheme.Event
	switch {
	case !total.Decimal().LessThan(contract.GrossAmount.Decimal()):
		ev = scheme.EventFullyPaid
	case sctx.state == scheme.StateSigned && !dp.IsZero() && !total.Decimal().LessThan(dp.Decimal()):
		ev = scheme.EventDPPaid
	case sctx.state == scheme.StateDPPaid && total.Decimal().GreaterThan(dp.Decimal()):
		ev = scheme.EventInstallmentPaid
	default:
		return
	}

	tr, ok := sctx.policy.AllowedEvents()[ev]
	if !ok || !stateIn(sctx.state, tr.From) {
		return // transisi tak sah untuk scheme ini → lewati (proyeksi saja)
	}
	newState := sctx.state
	if !tr.KeepState {
		newState = tr.To
	}
	if eventDate.IsZero() {
		eventDate = timeNow()
	}
	// Aturan sama dengan pencairan: state hanya boleh maju kalau saldo piutang
	// ikut pindah. Untuk milestone ini biasanya no-op (akun tidak berubah, atau
	// outstanding sudah nol pada fully_paid).
	jid, rerr := s.reclassOnStateChange(ctx, tenantID, contract, sctx.state, newState, ev, eventDate, nil)
	if rerr != nil {
		return
	}
	_ = s.schemeFlow.AppendPaymentEvent(ctx, &ContractPaymentEvent{
		TenantID:       tenantID,
		SaleContractID: contract.ID,
		Event:          string(ev),
		FromState:      string(sctx.state),
		ToState:        string(newState),
		EventDate:      eventDate, // tanggal pembayaran (kejadian bisnis)
		JournalEntryID: jid,
		CreatedBy:      createdBy,
	})
	_ = s.schemeFlow.UpdateContractSchemeFields(ctx, tenantID, contract.ID, string(newState), nil)
}

// applyHandedOverMilestone mencatat BAST pada lifecycle scheme (best-effort).
func (s *Service) applyHandedOverMilestone(ctx context.Context, tenantID, unitID uint64, bastDate time.Time) {
	if !s.schemeEnabled() || s.contracts == nil {
		return
	}
	c, err := s.contracts.FindContractByUnitID(ctx, tenantID, unitID)
	if err != nil {
		return
	}
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil || sctx == nil {
		return
	}
	tr, ok := sctx.policy.AllowedEvents()[scheme.EventHandedOver]
	if !ok || !stateIn(sctx.state, tr.From) {
		return
	}
	newState := sctx.state
	if !tr.KeepState {
		newState = tr.To
	}
	if bastDate.IsZero() {
		bastDate = timeNow()
	}
	_ = s.schemeFlow.AppendPaymentEvent(ctx, &ContractPaymentEvent{
		TenantID:       tenantID,
		SaleContractID: c.ID,
		Event:          string(scheme.EventHandedOver),
		FromState:      string(sctx.state),
		ToState:        string(newState),
		EventDate:      bastDate, // tanggal BAST (kejadian bisnis)
	})
	_ = s.schemeFlow.UpdateContractSchemeFields(ctx, tenantID, c.ID, string(newState), nil)
}

// applyAkadMilestone mencatat Akad pada lifecycle scheme (best-effort — SoT
// pengakuan tetap SaleRecord/ledger; proyeksi ini murni audit-trail). Untuk
// KPR, state biasanya SUDAH akad (dicapai lebih dulu via ApplySchemeEvent di
// FinancingMilestones), sehingga transisi ini efektif no-op (AllowedEvents
// KPR tidak punya EventAkad dari StateAkad) — itu benar, event Akad KPR sudah
// tercatat saat itu terjadi, bukan diulang di sini.
func (s *Service) applyAkadMilestone(ctx context.Context, tenantID, unitID uint64, recognitionDate time.Time) {
	if !s.schemeEnabled() || s.contracts == nil {
		return
	}
	c, err := s.contracts.FindContractByUnitID(ctx, tenantID, unitID)
	if err != nil {
		return
	}
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil || sctx == nil {
		return
	}
	tr, ok := sctx.policy.AllowedEvents()[scheme.EventAkad]
	if !ok || !stateIn(sctx.state, tr.From) {
		return
	}
	newState := sctx.state
	if !tr.KeepState {
		newState = tr.To
	}
	if recognitionDate.IsZero() {
		recognitionDate = timeNow()
	}
	_ = s.schemeFlow.AppendPaymentEvent(ctx, &ContractPaymentEvent{
		TenantID:       tenantID,
		SaleContractID: c.ID,
		Event:          string(scheme.EventAkad),
		FromState:      string(sctx.state),
		ToState:        string(newState),
		EventDate:      recognitionDate,
	})
	_ = s.schemeFlow.UpdateContractSchemeFields(ctx, tenantID, c.ID, string(newState), nil)
}

// schemeAkadGuard menegakkan gate Akad (pengakuan pendapatan/HPP) + me-resolve
// akun piutang untuk Akad. Kontrak legacy / scheme tidak aktif → gate lewat,
// akun default (perilaku lama). Temuan #7: dulu bernama schemeBASTGuard —
// dipanggil dari RecordAkad, bukan lagi dari alur serah-terima fisik.
func (s *Service) schemeAkadGuard(ctx context.Context, tenantID, unitID uint64, totalAdvance, gross domain.Money) (receivableCode string, err error) {
	receivableCode = accountCodePiutang
	if !s.schemeEnabled() || s.contracts == nil {
		return receivableCode, nil
	}
	c, cerr := s.contracts.FindContractByUnitID(ctx, tenantID, unitID)
	if cerr != nil {
		return receivableCode, nil // unit tanpa kontrak → perilaku lama
	}
	sctx, serr := s.schemeContextFor(ctx, c)
	if serr != nil {
		return "", serr
	}
	if sctx == nil {
		return receivableCode, nil
	}
	if err := sctx.policy.CanRecognize(sctx.params, sctx.state, scheme.PaymentFacts{
		TotalReceived: totalAdvance,
		GrossAmount:   gross,
	}); err != nil {
		return "", err
	}
	return sctx.policy.ResolveReceivableAccount(sctx.params, sctx.state), nil
}

// schemeCreditAccountForPayment me-resolve akun kredit pembayaran PASCA-BAST
// dari policy (pra-BAST selalu Uang Muka — routing GAP-1 tidak berubah).
//
// 7C fix (UAT 2026-09-07): source=kpr_disbursement SELALU menuju Dana Jaminan
// Bank (FinancingReceivableAccount) selama akun itu terkonfigurasi — TIDAK
// LAGI bergantung pada scheme_state. Sebelumnya akun kredit ditentukan lewat
// ResolveReceivableAccount(state), yang berpindah ke Piutang Usaha begitu
// state maju ke `disbursed` (dipicu OTOMATIS oleh pencairan PERTAMA) — akibatnya
// pencairan ke-2/ke-3 salah mengkredit Piutang Usaha, bukan lagi mengurangi
// Dana Jaminan Bank. Pembayaran BUKAN pencairan bank (mis. pelunasan langsung
// oleh customer) tetap memakai resolusi berbasis state seperti semula.
func (s *Service) schemeCreditAccountForPayment(ctx context.Context, c *SaleContract, source PaymentSource) (string, bool) {
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil || sctx == nil {
		return "", false
	}
	if source == PaymentSourceKPRDisbursement && sctx.params.FinancingReceivableAccount != "" {
		return sctx.params.FinancingReceivableAccount, true
	}
	return sctx.policy.ResolveReceivableAccount(sctx.params, sctx.state), true
}

// schemeAkadSplitAccounts (Item 7A, UAT 2026-09-07) me-resolve DUA akun yang
// dibutuhkan untuk memecah baris piutang Event 3 kontrak ber-scheme KPR: kode
// Dana Jaminan Bank (financingCode, kosong bila kontrak tidak dibiayai bank)
// dan kode Piutang Usaha default (defaultReceivableCode). Keduanya diambil
// LANGSUNG dari params scheme, TIDAK bergantung pada scheme_state — Akad
// adalah SATU-SATUNYA momen kedua akun ini sekaligus diposting; transisi
// state sesudahnya (mis. `disbursed`) tidak boleh mengubah retroaktif ke
// mana Akad tadinya memposting (lihat 7C: reclassOnStateChange sudah tidak
// lagi melakukan reklas lump-sum otomatis).
func (s *Service) schemeAkadSplitAccounts(ctx context.Context, c *SaleContract) (financingCode, defaultReceivableCode string) {
	sctx, err := s.schemeContextFor(ctx, c)
	if err != nil || sctx == nil {
		return "", ""
	}
	return sctx.params.FinancingReceivableAccount, sctx.params.ReceivableOrDefault()
}

// resolveBankApprovedAmount (Item 7A, UAT 2026-09-07) menentukan DAN menyimpan
// Nilai Persetujuan KPR Bank — dipanggil dari DUA titik yang sama-sama bisa
// jadi "momen Akad" kontrak KPR-financed: RecordAkad langsung (Akad
// terjadi di/sebelum BAST — lihat blok Item 7A di RecordAkad) dan
// ApplySchemeEvent EventAkad (Akad terjadi SETELAH BAST — lihat
// reclassToFinancingIfBAST). reqAmount menang atas contract.LoanAmount lama
// (kontrak legacy pra-7A yang sudah mengisi saat pembuatan kontrak); dipakai
// sebagai fallback hanya bila reqAmount nil. Hanya reqAmount yang dipersist —
// fallback dari LoanAmount sudah tersimpan sebelumnya, menulis ulang di sini
// hanya membuang siklus.
func (s *Service) resolveBankApprovedAmount(ctx context.Context, tenantID uint64, contract *SaleContract, reqAmount *domain.Money) (domain.Money, error) {
	var approved *domain.Money
	if reqAmount != nil {
		if !reqAmount.IsWholeRupiah() {
			return domain.Zero, ErrBankApprovedAmountFractional
		}
		if reqAmount.IsZero() || reqAmount.IsNeg() {
			return domain.Zero, ErrBankApprovedAmountZeroOrNeg
		}
		approved = reqAmount
	} else if contract.LoanAmount != nil {
		approved = contract.LoanAmount
	}
	if approved == nil {
		return domain.Zero, ErrBankApprovedAmountRequired
	}
	if reqAmount != nil && s.schemeFlow != nil {
		if err := s.schemeFlow.UpdateContractLoanAmount(ctx, tenantID, contract.ID, *approved); err != nil {
			return domain.Zero, fmt.Errorf("simpan Nilai Persetujuan KPR Bank: %w", err)
		}
	}
	return *approved, nil
}

// reclassToFinancingIfBAST (Item 7A, UAT 2026-09-07) menangani kasus Akad
// TERJADI SETELAH BAST untuk kontrak KPR-financed: pada saat BAST, seluruh
// outstanding masih di akun piutang default (belum ada bank yang di-approve).
// Begitu event Akad membawa Nilai Persetujuan KPR Bank, HANYA sebesar
// min(approved, outstanding) yang direklas ke Dana Jaminan Bank — sisanya
// (jika ada) TETAP di Piutang Usaha, bukan direklas penuh seperti perilaku
// lama (itu bug: menganggap seluruh sisa tagihan otomatis jadi tanggungan
// bank, padahal Nilai Persetujuan KPR Bank bisa lebih kecil dari harga unit).
func (s *Service) reclassToFinancingIfBAST(ctx context.Context, tenantID uint64, c *SaleContract, fromCode, toCode string, bankApprovedAmount *domain.Money, ev scheme.Event, eventDate time.Time) (*uint64, error) {
	saleRec, err := s.store.FindSaleRecord(ctx, tenantID, c.UnitID)
	if err != nil {
		if errors.Is(err, ErrSaleRecordNotFound) {
			return nil, nil // pra-BAST: split sudah ditangani langsung oleh RecordAkad
		}
		return nil, fmt.Errorf("cek status BAST: %w", err)
	}
	outstanding, err := s.unitOutstanding(ctx, tenantID, saleRec)
	if err != nil {
		return nil, err
	}
	if outstanding.IsZero() || outstanding.IsNeg() {
		return nil, nil
	}

	approved, aerr := s.resolveBankApprovedAmount(ctx, tenantID, c, bankApprovedAmount)
	if aerr != nil {
		return nil, aerr
	}
	financingAmount := approved
	if financingAmount.GreaterThan(outstanding) {
		financingAmount = outstanding
	}
	if financingAmount.IsZero() {
		return nil, nil // Invariant #1: tidak ada baris jurnal nol
	}

	fromID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, fromCode)
	if err != nil {
		return nil, fmt.Errorf("akun piutang asal %s: %w", fromCode, err)
	}
	toID, err := s.accounts.FindAccountIDByCode(ctx, tenantID, toCode)
	if err != nil {
		return nil, fmt.Errorf("akun piutang tujuan %s: %w", toCode, err)
	}

	pid := saleRec.ProjectID
	uid := c.UnitID
	desc := fmt.Sprintf("Reklas piutang %s → %s (event scheme: %s) — Dana Jaminan Bank = Nilai Persetujuan KPR Bank", fromCode, toCode, ev)
	lines := []JournalLineInput{
		{AccountID: toID, Debit: financingAmount, ProjectID: &pid, PhaseID: saleRec.PhaseID, UnitID: &uid, Description: desc},
		{AccountID: fromID, Credit: financingAmount, ProjectID: &pid, PhaseID: saleRec.PhaseID, UnitID: &uid, Description: desc},
	}
	jid, err := s.journals.CreateJournal(ctx, tenantID, eventDate, desc, lines)
	if err != nil {
		return nil, fmt.Errorf("buat jurnal reklas: %w", err)
	}
	if err := s.journals.PostJournal(ctx, tenantID, jid); err != nil {
		return nil, fmt.Errorf("posting jurnal reklas: %w", err)
	}
	return &jid, nil
}
