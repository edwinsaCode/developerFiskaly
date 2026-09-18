//go:build integration

package sale_test

// Regresi kwitansi setelah transfer unit (Task 2, 2026-09-18): kwitansi
// booking (KWB) diterbitkan idempoten by termin_payment_id, yang TIDAK
// berubah lintas TransferBookingAtomic — maka LoadReceiptPrintData WAJIB
// meresolusi unit dari booking TERKINI (bukan snapshot receipts.unit_id)
// untuk kwitansi yang terhubung ke booking. Lihat catatan di
// internal/billing/receipt_repository.go (LoadReceiptPrintData).

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/billing"
	"esaproperti/internal/domain"
)

// LoadReceiptPrintData (billing) JOIN ke tenants (nama perusahaan di kop
// kwitansi) — infra yang tidak dibutuhkan oleh test booking lain di file ini,
// jadi bkSeed tidak menanamnya. Idempoten: ID tenant test tetap sama antar run.
func rtSeedTenant(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`INSERT INTO tenants (id, name) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE name = VALUES(name)`, bkTenant, "E2E Sale Test Co").Error; err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
}

func TestIntegration_ReceiptPrint_UnitAfterBookingTransfer(t *testing.T) {
	db := itConnect(t)
	bkCleanup(t, db)
	defer bkCleanup(t, db)
	ctx := context.Background()
	rtSeedTenant(t, db)
	projectID, unitAID, customerID := bkSeed(t, db)

	// Unit tujuan transfer (B) — proyek yang sama.
	var unitBID uint64
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`,
		bkTenant, projectID, "BK-02", "villa", domain.FromInt(100), domain.FromInt(0), "available").Error; err != nil {
		t.Fatalf("seed unit B: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitBID)

	svc := bkWire(db)
	prints := billing.NewGORMRepository(db)

	b, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitAID, customerID, false, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	if b.TerminPaymentID == nil {
		t.Fatal("booking tanpa termin_payment_id — kwitansi tidak bisa diverifikasi")
	}
	var receiptID uint64
	db.Raw(`SELECT id FROM receipts WHERE tenant_id = ? AND termin_payment_id = ?`, bkTenant, *b.TerminPaymentID).Scan(&receiptID)
	if receiptID == 0 {
		t.Fatal("kwitansi booking tidak ditemukan setelah CreateBooking")
	}

	// Baseline: sebelum transfer, kwitansi menampilkan unit ASAL (A).
	before, err := prints.LoadReceiptPrintData(ctx, bkTenant, receiptID)
	if err != nil {
		t.Fatalf("LoadReceiptPrintData (sebelum transfer): %v", err)
	}
	if before.UnitCode != "BK-01" {
		t.Errorf("unit sebelum transfer = %q, want BK-01", before.UnitCode)
	}

	if _, err := svc.TransferBooking(ctx, bkTenant, b.ID, unitBID, "regresi: pindah blok",
		time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatalf("TransferBooking: %v", err)
	}

	// Regresi utama: kwitansi (nomor & id SAMA — idempoten) sekarang WAJIB
	// meresolusi unit TUJUAN (B), bukan unit asal yang sudah dilepas.
	after, err := prints.LoadReceiptPrintData(ctx, bkTenant, receiptID)
	if err != nil {
		t.Fatalf("LoadReceiptPrintData (setelah transfer): %v", err)
	}
	if after.ReceiptNumber != before.ReceiptNumber {
		t.Errorf("nomor kwitansi berubah setelah transfer: %q -> %q (harusnya idempoten/sama)", before.ReceiptNumber, after.ReceiptNumber)
	}
	if after.UnitCode != "BK-02" {
		t.Errorf("unit setelah transfer = %q, want BK-02 (unit tujuan)", after.UnitCode)
	}
	if after.UnitCode == "BK-01" {
		t.Error("kwitansi masih menampilkan unit ASAL setelah transfer — regresi bug utama")
	}

	// Snapshot historis (receipts.unit_id) TIDAK BOLEH ditulis-ulang — hanya
	// resolusi TAMPILAN yang berubah, bukan dokumen historisnya.
	var snapshotUnitID uint64
	db.Raw(`SELECT unit_id FROM receipts WHERE id = ?`, receiptID).Scan(&snapshotUnitID)
	if snapshotUnitID != unitAID {
		t.Errorf("receipts.unit_id (snapshot) = %d, want tetap %d (unit asal, tidak diubah oleh transfer)", snapshotUnitID, unitAID)
	}

	// Audit transfer tetap utuh (append-only) — kedua baris log status unit.
	var transitions int64
	db.Raw(`SELECT COUNT(*) FROM unit_status_transitions
		WHERE tenant_id = ? AND reference_type = 'booking' AND reference_id = ?
		AND event IN ('booking_transferred_out','booking_transferred_in')`,
		bkTenant, b.ID).Scan(&transitions)
	if transitions != 2 {
		t.Errorf("log transisi transfer = %d, want 2 (out + in)", transitions)
	}
}

// Regresi: booking yang TIDAK PERNAH ditransfer tetap menampilkan unitnya
// sendiri seperti biasa — fix di LoadReceiptPrintData tidak boleh mengubah
// perilaku jalur normal (COALESCE jatuh ke bk.unit_id yang sama dgn semula).
func TestIntegration_ReceiptPrint_UnitNormal_NoTransfer(t *testing.T) {
	db := itConnect(t)
	bkCleanup(t, db)
	defer bkCleanup(t, db)
	ctx := context.Background()
	rtSeedTenant(t, db)
	_, unitID, customerID := bkSeed(t, db)

	svc := bkWire(db)
	prints := billing.NewGORMRepository(db)

	b, err := svc.CreateBooking(ctx, bkTenant, bkBookingReq(unitID, customerID, false, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	var receiptID uint64
	db.Raw(`SELECT id FROM receipts WHERE tenant_id = ? AND termin_payment_id = ?`, bkTenant, *b.TerminPaymentID).Scan(&receiptID)
	if receiptID == 0 {
		t.Fatal("kwitansi booking tidak ditemukan")
	}
	data, err := prints.LoadReceiptPrintData(ctx, bkTenant, receiptID)
	if err != nil {
		t.Fatalf("LoadReceiptPrintData: %v", err)
	}
	if data.UnitCode != "BK-01" {
		t.Errorf("unit = %q, want BK-01 (booking tanpa transfer)", data.UnitCode)
	}
}
