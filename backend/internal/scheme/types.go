package scheme

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── PolicyType (SSOT — pola CostTier: konstanta bertipe, tanpa literal tersebar) ─

// PolicyType mengikat sebuah Payment Scheme ke strategy implementasinya.
type PolicyType string

const (
	PolicyCash            PolicyType = "cash"
	PolicyCashInstallment PolicyType = "cash_installment"
	PolicyKPR             PolicyType = "kpr"
	PolicyInHouse         PolicyType = "inhouse"
)

// AllPolicyTypes adalah urutan kanonik policy untuk validasi/registry.
var AllPolicyTypes = []PolicyType{PolicyCash, PolicyCashInstallment, PolicyKPR, PolicyInHouse}

func (t PolicyType) Valid() bool {
	switch t {
	case PolicyCash, PolicyCashInstallment, PolicyKPR, PolicyInHouse:
		return true
	}
	return false
}

// LegacyPaymentType memetakan policy ke nilai sale_contracts.payment_type lama
// (kpr|tunai) — PEMETAAN TERPUSAT untuk kompatibilitas reader legacy (approval
// note #4); jangan tulis mapping ini di tempat lain (pola alias construction↔hard).
func (t PolicyType) LegacyPaymentType() string {
	if t == PolicyKPR {
		return "kpr"
	}
	return "tunai"
}

// ── State (state machine scheme di Sale Contract) ─────────────────────────────

// State adalah posisi kontrak dalam lifecycle pembiayaan skemanya.
// Kontrak legacy (snapshot NULL) TIDAK punya state (string kosong di DB = NULL).
type State string

const (
	StateSigned             State = "signed"
	StateDPPaid             State = "dp_paid"
	StateInstallmentRunning State = "installment_running"
	StateFullyPaid          State = "fully_paid"
	StateSubmittedToBank    State = "submitted_to_bank"
	StateBankApproved       State = "bank_approved"
	StateBankRejected       State = "bank_rejected"
	StateAkad               State = "akad"
	StateDisbursed          State = "disbursed"
	StateHandedOver         State = "handed_over"
	StateConverted          State = "converted"
	StateCancelled          State = "cancelled"
)

// ── Event (Payment Event append-only — approval note #3) ─────────────────────

// Event adalah kejadian bisnis pada lifecycle scheme. Event dicatat append-only
// di contract_payment_events; State hanyalah proyeksi event terakhir.
type Event string

const (
	EventContractSigned  Event = "contract_signed"
	EventDPPaid          Event = "dp_paid"
	EventInstallmentPaid Event = "installment_paid"
	EventSubmittedToBank Event = "submitted_to_bank"
	EventBankApproved    Event = "bank_approved"
	EventBankRejected    Event = "bank_rejected"
	EventAkad            Event = "akad"
	EventDisbursed       Event = "disbursed"
	EventTakeover        Event = "takeover"
	EventRescheduled     Event = "rescheduled"
	EventConverted       Event = "converted"
	EventFullyPaid       Event = "fully_paid"
	EventHandedOver      Event = "handed_over"
	EventCancelled       Event = "cancelled"
)

// Transition mendefinisikan dari state mana sebuah Event sah, dan ke state mana
// ia membawa kontrak. KeepState=true berarti event tercatat tanpa memindah state
// (mis. takeover bank, reschedule).
type Transition struct {
	From      []State
	To        State
	KeepState bool
}

// ── BASTGate ──────────────────────────────────────────────────────────────────

// BASTGate menentukan syarat minimum sebelum pendapatan/HPP boleh diakui saat
// Akad (per scheme row, dieksekusi policy.CanRecognize — configurable, bukan
// hardcode; nama tipe dipertahankan utk diff minimal, semantiknya kini
// menggerbang Akad, bukan BAST). Gate ini MENAMBAH gate BCA-2 (RAB approved +
// basis), tidak menggantikan.
type BASTGate string

const (
	GateFullPayment BASTGate = "full_payment"
	GateAkad        BASTGate = "akad"
	GateDPPaid      BASTGate = "dp_paid"
)

func (g BASTGate) Valid() bool {
	switch g {
	case GateFullPayment, GateAkad, GateDPPaid:
		return true
	}
	return false
}

// ── Params (Value Object — dibekukan ke kontrak sebagai Terms Snapshot) ───────

// DefaultReceivableAccount adalah akun piutang buyer default. SATU-SATUNYA tempat
// default ini didefinisikan (approval note #1: tanpa hardcode akun di service —
// resolusi selalu via Params + ResolveReceivableAccount).
const DefaultReceivableAccount = "1-2000"

// Params adalah parameter sebuah Payment Scheme. Disimpan sebagai JSON di master
// (payment_schemes.params) dan DIBEKUKAN ke sale_contracts.scheme_params_snapshot
// saat kontrak dibuat (approval note #2) — kontrak berjalan tidak pernah membaca
// master aktif.
type Params struct {
	// DPPercent: persen uang muka sebagai string desimal ("10" = 10%). String —
	// bukan float (Invariant #2 spirit); dihitung via Money.Allocate.
	DPPercent        string   `json:"dp_percent,omitempty"`
	InstallmentCount int      `json:"installment_count,omitempty"` // cash_installment
	TenorMonths      int      `json:"tenor_months,omitempty"`      // inhouse
	FinalDueMonths   int      `json:"final_due_months,omitempty"`  // kpr: target pelunasan bank
	BASTGate         BASTGate `json:"bast_gate"`
	// ReceivableAccount: akun piutang buyer (default 1-2000).
	ReceivableAccount string `json:"receivable_account,omitempty"`
	// FinancingReceivableAccount: akun piutang counterparty pembiayaan (bank KPR,
	// mis. 1-2200) — dipakai ResolveReceivableAccount saat state akad/disbursed.
	FinancingReceivableAccount string `json:"financing_receivable_account,omitempty"`
	// InterestRatePct: SEAM bunga cicilan (BS-6). WAJIB kosong/"0" saat ini —
	// jadwal zero-interest; komponen pembiayaan PSAK 72 belum dimodelkan.
	// TODO(tax-advisor): perlakuan pendapatan bunga cicilan inhouse (PPh/PPN).
	InterestRatePct string `json:"interest_rate_pct,omitempty"`
}

// ParseParams mem-parse JSON params (dari master atau snapshot kontrak).
func ParseParams(raw string) (Params, error) {
	var p Params
	if raw == "" {
		return p, fmt.Errorf("%w: params kosong", ErrInvalidParams)
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return p, fmt.Errorf("%w: %v", ErrInvalidParams, err)
	}
	return p, nil
}

// ── TermsSnapshot (hardening pasca-review Increment 3) ────────────────────────

// TermsSnapshot adalah amplop Contract Payment Terms Snapshot yang dibekukan ke
// sale_contracts.scheme_params_snapshot. Selain params, ia MEMBEKUKAN identitas
// strategy (policy_type) dan VERSINYA — sehingga perubahan algoritma policy di
// masa depan (registrasi versi baru) TIDAK mengubah interpretasi kontrak lama.
// Interpretasi kontrak sepenuhnya self-contained: tidak bergantung master aktif.
type TermsSnapshot struct {
	PolicyType    PolicyType `json:"policy_type"`
	PolicyVersion int        `json:"policy_version"`
	Params        Params     `json:"params"`
}

// NewTermsSnapshot membekukan (policy, params) menjadi amplop snapshot.
func NewTermsSnapshot(pol PaymentSchemePolicy, p Params) TermsSnapshot {
	return TermsSnapshot{PolicyType: pol.PolicyType(), PolicyVersion: pol.PolicyVersion(), Params: p}
}

// JSON meng-serialize amplop snapshot.
func (t TermsSnapshot) JSON() (string, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseTermsSnapshot mem-parse amplop snapshot. Kompatibel dengan snapshot
// format awal (bare Params tanpa amplop): policy_type kosong ⇒ pemanggil harus
// me-resolve policy dari master row kontrak (jalur legacy).
func ParseTermsSnapshot(raw string) (TermsSnapshot, error) {
	if raw == "" {
		return TermsSnapshot{}, fmt.Errorf("%w: snapshot kosong", ErrInvalidParams)
	}
	var t TermsSnapshot
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return TermsSnapshot{}, fmt.Errorf("%w: %v", ErrInvalidParams, err)
	}
	if t.PolicyType == "" {
		// Snapshot format awal: seluruh JSON adalah Params.
		p, err := ParseParams(raw)
		if err != nil {
			return TermsSnapshot{}, err
		}
		return TermsSnapshot{Params: p}, nil
	}
	return t, nil
}

// JSON meng-serialize Params (untuk snapshot kontrak).
func (p Params) JSON() (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// dpFraction mengembalikan (dp, sisa) sebagai bobot desimal — untuk Money.Allocate.
func (p Params) dpFraction() (decimal.Decimal, error) {
	if p.DPPercent == "" {
		return decimal.Zero, nil
	}
	d, err := decimal.NewFromString(p.DPPercent)
	if err != nil || d.IsNegative() || d.GreaterThan(decimal.NewFromInt(100)) {
		return decimal.Zero, fmt.Errorf("%w: dp_percent %q", ErrInvalidParams, p.DPPercent)
	}
	return d, nil
}

// DPAmount menghitung nominal DP dari GrossAmount via largest-remainder
// (Invariant #3: DP + sisa == gross persis; tanpa float).
func (p Params) DPAmount(gross domain.Money) (domain.Money, error) {
	dp, err := p.dpFraction()
	if err != nil {
		return domain.Zero, err
	}
	if dp.IsZero() {
		return domain.Zero, nil
	}
	parts := gross.Allocate([]decimal.Decimal{dp, decimal.NewFromInt(100).Sub(dp)})
	return parts[0], nil
}

// ReceivableOrDefault mengembalikan akun piutang buyer dari konfigurasi.
func (p Params) ReceivableOrDefault() string {
	if p.ReceivableAccount != "" {
		return p.ReceivableAccount
	}
	return DefaultReceivableAccount
}

// ── Facts (input policy — data, bukan dependensi) ─────────────────────────────

// ContractFacts adalah fakta kontrak yang dibutuhkan policy untuk validasi & plan.
type ContractFacts struct {
	GrossAmount        domain.Money // total tagihan buyer (incl. PPN utk PKP)
	ContractDate       time.Time
	HasFinancingSource bool
	LoanAmount         *domain.Money
}

// PaymentFacts adalah fakta pembayaran untuk gate BAST.
type PaymentFacts struct {
	TotalReceived domain.Money
	GrossAmount   domain.Money
}

// ── PlanItem (usulan jadwal — dikonversi sale package ke ScheduleItem) ────────

// PlanItemType mencerminkan sale.ScheduleType tanpa import sale (hindari cycle).
type PlanItemType string

const (
	PlanItemDP          PlanItemType = "dp"
	PlanItemInstallment PlanItemType = "installment"
	PlanItemFinal       PlanItemType = "final"
)

// PlanItem adalah satu baris usulan jadwal dari BuildSchedulePlan.
// Σ(Amount) == ContractFacts.GrossAmount PERSIS (Invariant #3).
type PlanItem struct {
	InstallmentNumber int
	DueDate           time.Time
	Amount            domain.Money
	Type              PlanItemType
}
