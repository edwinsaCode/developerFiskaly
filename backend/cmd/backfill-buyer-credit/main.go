// Command backfill-buyer-credit menutup SISA saldo kredit buyer historis yang
// sudah didisposisi (dikonsumsi netting Uang Muka saat Akad, atau
// didisposisi terminal saat pembatalan) SEBELUM perbaikan bug "Saldo Kredit
// Buyer" (2026-09-04) — kasus nyata: kontrak #4407 menampilkan Rp37.000.000
// sebagai kredit tersedia padahal sudah dipakai saat Akad.
//
// Root cause: GetBuyerCredit lama hanya mengurangi Σ(buyer_credit) dengan
// Σ(credit_applications) EKSPLISIT (ApplyCredit) — konsumsi OTOMATIS saat
// Akad/pembatalan tidak pernah tercatat ke credit_applications, sehingga
// saldo yang sudah habis dipakai tetap terlihat tersedia selamanya.
//
// Sejak perbaikan, RecordAkad dan cancellation.Process menutup sisa saldo
// itu SENDIRI, atomik, di transaksi yang sama (consumeRemainingCreditInTx).
// Perintah ini HANYA untuk unit yang Akad/pembatalannya SUDAH terjadi
// SEBELUM perbaikan tsb dipasang — tidak hardcode ke satu unit/kontrak
// tertentu: kandidat ditemukan generik lewat query di bawah.
//
// Kandidat = unit dengan available (Σbuyer_credit − Σcredit_applications) > 0
// DAN unit itu SUDAH memiliki jejak "Akad selesai" (baris sale_records) ATAU
// "pembatalan selesai" (cancellations.status = 'processed'). Unit yang belum
// pernah Akad/dibatalkan TIDAK disentuh — saldo kreditnya memang masih
// legitimately tersedia.
//
// Idempoten: consumeRemainingCreditInTx no-op bila available sudah 0 — putaran
// kedua tidak menulis apa pun. Append-only: tidak ada baris yang diubah/dihapus.
//
// Default DRY-RUN. Contoh:
//
//	go run ./cmd/backfill-buyer-credit                  # dry-run semua tenant
//	go run ./cmd/backfill-buyer-credit --tenant 11      # dry-run satu tenant
//	go run ./cmd/backfill-buyer-credit --tenant 11 --unit 4407 --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
	"esaproperti/internal/sale"
)

type candidate struct {
	TenantID   uint64
	UnitID     uint64
	Available  domain.Money
	ContractID uint64 // 0 = tidak ada sale_contract formal
}

func findCandidates(ctx context.Context, gdb *gorm.DB, tenantID, unitID uint64) ([]candidate, error) {
	var rows []struct {
		TenantID uint64
		UnitID   uint64
	}
	q := gdb.WithContext(ctx).Table("payment_allocations AS pa").
		Joins("JOIN termin_payments tp ON tp.id = pa.termin_payment_id AND tp.tenant_id = pa.tenant_id").
		Where("pa.allocation_type = ?", "buyer_credit").
		Where(`EXISTS (SELECT 1 FROM sale_records sr WHERE sr.tenant_id = tp.tenant_id AND sr.unit_id = tp.unit_id)
			OR EXISTS (SELECT 1 FROM cancellations c WHERE c.tenant_id = tp.tenant_id AND c.unit_id = tp.unit_id AND c.status = 'processed')`).
		Select("DISTINCT tp.tenant_id AS tenant_id, tp.unit_id AS unit_id")
	if tenantID != 0 {
		q = q.Where("tp.tenant_id = ?", tenantID)
	}
	if unitID != 0 {
		q = q.Where("tp.unit_id = ?", unitID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("cari kandidat: %w", err)
	}

	var out []candidate
	for _, r := range rows {
		var sources, applied domain.Money
		if err := gdb.WithContext(ctx).Table("payment_allocations AS pa").
			Joins("JOIN termin_payments tp ON tp.id = pa.termin_payment_id AND tp.tenant_id = pa.tenant_id").
			Where("pa.tenant_id = ? AND pa.allocation_type = ? AND tp.unit_id = ?", r.TenantID, "buyer_credit", r.UnitID).
			Select("COALESCE(SUM(pa.amount), 0)").Scan(&sources).Error; err != nil {
			return nil, fmt.Errorf("sum buyer_credit unit %d: %w", r.UnitID, err)
		}
		if err := gdb.WithContext(ctx).Table("credit_applications").
			Where("tenant_id = ? AND unit_id = ?", r.TenantID, r.UnitID).
			Select("COALESCE(SUM(amount), 0)").Scan(&applied).Error; err != nil {
			return nil, fmt.Errorf("sum credit_applications unit %d: %w", r.UnitID, err)
		}
		available := sources.Sub(applied)
		if available.IsZero() || available.IsNeg() {
			continue
		}

		var contractID uint64
		// Prioritas: cancellation TERAKHIR yang diproses (disposisi terminal
		// paling baru) → sale_contract aktif unit tsb → 0 (data legacy, tetap
		// sah — lihat migrasi 000097).
		gdb.WithContext(ctx).Table("cancellations").
			Where("tenant_id = ? AND unit_id = ? AND status = 'processed' AND sale_contract_id IS NOT NULL", r.TenantID, r.UnitID).
			Order("processed_at DESC").Limit(1).Pluck("sale_contract_id", &contractID)
		if contractID == 0 {
			gdb.WithContext(ctx).Table("sale_contracts").
				Where("tenant_id = ? AND unit_id = ?", r.TenantID, r.UnitID).
				Order("id DESC").Limit(1).Pluck("id", &contractID)
		}

		out = append(out, candidate{TenantID: r.TenantID, UnitID: r.UnitID, Available: available, ContractID: contractID})
	}
	return out, nil
}

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID (0 = semua tenant)")
	unitID := flag.Uint64("unit", 0, "batasi ke satu unit ID (0 = semua unit; utk verifikasi bertarget, mis. kontrak #4407)")
	apply := flag.Bool("apply", false, "tutup saldo (posting credit_applications); default: dry-run")
	verbose := flag.Bool("verbose", false, "cetak seluruh query SQL")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	gdb, err := db.Connect(cfg.DBDSN, *verbose)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	ctx := context.Background()

	mode := "DRY-RUN (tidak menulis apa pun)"
	if *apply {
		mode = "APPLY (menutup sisa saldo kredit)"
	}
	fmt.Printf("=== Backfill Saldo Kredit Buyer (bug #4407) · %s ===\n", mode)

	candidates, err := findCandidates(ctx, gdb, *tenantID, *unitID)
	if err != nil {
		log.Fatalf("cari kandidat: %v", err)
	}
	if len(candidates) == 0 {
		fmt.Println("Tidak ada unit dengan sisa saldo kredit yang perlu ditutup. Selesai.")
		return
	}

	// ConsumeRemainingCreditInTx tidak menyentuh dependency lain Service —
	// aman dipanggil dari Service kosong (lihat internal/sale/credit_application.go).
	svc := sale.NewService(nil, nil, nil, nil, nil, nil)

	total := domain.Zero
	for _, c := range candidates {
		fmt.Printf("Tenant %d unit %d — sisa tersedia %s (kontrak: %s)\n",
			c.TenantID, c.UnitID, c.Available, contractLabel(c.ContractID))
		total = total.Add(c.Available)
		if !*apply {
			continue
		}
		reason := "Backfill penutupan saldo kredit buyer historis (bug #4407) — dana sudah dikonsumsi/didisposisi sebelum perbaikan 2026-09-04"
		if err := gdb.Transaction(func(tx *gorm.DB) error {
			return svc.ConsumeRemainingCreditInTx(ctx, tx, c.TenantID, c.UnitID, c.ContractID, reason, nil)
		}); err != nil {
			log.Fatalf("tutup saldo tenant %d unit %d: %v", c.TenantID, c.UnitID, err)
		}
	}

	fmt.Printf("\n=== Total: %d unit, %s saldo kredit %s ===\n",
		len(candidates), total, map[bool]string{true: "ditutup", false: "AKAN ditutup"}[*apply])
	if !*apply {
		fmt.Println("Dry-run: tidak ada perubahan yang tersimpan. Jalankan ulang dengan --apply.")
	}
}

func contractLabel(id uint64) string {
	if id == 0 {
		return "tidak ada sale_contract formal"
	}
	return fmt.Sprintf("#%d", id)
}
