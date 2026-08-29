// Command backfill-cash-documents menutup sisa pelanggaran INV-DOC-1 pada data
// LAMA (W-3.3): jurnal kas yang sudah terposting sebelum W-3.2 menyambungkan
// setiap jalur kas ke Document Engine.
//
// Dua pekerjaan, keduanya idempoten:
//
//   - jurnal yang kwitansinya sudah ada → hanya diisi `document_id`;
//   - jurnal tanpa dokumen sama sekali → dokumen retro terbit sesuai ARAH kas
//     (BKM/BKK/BTP/RFC/MTI) atau JR untuk jurnal pembalik.
//
// Jurnal saldo awal DIKECUALIKAN: itu posisi kas, bukan pergerakan kas.
//
// Default DRY-RUN. Contoh:
//
//	go run ./cmd/backfill-cash-documents                  # dry-run semua tenant
//	go run ./cmd/backfill-cash-documents --tenant 11      # dry-run satu tenant
//	go run ./cmd/backfill-cash-documents --tenant 11 --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"

	"esaproperti/internal/billing"
	"esaproperti/internal/document"
	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
)

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID (0 = semua tenant yang punya jurnal)")
	apply := flag.Bool("apply", false, "terbitkan dokumen & tulis tautan (default: dry-run)")
	verbose := flag.Bool("verbose", false, "cetak seluruh query SQL")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	// Log SQL dimatikan secara default: laporan backfill adalah keluaran yang
	// dibaca manusia, dan satu query per jurnal menenggelamkannya.
	gdb, err := db.Connect(cfg.DBDSN, *verbose)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	ctx := context.Background()

	var tenants []uint64
	if *tenantID != 0 {
		tenants = []uint64{*tenantID}
	} else {
		if err := gdb.WithContext(ctx).
			Table("journal_entries").Distinct().Order("tenant_id ASC").
			Pluck("tenant_id", &tenants).Error; err != nil {
			log.Fatalf("list tenants: %v", err)
		}
	}

	mode := "DRY-RUN (tidak menulis)"
	if *apply {
		mode = "APPLY (menerbitkan dokumen & menulis tautan)"
	}
	fmt.Printf("=== Backfill dokumen kas · INV-DOC-1 · %s ===\n", mode)
	if len(tenants) == 0 {
		fmt.Println("Tidak ada tenant dengan jurnal. Selesai.")
		return
	}

	resolver := billing.NewJournalDocumentResolver()
	var totalScanned, totalLinked, totalIssued, totalFlagged int
	for _, tid := range tenants {
		rep, err := document.BackfillCashDocuments(ctx, gdb, tid, *apply, resolver)
		if err != nil {
			log.Fatalf("backfill tenant %d: %v", tid, err)
		}
		if rep.Scanned == 0 {
			continue
		}
		printReport(rep)
		totalScanned += rep.Scanned
		totalLinked += rep.Linked
		totalIssued += rep.IssuedTotal()
		totalFlagged += rep.Flagged
	}

	fmt.Println("--------------------------------------------------")
	fmt.Printf("Total: %d jurnal kas tanpa dokumen · %d ditautkan · %d dokumen retro · %d ditandai\n",
		totalScanned, totalLinked, totalIssued, totalFlagged)
	if totalFlagged > 0 {
		fmt.Printf("⚠  %d jurnal TIDAK bisa disimpulkan otomatis dan dilewati — tangani manual.\n", totalFlagged)
	}
	if !*apply {
		fmt.Println("Ini DRY-RUN. Jalankan ulang dengan --apply untuk menulis.")
	}
}

func printReport(r *document.BackfillReport) {
	fmt.Printf("\nTenant %d:\n", r.TenantID)
	fmt.Printf("  jurnal kas tanpa dokumen : %d\n", r.Scanned)
	fmt.Printf("  dikecualikan (saldo awal): %d\n", r.Exempt)
	fmt.Printf("  ditautkan ke kwitansi    : %d\n", r.Linked)
	verb := "rencana"
	if r.Apply {
		verb = "terbit"
	}
	fmt.Printf("  dokumen retro (%s)   : %d\n", verb, r.IssuedTotal())
	codes := make([]string, 0, len(r.Issued))
	for c := range r.Issued {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		fmt.Printf("      %-20s %d\n", c, r.Issued[c])
	}
	for _, f := range r.Flags {
		fmt.Printf("  ⚠ jurnal %d (%s, source=%s): %s\n", f.JournalID, f.Date, f.Source, f.Reason)
	}
}
