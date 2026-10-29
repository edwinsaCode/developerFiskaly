package sale_test

// Audit kwitansi Kelebihan Tanah (2026-10-29): skenario klien persis — harga
// unit 185jt + Kelebihan Tanah 45jt, DP 6jt → sisa harga unit 179jt. Bayar
// Kelebihan Tanah 45jt SEBELUM Akad: sisa harga unit HARUS tetap 179jt, sisa
// Kelebihan Tanah menjadi 0 (bug lama: sisa unit turun jadi 134jt).

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

const (
	landTenant   = uint64(1)
	landContract = uint64(10)
	landUnit     = uint64(100)
)

// setupLandContract: kontrak unit 185jt (DP 6jt + pelunasan 179jt), dengan
// komponen Kelebihan Tanah 90 m² x Rp500rb = 45jt bila withLand.
func setupLandContract(t *testing.T, withLand bool) (*sale.Service, *mockContractStore, *mockTerminStore) {
	t.Helper()
	dp := &sale.PaymentSchedule{ID: 1, InstallmentNumber: 0, Amount: domain.FromInt(6_000_000),
		Type: sale.ScheduleTypeDP, Status: sale.ScheduleStatusScheduled, DueDate: collectionDate()}
	final := &sale.PaymentSchedule{ID: 2, InstallmentNumber: 99, Amount: domain.FromInt(179_000_000),
		Type: sale.ScheduleTypeFinal, Status: sale.ScheduleStatusScheduled, DueDate: collectionDate()}
	svc, cs, ts := setupCollection(landTenant, landContract, landUnit, 185_000_000, "reserved",
		[]*sale.PaymentSchedule{dp, final})
	if withLand {
		c := cs.contracts[landContract]
		qty := decimal.RequireFromString("90")
		price := domain.FromInt(500_000)
		stockID := uint64(3)
		c.LandQuantityM2 = &qty
		c.LandUnitPriceSnapshot = &price
		c.LandStockID = &stockID
	}
	return svc, cs, ts
}

func payLand(svc *sale.Service, amount int64, key string) (*sale.ReceivePaymentResult, error) {
	cid := landContract
	return svc.ReceivePayment(context.Background(), landTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceUnitTermin, ContractID: &cid, Amount: domain.FromInt(amount),
		Date: collectionDate(), BankAccountCode: "1-1300", Kind: sale.TerminKindLandExcess,
		IdempotencyKey: key,
	})
}

func payDP(t *testing.T, svc *sale.Service) {
	t.Helper()
	cid, sid := landContract, uint64(1)
	if _, err := svc.ReceivePayment(context.Background(), landTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceUnitTermin, ContractID: &cid, ScheduleID: &sid,
		Amount: domain.FromInt(6_000_000), Date: collectionDate(), BankAccountCode: "1-1300",
		IdempotencyKey: "dp",
	}); err != nil {
		t.Fatalf("bayar DP: %v", err)
	}
}

func TestLandExcess_PreAkad_DoesNotReduceHousePrice(t *testing.T) {
	svc, cs, ts := setupLandContract(t, true)
	payDP(t, svc)

	if _, err := payLand(svc, 45_000_000, "land"); err != nil {
		t.Fatalf("bayar Kelebihan Tanah: %v", err)
	}
	got := ts.termins[len(ts.termins)-1]
	if got.Kind != sale.TerminKindLandExcess || got.CountsTowardPrice {
		t.Errorf("termin tanah: Kind=%q CountsTowardPrice=%v, want land_excess/false", got.Kind, got.CountsTowardPrice)
	}
	if paid := cs.SchedulePaidAmount(2); paid != "0" && paid != "0.0000" && paid != "" {
		t.Errorf("cicilan pelunasan RUMAH tersentuh pembayaran tanah: paid=%s", paid)
	}

	sum, err := svc.ContractFinancialSummaryByID(context.Background(), landTenant, landContract)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	checks := []struct {
		name      string
		got, want domain.Money
	}{
		{"Outstanding (unit)", sum.Outstanding, domain.FromInt(179_000_000)},
		{"HouseOutstanding", sum.HouseOutstanding, domain.FromInt(179_000_000)},
		{"LandPaid", sum.LandPaid, domain.FromInt(45_000_000)},
		{"LandOutstanding", sum.LandOutstanding, domain.Zero},
		{"TotalOutstanding (unit+tanah)", sum.TotalOutstanding, domain.FromInt(179_000_000)},
	}
	for _, c := range checks {
		if !c.got.Equal(c.want) {
			t.Errorf("%s: got %s, want %s", c.name, c.got, c.want)
		}
	}
}

func TestLandExcess_PreAkad_PartialSplit(t *testing.T) {
	svc, _, _ := setupLandContract(t, true)
	payDP(t, svc)
	if _, err := payLand(svc, 20_000_000, "land-1"); err != nil {
		t.Fatalf("bayar tanah sebagian: %v", err)
	}
	sum, err := svc.ContractFinancialSummaryByID(context.Background(), landTenant, landContract)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !sum.HouseOutstanding.Equal(domain.FromInt(179_000_000)) {
		t.Errorf("HouseOutstanding: got %s, want 179jt", sum.HouseOutstanding)
	}
	if !sum.LandOutstanding.Equal(domain.FromInt(25_000_000)) {
		t.Errorf("LandOutstanding: got %s, want 25jt (45 − 20)", sum.LandOutstanding)
	}
	// Σ bagian == total: unit + tanah = sisa gabungan, persis.
	if !sum.HouseOutstanding.Add(sum.LandOutstanding).Equal(sum.TotalOutstanding) {
		t.Errorf("unit %s + tanah %s != total %s", sum.HouseOutstanding, sum.LandOutstanding, sum.TotalOutstanding)
	}
}

func TestLandExcess_PreAkad_RejectsOverpayment(t *testing.T) {
	svc, _, ts := setupLandContract(t, true)
	if _, err := payLand(svc, 46_000_000, "over"); !errors.Is(err, sale.ErrLandPaymentExceedsOutstanding) {
		t.Fatalf("46jt > tagihan tanah 45jt: err=%v, want ErrLandPaymentExceedsOutstanding", err)
	}
	if _, err := payLand(svc, 45_000_000, "full"); err != nil {
		t.Fatalf("bayar tanah lunas: %v", err)
	}
	n := len(ts.termins)
	if _, err := payLand(svc, 1, "extra"); !errors.Is(err, sale.ErrLandPaymentExceedsOutstanding) {
		t.Fatalf("tanah sudah lunas: err=%v, want ErrLandPaymentExceedsOutstanding", err)
	}
	if len(ts.termins) != n {
		t.Error("pembayaran yang ditolak tidak boleh menyimpan termin")
	}
}

func TestLandExcess_NoLandComponent_Rejected(t *testing.T) {
	svc, _, ts := setupLandContract(t, false)
	if _, err := payLand(svc, 45_000_000, "no-land"); !errors.Is(err, sale.ErrLandComponentMissing) {
		t.Fatalf("kontrak tanpa tanah: err=%v, want ErrLandComponentMissing", err)
	}
	if len(ts.termins) != 0 {
		t.Error("pembayaran yang ditolak tidak boleh menyimpan termin")
	}
}

// Pratinjau HARUS sama dengan submit: land_excess pra-Akad tidak boleh
// menampilkan alokasi ke cicilan rumah.
func TestLandExcess_PreAkad_PreviewMatchesSubmit(t *testing.T) {
	svc, _, _ := setupLandContract(t, true)
	payDP(t, svc)

	p, err := svc.PreviewCollectionPaymentKind(context.Background(), landTenant, landContract,
		domain.FromInt(45_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection, sale.TerminKindLandExcess)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !p.Valid || !p.LandAdvance {
		t.Fatalf("preview: Valid=%v LandAdvance=%v reason=%q, want true/true", p.Valid, p.LandAdvance, p.Reason)
	}
	if p.Outstanding != "45000000" {
		t.Errorf("Outstanding pratinjau = %s, want 45000000 (sisa Kelebihan Tanah, bukan sisa unit)", p.Outstanding)
	}
	for _, a := range p.Applied {
		if a.ScheduleID != 0 {
			t.Errorf("pratinjau mengalokasikan ke cicilan rumah #%d (%s)", a.ScheduleID, a.Amount)
		}
	}
	if p.OverpaymentUnapplied != "0" {
		t.Errorf("OverpaymentUnapplied = %s, want 0 (bukan kelebihan bayar)", p.OverpaymentUnapplied)
	}

	over, err := svc.PreviewCollectionPaymentKind(context.Background(), landTenant, landContract,
		domain.FromInt(46_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection, sale.TerminKindLandExcess)
	if err != nil {
		t.Fatalf("preview over: %v", err)
	}
	if over.Valid || over.Reason != sale.ErrLandPaymentExceedsOutstanding.Error() {
		t.Errorf("preview 46jt: Valid=%v Reason=%q, want invalid ErrLandPaymentExceedsOutstanding", over.Valid, over.Reason)
	}

	noLandSvc, _, _ := setupLandContract(t, false)
	nl, err := noLandSvc.PreviewCollectionPaymentKind(context.Background(), landTenant, landContract,
		domain.FromInt(1_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection, sale.TerminKindLandExcess)
	if err != nil {
		t.Fatalf("preview tanpa tanah: %v", err)
	}
	if nl.Valid || nl.Reason != sale.ErrLandComponentMissing.Error() {
		t.Errorf("preview tanpa tanah: Valid=%v Reason=%q, want invalid ErrLandComponentMissing", nl.Valid, nl.Reason)
	}
}

// Kondisi produksi unit klien (kontrak 19): TANPA baris cicilan rumah — sisa
// harga unit dihitung dari harga kontrak − termin yang mengurangi harga.
// Data diperbaiki (counts_toward_price=FALSE) → 179jt / tanah 0.
func TestLandExcess_PreAkad_NoHouseSchedules(t *testing.T) {
	svc, cs, ts := setupCollection(landTenant, landContract, landUnit, 185_000_000, "reserved", nil)
	c := cs.contracts[landContract]
	qty := decimal.RequireFromString("30")
	price := domain.FromInt(1_500_000)
	stockID := uint64(5)
	c.LandQuantityM2, c.LandUnitPriceSnapshot, c.LandStockID = &qty, &price, &stockID

	// Termin 219 (other, counts=FALSE) di produksi tidak dimasukkan: repo asli
	// hanya menjumlah counts=TRUE, tetapi mock menghitung flag zero-value
	// (fixture lama) — bukan bagian dari logika tanah yang diuji di sini.
	ts.termins = append(ts.termins,
		&sale.TerminPayment{ID: 346, TenantID: landTenant, UnitID: landUnit, Amount: domain.FromInt(6_000_000), Kind: "dp", CountsTowardPrice: true},
		&sale.TerminPayment{ID: 347, TenantID: landTenant, UnitID: landUnit, Amount: domain.FromInt(45_000_000), Kind: sale.TerminKindLandExcess, CountsTowardPrice: false},
	)
	sum, err := svc.ContractFinancialSummaryByID(context.Background(), landTenant, landContract)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !sum.HouseOutstanding.Equal(domain.FromInt(179_000_000)) || !sum.LandOutstanding.IsZero() || !sum.LandPaid.Equal(domain.FromInt(45_000_000)) {
		t.Errorf("unit=%s tanah=%s tanahDibayar=%s, want 179jt / 0 / 45jt", sum.HouseOutstanding, sum.LandOutstanding, sum.LandPaid)
	}
}
