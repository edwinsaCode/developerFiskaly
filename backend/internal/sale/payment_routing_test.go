package sale_test

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// GAP-1 routing, sekarang diuji lewat ReceivePayment (pintu masuk tunggal).
// Kredit Uang Muka (2-2000) sebelum BAST, Piutang (1-2000) sesudahnya.

func buildRoutingService(units map[uint64]*sale.UnitSaleInfo, ts *mockTerminStore) (*sale.Service, *mockJournalWriter) {
	af := &mockAccountFinder{accounts: standardAccounts}
	jw := &mockJournalWriter{}
	ur := &mockUnitReader{units: units}
	cp := &mockUnitCostProvider{}
	bw := &mockBASTWriter{}
	// Advance unit_termin: tanpa kontrak; ContractStore kosong (FindContractByUnitID → not found).
	svc := sale.NewService(af, jw, ur, ts, cp, bw, sale.WithContractStore(newMockContractStore()))
	return svc, jw
}

func creditLine(lines []sale.JournalLineInput) (sale.JournalLineInput, bool) {
	for _, l := range lines {
		if l.Credit.GreaterThan(domain.Zero) {
			return l, true
		}
	}
	return sale.JournalLineInput{}, false
}

func assertLinesBalanced(t *testing.T, lines []sale.JournalLineInput) {
	t.Helper()
	var d, c domain.Money
	for _, l := range lines {
		d = d.Add(l.Debit)
		c = c.Add(l.Credit)
	}
	if !d.Equal(c) {
		t.Errorf("jurnal tidak balanced: Σdebit=%s Σkredit=%s", d.String(), c.String())
	}
}

// recvAdvance mencatat penerimaan advance unit (source=unit_termin) via ReceivePayment.
func recvAdvance(svc *sale.Service, tenant, unitID uint64, amount int64) (*sale.ReceivePaymentResult, error) {
	uid := unitID
	return svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: "1-1300",
		Amount:          domain.FromInt(amount),
		Date:            time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	})
}

func saleRecordFor(tenantID, unitID uint64, dpp int64) *sale.SaleRecord {
	return &sale.SaleRecord{ID: 1, TenantID: tenantID, UnitID: unitID, SalePrice: domain.FromInt(dpp), IsVAT: false}
}

// ── Before BAST → Cr Uang Muka ───────────────────────────────────────────────

func TestPaymentRouting_BeforeBAST_CreditsUangMuka(t *testing.T) {
	const tenantID, unitID = uint64(1), uint64(100)
	ts := &mockTerminStore{}
	svc, jw := buildRoutingService(map[uint64]*sale.UnitSaleInfo{
		unitID: {ID: unitID, ProjectID: 9, Status: "reserved"},
	}, ts)

	res, err := recvAdvance(svc, tenantID, unitID, 250_000_000)
	if err != nil {
		t.Fatalf("ReceivePayment: %v", err)
	}
	if res.CreditAccount != "2-2000" {
		t.Errorf("CreditAccount: got %s, want 2-2000 (Uang Muka)", res.CreditAccount)
	}
	cl, ok := creditLine(jw.lastLines)
	if !ok || cl.AccountID != accUMP {
		t.Errorf("baris kredit harus akun Uang Muka (%d), got %+v", accUMP, cl)
	}
	assertLinesBalanced(t, jw.lastLines)
}

// ── After BAST → Cr Piutang ──────────────────────────────────────────────────

func TestPaymentRouting_AfterBAST_CreditsPiutang(t *testing.T) {
	const tenantID, unitID = uint64(1), uint64(100)
	ts := &mockTerminStore{
		termins:          []*sale.TerminPayment{{TenantID: tenantID, UnitID: unitID, Amount: domain.FromInt(400_000_000)}},
		saleRecord:       saleRecordFor(tenantID, unitID, 1_000_000_000),
		saleRecordTenant: tenantID,
	}
	svc, jw := buildRoutingService(map[uint64]*sale.UnitSaleInfo{
		unitID: {ID: unitID, ProjectID: 9, Status: "sold"},
	}, ts)

	res, err := recvAdvance(svc, tenantID, unitID, 200_000_000)
	if err != nil {
		t.Fatalf("ReceivePayment pasca-BAST: %v", err)
	}
	if res.CreditAccount != "1-2000" {
		t.Errorf("CreditAccount: got %s, want 1-2000 (Piutang)", res.CreditAccount)
	}
	cl, ok := creditLine(jw.lastLines)
	if !ok || cl.AccountID != accPiutang {
		t.Errorf("baris kredit harus akun Piutang (%d), got %+v", accPiutang, cl)
	}
	assertLinesBalanced(t, jw.lastLines)
}

func TestPaymentRouting_AfterBAST_OverpaymentRejected(t *testing.T) {
	const tenantID, unitID = uint64(1), uint64(100)
	ts := &mockTerminStore{
		termins:          []*sale.TerminPayment{{TenantID: tenantID, UnitID: unitID, Amount: domain.FromInt(900_000_000)}},
		saleRecord:       saleRecordFor(tenantID, unitID, 1_000_000_000),
		saleRecordTenant: tenantID,
	}
	svc, _ := buildRoutingService(map[uint64]*sale.UnitSaleInfo{
		unitID: {ID: unitID, ProjectID: 9, Status: "sold"},
	}, ts)

	_, err := recvAdvance(svc, tenantID, unitID, 200_000_000)
	if err != sale.ErrPaymentExceedsReceivable {
		t.Errorf("expected ErrPaymentExceedsReceivable, got %v", err)
	}
}

// ── Mixed before/after BAST ──────────────────────────────────────────────────

func TestPaymentRouting_Mixed_BeforeThenAfterBAST(t *testing.T) {
	const tenantID, unitID = uint64(1), uint64(100)
	units := map[uint64]*sale.UnitSaleInfo{unitID: {ID: unitID, ProjectID: 9, Status: "reserved"}}
	ts := &mockTerminStore{}
	svc, _ := buildRoutingService(units, ts)

	r1, err := recvAdvance(svc, tenantID, unitID, 400_000_000)
	if err != nil {
		t.Fatalf("pembayaran pra-BAST: %v", err)
	}
	if r1.CreditAccount != "2-2000" {
		t.Errorf("pembayaran pra-BAST harus Uang Muka, got %s", r1.CreditAccount)
	}

	ts.saleRecord = saleRecordFor(tenantID, unitID, 1_000_000_000)
	ts.saleRecordTenant = tenantID
	units[unitID].Status = "sold"

	r2, err := recvAdvance(svc, tenantID, unitID, 600_000_000)
	if err != nil {
		t.Fatalf("pembayaran pasca-BAST: %v", err)
	}
	if r2.CreditAccount != "1-2000" {
		t.Errorf("pembayaran pasca-BAST harus Piutang, got %s", r2.CreditAccount)
	}

	if len(ts.termins) != 2 {
		t.Fatalf("harus 2 termin tersimpan, got %d", len(ts.termins))
	}
	if ts.termins[0].CreditAccountCode != "2-2000" || ts.termins[1].CreditAccountCode != "1-2000" {
		t.Errorf("urutan akun kredit salah: %s lalu %s (want 2-2000 lalu 1-2000)",
			ts.termins[0].CreditAccountCode, ts.termins[1].CreditAccountCode)
	}
}

// ── Tenant isolation ─────────────────────────────────────────────────────────

func TestPaymentRouting_TenantIsolation(t *testing.T) {
	const tenantA, tenantB, unitID = uint64(1), uint64(2), uint64(100)
	ts := &mockTerminStore{
		saleRecord:       saleRecordFor(tenantA, unitID, 1_000_000_000),
		saleRecordTenant: tenantA,
	}
	units := map[uint64]*sale.UnitSaleInfo{unitID: {ID: unitID, ProjectID: 9, Status: "reserved"}}
	svc, _ := buildRoutingService(units, ts)

	tb, err := recvAdvance(svc, tenantB, unitID, 100_000_000)
	if err != nil {
		t.Fatalf("ReceivePayment tenant B: %v", err)
	}
	if tb.CreditAccount != "2-2000" {
		t.Errorf("tenant B tidak boleh mewarisi status BAST tenant A; got %s, want 2-2000", tb.CreditAccount)
	}

	ta, err := recvAdvance(svc, tenantA, unitID, 100_000_000)
	if err != nil {
		t.Fatalf("ReceivePayment tenant A: %v", err)
	}
	if ta.CreditAccount != "1-2000" {
		t.Errorf("tenant A pasca-BAST harus Piutang; got %s", ta.CreditAccount)
	}
}

// ── Ledger balance invariant ─────────────────────────────────────────────────

func TestPaymentRouting_LedgerBalanceInvariant(t *testing.T) {
	const tenantID, unitID = uint64(1), uint64(100)

	tsPre := &mockTerminStore{}
	svcPre, jwPre := buildRoutingService(map[uint64]*sale.UnitSaleInfo{
		unitID: {ID: unitID, ProjectID: 9, Status: "reserved"},
	}, tsPre)
	if _, err := recvAdvance(svcPre, tenantID, unitID, 250_000_000); err != nil {
		t.Fatalf("pra-BAST: %v", err)
	}
	assertLinesBalanced(t, jwPre.lastLines)
	if len(jwPre.lastLines) != 2 {
		t.Errorf("jurnal pra-BAST harus 2 baris, got %d", len(jwPre.lastLines))
	}

	tsPost := &mockTerminStore{
		saleRecord:       saleRecordFor(tenantID, unitID, 1_000_000_000),
		saleRecordTenant: tenantID,
	}
	svcPost, jwPost := buildRoutingService(map[uint64]*sale.UnitSaleInfo{
		unitID: {ID: unitID, ProjectID: 9, Status: "sold"},
	}, tsPost)
	if _, err := recvAdvance(svcPost, tenantID, unitID, 300_000_000); err != nil {
		t.Fatalf("pasca-BAST: %v", err)
	}
	assertLinesBalanced(t, jwPost.lastLines)
	if len(jwPost.lastLines) != 2 {
		t.Errorf("jurnal pasca-BAST harus 2 baris, got %d", len(jwPost.lastLines))
	}
}
