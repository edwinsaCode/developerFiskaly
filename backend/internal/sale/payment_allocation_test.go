package sale_test

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// FE-2 · P2 — dual-write payment_allocations (in-memory contract tests).
// Rollback SQL nyata dibuktikan terpisah di payment_allocation_integration_test.go
// (//go:build integration). Test di sini menjalankan committer default (non-atomik,
// in-memory) + stagingCommitter untuk kontrak all-or-nothing.

// ── 1. Partial payment ke SATU cicilan (jalur ScheduleID) ────────────────────

func TestAllocation_PartialPayment_SingleSchedule(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, cs, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 500_000_000), sched(2, 2, 500_000_000)})

	sid := uint64(1)
	res, err := svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceScheduleReceived,
		ScheduleID:      &sid,
		Amount:          domain.FromInt(300_000_000),
		Date:            collectionDate(),
		BankAccountCode: "1-1300",
	})
	if err != nil {
		t.Fatalf("ReceivePayment: %v", err)
	}

	// Cache paid_amount konsisten.
	if cs.SchedulePaidAmount(1) != "300000000" {
		t.Errorf("paid_amount cicilan 1: got %s, want 300000000", cs.SchedulePaidAmount(1))
	}
	// Tepat satu baris alokasi 'schedule' 300jt ke cicilan 1; tidak ada buyer_credit.
	allocs := cs.Allocations()
	if len(allocs) != 1 {
		t.Fatalf("harus 1 baris alokasi, got %d: %+v", len(allocs), allocs)
	}
	a := allocs[0]
	if a.Type != sale.AllocationTypeSchedule || a.Amount != "300000000" ||
		a.ScheduleID == nil || *a.ScheduleID != 1 || a.TerminPaymentID != res.TerminID {
		t.Errorf("baris alokasi salah: %+v (terminID=%d)", a, res.TerminID)
	}
	// Termin memang tercatat.
	if ts.TerminCount() != 1 {
		t.Errorf("harus 1 termin, got %d", ts.TerminCount())
	}
}

// ── 2. Satu pembayaran waterfall ke BEBERAPA cicilan (jalur ContractID) ───────

func TestAllocation_Waterfall_MultipleSchedules(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, cs, _ := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 500_000_000), sched(2, 2, 500_000_000)})

	// 800jt: menutup cicilan 1 (500jt penuh) + cicilan 2 (300jt partial).
	if _, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 800_000_000, "")); err != nil {
		t.Fatalf("RecordCollectionPayment: %v", err)
	}

	allocs := cs.Allocations()
	if len(allocs) != 2 {
		t.Fatalf("harus 2 baris alokasi (waterfall), got %d: %+v", len(allocs), allocs)
	}
	// Urutan waterfall: cicilan 1 penuh, cicilan 2 partial.
	if allocs[0].Amount != "500000000" || allocs[0].ScheduleID == nil || *allocs[0].ScheduleID != 1 {
		t.Errorf("alokasi #1 harus 500jt ke cicilan 1: %+v", allocs[0])
	}
	if allocs[1].Amount != "300000000" || allocs[1].ScheduleID == nil || *allocs[1].ScheduleID != 2 {
		t.Errorf("alokasi #2 harus 300jt ke cicilan 2: %+v", allocs[1])
	}
	// Semua baris dari termin yang sama (Σ alokasi == termin.amount).
	if !allocsSumTo(allocs, "800000000") {
		t.Errorf("Σ alokasi harus 800000000, got %s", allocsSum(allocs))
	}
	if cs.SchedulePaidAmount(1) != "500000000" || cs.SchedulePaidAmount(2) != "300000000" {
		t.Errorf("cache paid_amount tidak konsisten: c1=%s c2=%s", cs.SchedulePaidAmount(1), cs.SchedulePaidAmount(2))
	}
}

// ── 3. Overpay pra-BAST → baris buyer_credit ─────────────────────────────────

func TestAllocation_OverpayBeforeBAST_BuyerCredit(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, cs, _ := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})

	res, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 1_200_000_000, ""))
	if err != nil {
		t.Fatalf("overpay pra-BAST harus diterima: %v", err)
	}
	if res.BuyerCredit != "200000000" {
		t.Errorf("BuyerCredit: got %s, want 200000000", res.BuyerCredit)
	}

	allocs := cs.Allocations()
	if len(allocs) != 2 {
		t.Fatalf("harus 2 baris (1 schedule + 1 buyer_credit), got %d: %+v", len(allocs), allocs)
	}
	var sawSchedule, sawCredit bool
	for _, a := range allocs {
		switch a.Type {
		case sale.AllocationTypeSchedule:
			sawSchedule = a.Amount == "1000000000" && a.ScheduleID != nil && *a.ScheduleID == 1
		case sale.AllocationTypeBuyerCredit:
			sawCredit = a.Amount == "200000000" && a.ScheduleID == nil
		}
	}
	if !sawSchedule {
		t.Errorf("baris schedule 1jt tidak ditemukan: %+v", allocs)
	}
	if !sawCredit {
		t.Errorf("baris buyer_credit 200jt (schedule NULL) tidak ditemukan: %+v", allocs)
	}
	// Σ alokasi == termin.amount (konservasi, Invariant #3 spirit).
	if !allocsSumTo(allocs, "1200000000") {
		t.Errorf("Σ alokasi harus 1200000000, got %s", allocsSum(allocs))
	}
}

// ── 3b. Sisa tak-berjadwal PASCA-BAST → baris 'direct', BUKAN buyer_credit ───
//
// Regresi: kontrak tanpa jadwal cicilan (schedule opsional, requirement F) yang
// sudah BAST. Sisa pembayaran yang tak cocok jadwal manapun sudah mengurangi
// piutang riil (1-2000) lewat jurnal — menandainya buyer_credit akan menghitung
// uang yang sama dua kali (piutang sudah lunas, TAPI "tersedia lagi" sbg saldo
// kredit yang bisa dipakai ulang via credit_applications).

func TestAllocation_PostBASTUnscheduled_DirectNotBuyerCredit(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, cs, ts := setupCollection(tenant, contract, unit, 500_000_000, "sold", nil)
	ts.SetSaleRecord(&sale.SaleRecord{ID: 1, TenantID: tenant, UnitID: unit, SalePrice: domain.FromInt(500_000_000)}, tenant)

	res, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 500_000_000, ""))
	if err != nil {
		t.Fatalf("RecordCollectionPayment pasca-BAST tanpa jadwal: %v", err)
	}

	allocs := cs.Allocations()
	if len(allocs) != 1 {
		t.Fatalf("harus 1 baris alokasi (schedule NULL), got %d: %+v", len(allocs), allocs)
	}
	a := allocs[0]
	if a.Type != sale.AllocationTypeDirect || a.ScheduleID != nil || a.Amount != "500000000" {
		t.Errorf("baris alokasi salah: got %+v, want Type=direct ScheduleID=nil Amount=500000000", a)
	}
	// Σ alokasi == termin.amount (Invariant #3: tak pernah dibuang).
	if !allocsSumTo(allocs, "500000000") {
		t.Errorf("Σ alokasi harus 500000000, got %s", allocsSum(allocs))
	}
	// Bukan kelebihan bayar: BuyerCredit hasil operasi harus nol.
	if res.BuyerCredit != "" && res.BuyerCredit != "0" {
		t.Errorf("BuyerCredit hasil operasi harus 0/kosong, got %q", res.BuyerCredit)
	}
	// Saldo kredit kanonik TIDAK boleh ikut naik akibat baris 'direct' ini.
	bc, err := cs.GetBuyerCredit(context.Background(), tenant, unit)
	if err != nil {
		t.Fatalf("GetBuyerCredit: %v", err)
	}
	if bc.Available != "0" {
		t.Errorf("saldo kredit buyer harus tetap 0 (bukan buyer_credit), got %s", bc.Available)
	}
}

// ── 4. Duplicate retry (idempotency) tidak membuat alokasi kedua ─────────────

func TestAllocation_DuplicateRetry_NoSecondAllocation(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	svc, cs, ts := setupCollection(tenant, contract, unit, 1_000_000_000, "reserved",
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})

	key := "idem-alloc-1"
	if _, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 400_000_000, key)); err != nil {
		t.Fatalf("submit #1: %v", err)
	}
	afterFirst := len(cs.Allocations())
	if afterFirst != 1 {
		t.Fatalf("submit #1 harus menulis 1 alokasi, got %d", afterFirst)
	}

	r2, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 400_000_000, key))
	if err != nil {
		t.Fatalf("submit #2: %v", err)
	}
	if !r2.AlreadyExisted {
		t.Error("submit #2 harus dikenali sebagai duplikat")
	}
	if got := len(cs.Allocations()); got != afterFirst {
		t.Errorf("retry idempoten tidak boleh menambah alokasi: got %d, want %d", got, afterFirst)
	}
	if ts.TerminCount() != 1 {
		t.Errorf("harus tetap 1 termin, got %d", ts.TerminCount())
	}
}

// ── 5. Failure injection: alokasi gagal → tidak ada yang dipersist ───────────
//
// Kontrak all-or-nothing (in-memory). Rollback SQL nyata diuji di integration
// test (//go:build integration). stagingCommitter meniru urutan committer nyata
// (jurnal → termin → alokasi) namun hanya "commit" ke state bila SEMUA sukses.

var errInjected = errors.New("injeksi kegagalan alokasi")

type stagingCommitter struct {
	failOn      string // "journal" | "allocation" | "receipt" | ""
	termins     int
	journals    int
	allocations []recordedAllocation
}

func (c *stagingCommitter) CommitPayment(_ context.Context, _ uint64, p sale.PaymentCommitParams) (*sale.PaymentCommitResult, error) {
	// Stage jurnal.
	if c.failOn == "journal" {
		return nil, errInjected
	}
	// Stage satu alokasi sintetik untuk seluruh amount (cukup untuk uji kontrak
	// all-or-nothing; committer nyata yang merencanakan alokasi sesungguhnya).
	staged := []recordedAllocation{{Type: sale.AllocationTypeSchedule, Amount: p.Amount.String()}}
	if c.failOn == "allocation" {
		return nil, errInjected // DISCARD: tidak ada mutasi state
	}
	if c.failOn == "receipt" {
		return nil, errInjected
	}
	// Commit atomik (in-memory).
	c.journals++
	c.termins++
	c.allocations = append(c.allocations, staged...)
	return &sale.PaymentCommitResult{TerminID: uint64(c.termins), JournalID: uint64(c.journals)}, nil
}

func TestAllocation_CommitFailure_AllOrNothing(t *testing.T) {
	const tenant, contract, unit = uint64(1), uint64(10), uint64(100)
	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenant, contract, unit, domain.FromInt(1_000_000_000),
		[]*sale.PaymentSchedule{sched(1, 1, 1_000_000_000)})
	units := map[uint64]*sale.UnitSaleInfo{unit: {ID: unit, ProjectID: 9, Status: "reserved"}}

	committer := &stagingCommitter{failOn: "allocation"}
	svc, ts := buildServiceInjectedCommitter(cs, units, committer)

	_, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 400_000_000, ""))
	if !errors.Is(err, errInjected) {
		t.Fatalf("harus gagal dengan errInjected, got %v", err)
	}
	// Tidak ada jurnal / termin / alokasi yang ter-commit (guard #4).
	if committer.journals != 0 {
		t.Errorf("jurnal tidak boleh ter-commit saat alokasi gagal, got %d", committer.journals)
	}
	if committer.termins != 0 {
		t.Errorf("termin tidak boleh ter-commit saat alokasi gagal, got %d", committer.termins)
	}
	if len(committer.allocations) != 0 {
		t.Errorf("alokasi tidak boleh ter-commit, got %d", len(committer.allocations))
	}
	// Service tidak menulis apa pun di luar committer.
	if ts.TerminCount() != 0 {
		t.Errorf("service tidak boleh menyimpan termin di luar committer, got %d", ts.TerminCount())
	}

	// Sanity: tanpa injeksi, commit sukses menulis state.
	committer.failOn = ""
	if _, err := svc.RecordCollectionPayment(context.Background(), tenant, collReq(contract, 400_000_000, "")); err != nil {
		t.Fatalf("commit sukses: %v", err)
	}
	if committer.termins != 1 || committer.journals != 1 || len(committer.allocations) != 1 {
		t.Errorf("commit sukses harus menulis 1/1/1, got t=%d j=%d a=%d",
			committer.termins, committer.journals, len(committer.allocations))
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func buildServiceInjectedCommitter(cs *mockContractStore, units map[uint64]*sale.UnitSaleInfo, committer sale.PaymentCommitter) (*sale.Service, *mockTerminStore) {
	af := &mockAccountFinder{accounts: standardAccounts}
	jw := &mockJournalWriter{}
	ur := &mockUnitReader{units: units}
	ts := &mockTerminStore{}
	cp := &mockUnitCostProvider{}
	bw := &mockBASTWriter{}
	svc := sale.NewService(af, jw, ur, ts, cp, bw, sale.WithContractStore(cs), sale.WithPaymentCommitter(committer))
	return svc, ts
}

func allocsSum(allocs []recordedAllocation) string {
	sum := domain.Zero
	for _, a := range allocs {
		m, _ := domain.NewMoney(a.Amount)
		sum = sum.Add(m)
	}
	return sum.String()
}

func allocsSumTo(allocs []recordedAllocation, want string) bool {
	return allocsSum(allocs) == want
}
