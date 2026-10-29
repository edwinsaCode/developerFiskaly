package billing

// Audit kwitansi Kelebihan Tanah (2026-10-29): skenario klien — harga unit
// 185jt + Kelebihan Tanah 45jt, DP 6jt. Kwitansi pembayaran tanah 45jt HARUS
// mencetak Sisa Harga Unit 179jt (tidak berkurang) dan Sisa Kelebihan Tanah 0
// sebagai dua angka terpisah (bug lama: sisa terutang turun jadi 134jt).

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"esaproperti/internal/domain"
)

type stubPrintLoader struct{ data ReceiptPrintData }

func (s *stubPrintLoader) LoadReceiptPrintData(_ context.Context, _, _ uint64) (*ReceiptPrintData, error) {
	d := s.data
	return &d, nil
}

type landSummaryProvider struct{}

func (landSummaryProvider) summary() *ContractSummary {
	return &ContractSummary{
		PriceIsSnapshot: true,
		UnitPrice:       domain.FromInt(185_000_000),
		Discount:        domain.Zero,
		NetContract:     domain.FromInt(185_000_000),
		TotalPaid:       domain.FromInt(6_000_000),
		// Gabungan lama (unit + tanah) — TIDAK boleh tercetak sebagai sisa unit.
		Outstanding:                 domain.FromInt(179_000_000),
		CustomerReceivableRemaining: domain.FromInt(179_000_000),
		HasLand:                     true,
		HouseCustomerRemaining:      domain.FromInt(179_000_000),
		LandBilled:                  domain.FromInt(45_000_000),
		LandPaid:                    domain.FromInt(45_000_000),
		LandOutstanding:             domain.Zero,
	}
}

func (p landSummaryProvider) SummaryByContractID(_ context.Context, _, _ uint64) (*ContractSummary, error) {
	return p.summary(), nil
}
func (p landSummaryProvider) SummaryByUnitID(_ context.Context, _, _ uint64) (*ContractSummary, error) {
	return p.summary(), nil
}

func landReceiptService(t *testing.T, rt ReceiptType, kind string) *ReceiptService {
	t.Helper()
	receipts := newMockReceipts()
	termins := newMockTermins()
	termins.add(1, &TerminInfo{ID: 50, UnitID: 100, Amount: domain.FromInt(45_000_000), BankAccountCode: "1-1300", Date: time.Now()})
	svc := NewReceiptService(termins, receipts, &stubPrintLoader{data: ReceiptPrintData{
		ReceiptType:   rt,
		TerminKind:    kind,
		ReceiptNumber: "KWT/2026/000045",
		Amount:        domain.FromInt(45_000_000),
		ReceivedAt:    time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
		CompanyName:   "PT Contoh",
		BuyerName:     "Pembeli",
		UnitCode:      "A-01",
	}})
	if _, err := svc.GenerateReceipt(context.Background(), 1, 7, 50, ""); err != nil {
		t.Fatalf("GenerateReceipt: %v", err)
	}
	svc.SetContractSummaryProvider(landSummaryProvider{})
	return svc
}

// cellValue mengambil isi <div class="sv"> tepat setelah label ringkasan.
func cellValue(t *testing.T, html, label string) string {
	t.Helper()
	marker := `<div class="sk">` + label + `</div><div class="sv">`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("label %q tidak tercetak di kwitansi", label)
	}
	rest := html[i+len(marker):]
	return rest[:strings.Index(rest, "</div>")]
}

func TestReceipt_LandExcessPayment_HouseAndLandSeparate(t *testing.T) {
	svc := landReceiptService(t, ReceiptTypeHousePayment, "land_excess")
	data, err := svc.GetReceiptPrintData(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("GetReceiptPrintData: %v", err)
	}
	if !data.HasLand || !data.Outstanding.Equal(domain.FromInt(179_000_000)) {
		t.Fatalf("HasLand=%v Outstanding=%s, want true / 179jt", data.HasLand, data.Outstanding)
	}

	var buf bytes.Buffer
	if err := RenderReceiptPrint(&buf, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for label, want := range map[string]string{
		"Harga Unit":            "Rp 185.000.000",
		"Sudah Dibayar (Unit)":  "Rp 6.000.000",
		"Sisa Harga Unit":       "Rp 179.000.000",
		"Kelebihan Tanah":       "Rp 45.000.000",
		"Sudah Dibayar (Tanah)": "Rp 45.000.000",
		"Sisa Kelebihan Tanah":  "Rp 0",
	} {
		if got := cellValue(t, html, label); got != want {
			t.Errorf("%s = %q, want %q", label, got, want)
		}
	}
	if strings.Contains(html, "134.000.000") {
		t.Error("kwitansi masih mencetak 134jt (sisa unit dikurangi pembayaran tanah)")
	}
}

// Booking tetap Terutang Rp0 dan tidak memakai tata letak tanah.
func TestReceipt_Booking_LandLayoutNotUsed(t *testing.T) {
	svc := landReceiptService(t, ReceiptTypeBooking, "")
	data, err := svc.GetReceiptPrintData(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("GetReceiptPrintData: %v", err)
	}
	if data.HasLand || !data.Outstanding.IsZero() {
		t.Errorf("booking: HasLand=%v Outstanding=%s, want false / 0", data.HasLand, data.Outstanding)
	}
}
