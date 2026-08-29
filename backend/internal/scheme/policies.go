package scheme

import "fmt"

// Empat strategy bawaan. State machine mengikuti desain §4
// (docs/increment-3-payment-scheme-design.md). Event `cancelled` tersedia dari
// semua state non-terminal sebagai TARGET transisi — eksekusi pembatalan
// (jurnal pembalik/refund/forfeit) adalah domain Cancellation (increment lain).

// cancellableFrom: state aktif yang boleh menuju cancelled.
func withCommonEvents(m map[Event]Transition, activeStates []State) map[Event]Transition {
	m[EventCancelled] = Transition{From: activeStates, To: StateCancelled}
	m[EventRescheduled] = Transition{From: activeStates, KeepState: true}
	return m
}

// ── CashPolicy — tunai keras ──────────────────────────────────────────────────

type CashPolicy struct{}

func (CashPolicy) PolicyType() PolicyType { return PolicyCash }

// PolicyVersion: naikkan bila algoritma berubah (interpretasi kontrak lama terjaga).
func (CashPolicy) PolicyVersion() int { return 1 }

func (CashPolicy) ValidateParams(p Params) error {
	if err := validateCommonParams(p); err != nil {
		return err
	}
	if p.InstallmentCount > 1 || p.TenorMonths > 0 {
		return fmt.Errorf("%w: cash tidak memakai tenor/cicilan", ErrInvalidParams)
	}
	if gateOrDefault(p, GateFullPayment) != GateFullPayment {
		return fmt.Errorf("%w: cash mensyaratkan bast_gate full_payment", ErrInvalidParams)
	}
	return nil
}

func (CashPolicy) ValidateContract(p Params, c ContractFacts) error {
	if c.HasFinancingSource {
		return ErrFinancingSourceNotAllowed
	}
	return nil
}

func (CashPolicy) BuildSchedulePlan(p Params, c ContractFacts) ([]PlanItem, error) {
	return []PlanItem{
		{InstallmentNumber: 1, DueDate: c.ContractDate, Amount: c.GrossAmount, Type: PlanItemFinal},
	}, nil
}

// Temuan #7: "Akad Jual Beli" (AJB) — analog Cash dari Akad Kredit KPR. Gate
// TIDAK berubah (tetap GateFullPayment, fakta pembayaran — bukan state) — yang
// berubah hanya penamaan aksi (Akad, bukan BAST) yang memicu jurnal. EventAkad
// diizinkan dari Signed maupun FullyPaid (defensif; gate sesungguhnya
// dievaluasi via CanRecognize berbasis fakta, bukan bergantung pada proyeksi
// state ini). HandedOver kini murni event fisik, bisa menyusul dari FullyPaid
// (bila proyeksi Akad tertinggal) atau dari Akad (jalur normal).
func (CashPolicy) AllowedEvents() map[Event]Transition {
	active := []State{StateSigned, StateAkad}
	return withCommonEvents(map[Event]Transition{
		EventFullyPaid:  {From: []State{StateSigned}, To: StateFullyPaid},
		EventAkad:       {From: []State{StateSigned, StateFullyPaid}, To: StateAkad},
		EventHandedOver: {From: []State{StateFullyPaid, StateAkad}, To: StateHandedOver},
	}, active)
}

func (CashPolicy) ResolveReceivableAccount(p Params, _ State) string {
	return p.ReceivableOrDefault()
}

func (CashPolicy) CanRecognize(p Params, s State, f PaymentFacts) error {
	return checkGate(gateOrDefault(p, GateFullPayment), p, s, f)
}

// ── CashInstallmentPolicy — tunai bertahap (cicilan pendek ke developer) ──────

type CashInstallmentPolicy struct{}

func (CashInstallmentPolicy) PolicyType() PolicyType { return PolicyCashInstallment }

// PolicyVersion: naikkan bila algoritma berubah (interpretasi kontrak lama terjaga).
func (CashInstallmentPolicy) PolicyVersion() int { return 1 }

func (CashInstallmentPolicy) ValidateParams(p Params) error {
	if err := validateCommonParams(p); err != nil {
		return err
	}
	if p.InstallmentCount < 1 || p.InstallmentCount > 24 {
		return fmt.Errorf("%w: installment_count harus 1..24", ErrInvalidParams)
	}
	return nil
}

func (CashInstallmentPolicy) ValidateContract(p Params, c ContractFacts) error {
	if c.HasFinancingSource {
		return ErrFinancingSourceNotAllowed
	}
	return nil
}

func (CashInstallmentPolicy) BuildSchedulePlan(p Params, c ContractFacts) ([]PlanItem, error) {
	dp, rest, err := splitDPAndRest(p, c.GrossAmount)
	if err != nil {
		return nil, err
	}
	var items []PlanItem
	num := 1
	if !dp.IsZero() {
		items = append(items, PlanItem{InstallmentNumber: num, DueDate: c.ContractDate, Amount: dp, Type: PlanItemDP})
		num++
	}
	parts := rest.Allocate(equalWeights(p.InstallmentCount))
	for i, amt := range parts {
		typ := PlanItemInstallment
		if i == len(parts)-1 {
			typ = PlanItemFinal
		}
		items = append(items, PlanItem{
			InstallmentNumber: num, DueDate: monthlyDue(c, i+1), Amount: amt, Type: typ,
		})
		num++
	}
	return items, nil
}

// Temuan #7: "Akad Jual Beli" analog — gate tetap GateFullPayment (fakta, bukan
// state). EventAkad diizinkan dari state manapun sebelum FullyPaid juga
// (defensif, sama alasan dengan CashPolicy).
func (CashInstallmentPolicy) AllowedEvents() map[Event]Transition {
	active := []State{StateSigned, StateDPPaid, StateInstallmentRunning, StateAkad}
	return withCommonEvents(map[Event]Transition{
		EventDPPaid:          {From: []State{StateSigned}, To: StateDPPaid},
		EventInstallmentPaid: {From: []State{StateDPPaid}, To: StateInstallmentRunning},
		EventFullyPaid:       {From: []State{StateSigned, StateDPPaid, StateInstallmentRunning}, To: StateFullyPaid},
		EventAkad:            {From: []State{StateSigned, StateDPPaid, StateInstallmentRunning, StateFullyPaid}, To: StateAkad},
		EventHandedOver:      {From: []State{StateFullyPaid, StateAkad}, To: StateHandedOver},
	}, active)
}

func (CashInstallmentPolicy) ResolveReceivableAccount(p Params, _ State) string {
	return p.ReceivableOrDefault()
}

func (CashInstallmentPolicy) CanRecognize(p Params, s State, f PaymentFacts) error {
	return checkGate(gateOrDefault(p, GateFullPayment), p, s, f)
}

// ── KPRPolicy — pembiayaan bank ───────────────────────────────────────────────

type KPRPolicy struct{}

func (KPRPolicy) PolicyType() PolicyType { return PolicyKPR }

// PolicyVersion: naikkan bila algoritma berubah (interpretasi kontrak lama terjaga).
func (KPRPolicy) PolicyVersion() int { return 1 }

func (KPRPolicy) ValidateParams(p Params) error {
	if err := validateCommonParams(p); err != nil {
		return err
	}
	if p.FinancingReceivableAccount == "" {
		return fmt.Errorf("%w: kpr wajib menyetel financing_receivable_account (mis. 1-2200)", ErrInvalidParams)
	}
	return nil
}

func (KPRPolicy) ValidateContract(p Params, c ContractFacts) error {
	if !c.HasFinancingSource {
		return ErrFinancingSourceRequired
	}
	if c.LoanAmount != nil && c.LoanAmount.Decimal().GreaterThan(c.GrossAmount.Decimal()) {
		return fmt.Errorf("%w: loan_amount melebihi nilai kontrak", ErrInvalidParams)
	}
	return nil
}

// Plan KPR: DP ke developer + pelunasan bank (sisa) pada target akad/pencairan.
func (KPRPolicy) BuildSchedulePlan(p Params, c ContractFacts) ([]PlanItem, error) {
	dp, rest, err := splitDPAndRest(p, c.GrossAmount)
	if err != nil {
		return nil, err
	}
	months := p.FinalDueMonths
	if months <= 0 {
		months = 3
	}
	var items []PlanItem
	num := 1
	if !dp.IsZero() {
		items = append(items, PlanItem{InstallmentNumber: num, DueDate: c.ContractDate, Amount: dp, Type: PlanItemDP})
		num++
	}
	items = append(items, PlanItem{
		InstallmentNumber: num, DueDate: monthlyDue(c, months), Amount: rest, Type: PlanItemFinal,
	})
	return items, nil
}

func (KPRPolicy) AllowedEvents() map[Event]Transition {
	active := []State{
		StateSigned, StateDPPaid, StateSubmittedToBank,
		StateBankApproved, StateBankRejected, StateAkad,
	}
	return withCommonEvents(map[Event]Transition{
		EventDPPaid:          {From: []State{StateSigned}, To: StateDPPaid},
		EventSubmittedToBank: {From: []State{StateSigned, StateDPPaid, StateBankRejected}, To: StateSubmittedToBank},
		EventBankApproved:    {From: []State{StateSubmittedToBank}, To: StateBankApproved},
		EventBankRejected:    {From: []State{StateSubmittedToBank}, To: StateBankRejected},
		EventAkad:            {From: []State{StateBankApproved}, To: StateAkad},
		// Pencairan: setelah akad (termasuk pasca-BAST — state tetap akad).
		EventDisbursed: {From: []State{StateAkad}, To: StateDisbursed},
		// Take-over/ganti bank pasca-komitmen: event tercatat, state tetap.
		EventTakeover: {From: []State{StateBankApproved, StateAkad, StateDisbursed}, KeepState: true},
		// Konversi scheme hanya masuk akal setelah bank menolak.
		EventConverted: {From: []State{StateBankRejected}, To: StateConverted},
		EventFullyPaid: {From: []State{StateAkad, StateDisbursed}, To: StateFullyPaid},
		// BAST pada KPR = EVENT TERCATAT TANPA memindah state (KeepState):
		// scheme_state KPR melacak lifecycle PEMBIAYAAN (`akad` adalah fakta
		// counterparty piutang — ResolveReceivableAccount bergantung padanya);
		// fakta serah terima sudah menjadi milik sale_records. Tanpa KeepState,
		// pencairan pasca-BAST akan salah ter-resolve ke piutang buyer padahal
		// bank belum mencairkan apa pun.
		EventHandedOver: {From: []State{StateAkad, StateDisbursed, StateFullyPaid}, KeepState: true},
	}, active)
}

// ResolveReceivableAccount (approval note #1) — SIAPA yang berutang atas sisa
// tagihan, per state. Kode akun dari KONFIGURASI (params), bukan hardcode.
//
// KEPUTUSAN KLIEN FINAL (T-3, 2026-08-05):
//
//	Antara akad dan pencairan, sisa tagihan adalah komitmen BANK → Piutang Bank
//	(FinancingReceivableAccount, mis. 1-2200). Begitu bank mencairkan, BANK
//	SELESAI PADA NILAI PENCAIRAN AKTUALNYA — selisih antara nilai kontrak dan
//	dana yang cair menjadi PIUTANG CUSTOMER (ReceivableAccount, 1-2000), bukan
//	piutang bank.
//
//	Contoh klien: harga 500jt, DP customer 50jt, bank cair 430jt →
//	Piutang Customer 20jt, Piutang Bank 0.
//
// Karena itu hanya StateAkad yang me-resolve ke akun pembiayaan. StateDisbursed
// dan seluruh state sesudahnya jatuh ke piutang buyer. Perpindahan akun ini
// otomatis memicu jurnal reklas (Dr 1-2000 / Cr 1-2200) lewat
// sale.reclassReceivableIfBAST pada event `disbursed` — sehingga tidak ada sisa
// yang menggantung di Piutang Bank, baik invoice KEKURANGAN diterbitkan maupun
// tidak.
func (KPRPolicy) ResolveReceivableAccount(p Params, s State) string {
	if s == StateAkad && p.FinancingReceivableAccount != "" {
		return p.FinancingReceivableAccount
	}
	return p.ReceivableOrDefault()
}

func (KPRPolicy) CanRecognize(p Params, s State, f PaymentFacts) error {
	return checkGate(gateOrDefault(p, GateAkad), p, s, f)
}

// ── InHousePolicy — cicilan jangka panjang ke developer ───────────────────────

type InHousePolicy struct{}

func (InHousePolicy) PolicyType() PolicyType { return PolicyInHouse }

// PolicyVersion: naikkan bila algoritma berubah (interpretasi kontrak lama terjaga).
func (InHousePolicy) PolicyVersion() int { return 1 }

func (InHousePolicy) ValidateParams(p Params) error {
	if err := validateCommonParams(p); err != nil {
		return err
	}
	if p.TenorMonths < 6 || p.TenorMonths > 120 {
		return fmt.Errorf("%w: tenor_months harus 6..120", ErrInvalidParams)
	}
	return nil
}

func (InHousePolicy) ValidateContract(p Params, c ContractFacts) error {
	if c.HasFinancingSource {
		return ErrFinancingSourceNotAllowed
	}
	return nil
}

func (InHousePolicy) BuildSchedulePlan(p Params, c ContractFacts) ([]PlanItem, error) {
	dp, rest, err := splitDPAndRest(p, c.GrossAmount)
	if err != nil {
		return nil, err
	}
	var items []PlanItem
	num := 1
	if !dp.IsZero() {
		items = append(items, PlanItem{InstallmentNumber: num, DueDate: c.ContractDate, Amount: dp, Type: PlanItemDP})
		num++
	}
	parts := rest.Allocate(equalWeights(p.TenorMonths))
	for i, amt := range parts {
		typ := PlanItemInstallment
		if i == len(parts)-1 {
			typ = PlanItemFinal
		}
		items = append(items, PlanItem{
			InstallmentNumber: num, DueDate: monthlyDue(c, i+1), Amount: amt, Type: typ,
		})
		num++
	}
	return items, nil
}

// Temuan #7: "Akad Jual Beli" analog — gate tetap GateDPPaid (fakta DP, bukan
// state), jatuh dini seperti sekarang (lihat catatan desain di plan). Akad
// disisipkan sebagai state nyata antara DPPaid dan InstallmentRunning/FullyPaid.
// EventHandedOver TIDAK diwajibkan lewat Akad dulu — tetap independen seperti
// desain asli InHouse (buyer bisa menghuni sambil mencicil sebelum Akad
// tercatat), ditambah StateAkad supaya proyeksi tetap jalan bila handover
// menyusul SETELAH Akad (kombinasi baru yang kini mungkin terjadi).
func (InHousePolicy) AllowedEvents() map[Event]Transition {
	active := []State{StateSigned, StateDPPaid, StateInstallmentRunning, StateAkad, StateHandedOver}
	return withCommonEvents(map[Event]Transition{
		EventDPPaid:          {From: []State{StateSigned}, To: StateDPPaid},
		EventInstallmentPaid: {From: []State{StateDPPaid, StateAkad}, To: StateInstallmentRunning},
		EventAkad:            {From: []State{StateDPPaid}, To: StateAkad},
		// BAST awal (gate dp_paid): buyer menghuni sambil mencicil — sisa jadi AR.
		EventHandedOver: {From: []State{StateDPPaid, StateInstallmentRunning, StateAkad, StateFullyPaid}, To: StateHandedOver},
		EventFullyPaid:  {From: []State{StateSigned, StateDPPaid, StateInstallmentRunning, StateAkad, StateHandedOver}, To: StateFullyPaid},
	}, active)
}

func (InHousePolicy) ResolveReceivableAccount(p Params, _ State) string {
	return p.ReceivableOrDefault()
}

func (InHousePolicy) CanRecognize(p Params, s State, f PaymentFacts) error {
	return checkGate(gateOrDefault(p, GateDPPaid), p, s, f)
}

// ── Guard kompilasi: keempat policy memenuhi interface ────────────────────────

var (
	_ PaymentSchemePolicy = CashPolicy{}
	_ PaymentSchemePolicy = CashInstallmentPolicy{}
	_ PaymentSchemePolicy = KPRPolicy{}
	_ PaymentSchemePolicy = InHousePolicy{}
)
