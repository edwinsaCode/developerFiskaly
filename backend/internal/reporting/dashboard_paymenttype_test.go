package reporting

import (
	"testing"

	"esaproperti/internal/domain"
)

// TestSumByPaymentType (P3, item A — Dashboard Cash vs KPR, 2026-08-27):
// memverifikasi pemecahan cash/kpr TIDAK memaksa baris tanpa kontrak
// (payment_type kosong) ke salah satu sisi — cross-cutting requirement I
// (no silent misclassification). Σ(cash, kpr) harus < total sumber bila ada
// baris tak terklasifikasi.
func TestSumByPaymentType_TunaiKprDanTakTerklasifikasi(t *testing.T) {
	rows := []RevenueByPaymentTypeRow{
		{PaymentType: "tunai", Amount: domain.FromInt(350_000_000)},
		{PaymentType: "kpr", Amount: domain.FromInt(120_000_000)},
		{PaymentType: "", Amount: domain.FromInt(10_000_000)}, // belum/tidak ber-kontrak
	}

	cash, kpr := sumByPaymentType(rows)

	if cash != "350000000" {
		t.Errorf("cash: got %s, want 350000000", cash)
	}
	if kpr != "120000000" {
		t.Errorf("kpr: got %s, want 120000000", kpr)
	}
	// Baris tak terklasifikasi (10jt) SENGAJA tidak muncul di cash maupun kpr.
}

// TestSumByPaymentType_TanpaBarisTakTerklasifikasi_JumlahSamaDenganTotal
// memverifikasi kasus normal (semua baris ber-kontrak): cash + kpr harus
// PERSIS sama dengan total sumber — reconciliation check inti item A.
func TestSumByPaymentType_TanpaBarisTakTerklasifikasi_JumlahSamaDenganTotal(t *testing.T) {
	total := domain.FromInt(350_000_000).Add(domain.FromInt(30_000_000))
	rows := []RevenueByPaymentTypeRow{
		{PaymentType: "tunai", Amount: domain.FromInt(350_000_000)},
		{PaymentType: "tunai", Amount: domain.FromInt(30_000_000)},
	}

	cash, kpr := sumByPaymentType(rows)

	gotTotal, err := domain.NewMoney(cash)
	if err != nil {
		t.Fatalf("cash tidak valid: %v", err)
	}
	kprMoney, err := domain.NewMoney(kpr)
	if err != nil {
		t.Fatalf("kpr tidak valid: %v", err)
	}
	if !gotTotal.Add(kprMoney).Equal(total) {
		t.Errorf("cash+kpr = %s, want %s (rekonsiliasi persis)", gotTotal.Add(kprMoney).String(), total.String())
	}
	if kpr != "0" {
		t.Errorf("kpr: got %s, want 0", kpr)
	}
}
