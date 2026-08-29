// Command backfill-allocations merekonstruksi payment_allocations (FE-2 · P3)
// untuk termin lama secara best-effort (replay waterfall per unit) dan mencetak
// laporan rekonsiliasi. Default DRY-RUN (tidak menulis); pakai --apply untuk
// benar-benar menyisipkan baris. Unit yang tidak rekonsiliasi ditandai untuk
// review manual — TIDAK pernah di-auto-merge.
//
// Contoh:
//
//	go run ./cmd/backfill-allocations                 # dry-run semua tenant
//	go run ./cmd/backfill-allocations --tenant 1      # dry-run tenant 1
//	go run ./cmd/backfill-allocations --tenant 1 --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
	"esaproperti/internal/sale"
)

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID (0 = semua tenant yang punya termin)")
	apply := flag.Bool("apply", false, "tulis baris alokasi (default: dry-run, hanya laporan)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	gdb, err := db.Connect(cfg.DBDSN, cfg.IsDevelopment())
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	ctx := context.Background()
	repo := sale.NewGORMRepository(gdb, nil, nil)

	var tenants []uint64
	if *tenantID != 0 {
		tenants = []uint64{*tenantID}
	} else {
		if err := gdb.WithContext(ctx).
			Table("termin_payments").
			Distinct().
			Order("tenant_id ASC").
			Pluck("tenant_id", &tenants).Error; err != nil {
			log.Fatalf("list tenants: %v", err)
		}
	}

	mode := "DRY-RUN (tidak menulis)"
	if *apply {
		mode = "APPLY (menulis baris alokasi)"
	}
	fmt.Printf("=== Backfill payment_allocations · %s ===\n", mode)
	if len(tenants) == 0 {
		fmt.Println("Tidak ada tenant dengan termin. Selesai.")
		return
	}

	var totalFlagged int
	for _, tid := range tenants {
		report, err := repo.BackfillAllocations(ctx, tid, *apply)
		if err != nil {
			log.Fatalf("backfill tenant %d: %v", tid, err)
		}
		printReport(report)
		totalFlagged += report.UnitsFlagged
	}

	fmt.Println("--------------------------------------------------")
	if totalFlagged > 0 {
		fmt.Printf("⚠  %d unit DITANDAI untuk review manual (tidak ditulis). Lihat detail di atas.\n", totalFlagged)
	} else {
		fmt.Println("✓ Semua unit rekonsiliasi bersih.")
	}
	if !*apply {
		fmt.Println("Ini DRY-RUN. Jalankan ulang dengan --apply untuk menulis.")
	}
}

func printReport(r *sale.BackfillReport) {
	fmt.Printf("\nTenant %d:\n", r.TenantID)
	fmt.Printf("  unit diproses      : %d\n", r.UnitsProcessed)
	fmt.Printf("  unit rekonsiliasi  : %d\n", r.UnitsReconciled)
	fmt.Printf("  unit ditandai      : %d\n", r.UnitsFlagged)
	fmt.Printf("  termin di-backfill : %d\n", r.TerminsBackfilled)
	fmt.Printf("  termin dilewati    : %d (sudah punya alokasi)\n", r.TerminsSkipped)
	fmt.Printf("  baris alokasi      : %d (%s)\n", r.AllocationsToWrite,
		map[bool]string{true: "ditulis", false: "rencana"}[r.Apply])
	for _, f := range r.Flags {
		fmt.Printf("  ⚠ unit %d (kontrak %d): %s — %s\n", f.UnitID, f.ContractID, f.Reason, f.Detail)
	}
}
