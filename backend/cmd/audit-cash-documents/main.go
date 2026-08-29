// Command audit-cash-documents memeriksa INV-DOC-1 terhadap seluruh buku
// (W-3.4): setiap pergerakan kas yang diposting harus punya TEPAT SATU dokumen
// bernomor yang dapat ditelusuri dua arah.
//
// Read-only sepenuhnya. Kembaran ops dari endpoint GET /documents/audit —
// endpoint melayani satu tenant yang sedang login, CLI ini menyapu semuanya
// sekaligus dan itulah yang dijalankan sebelum penegakan W-3.5 dinyalakan.
//
// Exit code 1 bila ada pelanggaran, supaya bisa dipakai sebagai gerbang rilis.
//
//	go run ./cmd/audit-cash-documents              # semua tenant
//	go run ./cmd/audit-cash-documents --tenant 11
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"esaproperti/internal/document"
	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
)

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID (0 = semua tenant yang punya jurnal)")
	limit := flag.Int("limit", 20, "maksimum baris temuan yang dicetak per pemeriksaan")
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

	fmt.Println("=== Audit INV-DOC-1 · dokumen kas (read-only) ===")
	total := 0
	for _, tid := range tenants {
		rep, err := document.AuditCashDocuments(ctx, gdb, tid, *limit)
		if err != nil {
			log.Fatalf("audit tenant %d: %v", tid, err)
		}
		if rep.CashPosted == 0 {
			continue
		}
		printReport(rep)
		total += rep.Violations
	}

	fmt.Println("--------------------------------------------------")
	if total == 0 {
		fmt.Println("✓ INV-DOC-1 utuh di seluruh tenant.")
		return
	}
	fmt.Printf("✗ %d pelanggaran INV-DOC-1. Bereskan sebelum penegakan dinyalakan.\n", total)
	os.Exit(1)
}

func printReport(r *document.AuditReport) {
	status := "✓ bersih"
	if !r.Clean {
		status = fmt.Sprintf("✗ %d pelanggaran", r.Violations)
	}
	fmt.Printf("\nTenant %d — %s\n", r.TenantID, status)
	fmt.Printf("  jurnal kas terposting : %d (berdokumen %d · saldo awal %d)\n",
		r.CashPosted, r.Documented, r.Exempt)
	for _, c := range r.Checks {
		if c.Count == 0 {
			continue
		}
		fmt.Printf("  • %s: %d\n", c.Title, c.Count)
		for _, f := range c.Findings {
			switch {
			case f.JournalID != 0:
				fmt.Printf("      jurnal %d (%s, %s) — %s\n", f.JournalID, f.Date, f.Amount, f.Detail)
			case f.DocumentID != 0:
				fmt.Printf("      dokumen %s — %s\n", f.Number, f.Detail)
			default:
				fmt.Printf("      %s\n", f.Detail)
			}
		}
		if c.Truncated {
			fmt.Printf("      … (%d temuan lagi tidak dicetak)\n", c.Count-len(c.Findings))
		}
	}
}
