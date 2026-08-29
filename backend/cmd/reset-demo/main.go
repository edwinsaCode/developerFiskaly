// cmd/reset-demo menghapus semua transaksi demo (sale, jurnal terkait) tanpa
// menyentuh master data: tenant, user, COA, project, units-master, RAB, cost entries,
// tax rates, allocation config.
//
// Hanya berjalan di APP_ENV=development.
//
// Usage:
//
//	go run ./cmd/reset-demo --tenant 1 --confirm-reset
//
// Urutan penghapusan (FK-safe, satu transaction):
//
//  1. Kumpulkan journal IDs dari termin_payments + sale_records + tax_obligations
//  2. tax_payments  (FK → tax_obligations)
//  3. tax_obligations
//  4. payment_schedules (FK → sale_contracts)
//  5. sale_contracts
//  6. termin_payments
//  7. sale_records
//  8. journal_lines  (WHERE journal_entry_id IN collected)
//  9. journal_entries (WHERE id IN collected)
//  10. UPDATE units: reset status → available, hapus sale fields
package main

import (
	"flag"
	"fmt"
	"log"

	"gorm.io/gorm"

	"esaproperti/internal/platform/config"
	dbplatform "esaproperti/internal/platform/db"
)

func main() {
	tenantID     := flag.Uint64("tenant", 0, "tenant ID (wajib)")
	confirmReset := flag.Bool("confirm-reset", false, "konfirmasi penghapusan data demo (WAJIB untuk eksekusi)")
	dryRun       := flag.Bool("dry-run", false, "tampilkan ringkasan tanpa menghapus")
	flag.Parse()

	if *tenantID == 0 {
		log.Fatal("--tenant <id> wajib diisi")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if !cfg.IsDevelopment() {
		log.Fatalf("AMAN: reset-demo hanya boleh dijalankan di APP_ENV=development (saat ini: %s)", cfg.AppEnv)
	}

	if !*confirmReset && !*dryRun {
		log.Fatal("Tambahkan flag --confirm-reset untuk eksekusi, atau --dry-run untuk preview saja.")
	}

	db, err := dbplatform.Connect(cfg.DBDSN, false)
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	tid := *tenantID

	log.Printf("[RESET-DEMO] === esaProperti demo reset (tenant %d, env=%s) ===", tid, cfg.AppEnv)

	// ── 1. Preview: hitung berapa row yang akan dihapus ──────────────────────

	counts := previewCounts(db, tid)
	printPreview(counts)

	if *dryRun {
		log.Printf("[RESET-DEMO] --dry-run: tidak ada yang dihapus.")
		return
	}

	// ── 2. Eksekusi dalam satu transaction ───────────────────────────────────

	log.Printf("[RESET-DEMO] Memulai penghapusan dalam satu transaction...")

	err = db.Transaction(func(tx *gorm.DB) error {
		return executeReset(tx, tid)
	})
	if err != nil {
		log.Fatalf("[RESET-DEMO] GAGAL — transaction di-rollback: %v", err)
	}

	// ── 3. Validasi pasca-reset ───────────────────────────────────────────────

	postCounts := previewCounts(db, tid)
	log.Printf("[RESET-DEMO] === Pasca-reset ===")
	printCounts(postCounts)

	log.Printf("[RESET-DEMO] === Selesai — master data tidak tersentuh ===")
	log.Printf("[RESET-DEMO]   Jalankan `make demo-seed TENANT=%d` untuk mengisi ulang data demo.", tid)
}

// ── Preview & counting ────────────────────────────────────────────────────────

type tableCounts struct {
	SaleContracts    int64
	PaymentSchedules int64
	TerminPayments   int64
	SaleRecords      int64
	TaxObligations   int64
	TaxPayments      int64
	JournalEntries   int64  // hanya yang terkait sale/tax demo
	JournalLines     int64  // hanya yang terkait journal entries tersebut
	UnitsSold        int64
}

func previewCounts(db *gorm.DB, tid uint64) tableCounts {
	var c tableCounts
	db.Table("sale_contracts").Where("tenant_id = ?", tid).Count(&c.SaleContracts)
	db.Table("payment_schedules").Where("tenant_id = ?", tid).Count(&c.PaymentSchedules)
	db.Table("termin_payments").Where("tenant_id = ?", tid).Count(&c.TerminPayments)
	db.Table("sale_records").Where("tenant_id = ?", tid).Count(&c.SaleRecords)
	db.Table("tax_obligations").Where("tenant_id = ?", tid).Count(&c.TaxObligations)
	db.Table("tax_payments").Where("tenant_id = ?", tid).Count(&c.TaxPayments)
	db.Table("units").Where("tenant_id = ? AND status = 'sold'", tid).Count(&c.UnitsSold)

	ids := collectDemoJournalIDs(db, tid)
	if len(ids) > 0 {
		c.JournalEntries = int64(len(ids))
		db.Table("journal_lines").Where("journal_entry_id IN ?", ids).Count(&c.JournalLines)
	}
	return c
}

func printPreview(c tableCounts) {
	log.Printf("[RESET-DEMO] --- Preview: yang akan dihapus/reset ---")
	log.Printf("[RESET-DEMO]   sale_contracts    : %d baris", c.SaleContracts)
	log.Printf("[RESET-DEMO]   payment_schedules : %d baris", c.PaymentSchedules)
	log.Printf("[RESET-DEMO]   termin_payments   : %d baris", c.TerminPayments)
	log.Printf("[RESET-DEMO]   sale_records      : %d baris", c.SaleRecords)
	log.Printf("[RESET-DEMO]   tax_obligations   : %d baris", c.TaxObligations)
	log.Printf("[RESET-DEMO]   tax_payments      : %d baris", c.TaxPayments)
	log.Printf("[RESET-DEMO]   journal_entries   : %d baris (sale/tax saja)", c.JournalEntries)
	log.Printf("[RESET-DEMO]   journal_lines     : %d baris", c.JournalLines)
	log.Printf("[RESET-DEMO]   units reset       : %d unit status→available", c.UnitsSold)
	log.Printf("[RESET-DEMO] --- Tidak disentuh: tenant, user, COA, project, units-master, budget, cost, tax_rates ---")
}

func printCounts(c tableCounts) {
	log.Printf("[RESET-DEMO]   sale_contracts    : %d (sisa)", c.SaleContracts)
	log.Printf("[RESET-DEMO]   payment_schedules : %d (sisa)", c.PaymentSchedules)
	log.Printf("[RESET-DEMO]   termin_payments   : %d (sisa)", c.TerminPayments)
	log.Printf("[RESET-DEMO]   sale_records      : %d (sisa)", c.SaleRecords)
	log.Printf("[RESET-DEMO]   tax_obligations   : %d (sisa)", c.TaxObligations)
	log.Printf("[RESET-DEMO]   journal_entries   : %d demo (sisa)", c.JournalEntries)
	log.Printf("[RESET-DEMO]   units masih sold  : %d (sisa)", c.UnitsSold)
}

// ── Collect demo journal IDs ──────────────────────────────────────────────────

// collectDemoJournalIDs mengumpulkan IDs journal yang terkait sale/tax demo:
// termin_payments.journal_entry_id, sale_records.revenue_journal_id,
// sale_records.cogs_journal_id, tax_obligations.journal_entry_id.
// Journal dari cost_entries / budget TIDAK dimasukkan — mereka bukan demo.
func collectDemoJournalIDs(db *gorm.DB, tid uint64) []uint64 {
	seen := make(map[uint64]struct{})

	var terminJournals []uint64
	db.Table("termin_payments").
		Select("journal_entry_id").
		Where("tenant_id = ? AND journal_entry_id IS NOT NULL", tid).
		Pluck("journal_entry_id", &terminJournals)
	for _, id := range terminJournals {
		seen[id] = struct{}{}
	}

	type saleJournalRow struct {
		RevenueJournalID uint64
		COGSJournalID    *uint64
	}
	var saleRows []saleJournalRow
	db.Table("sale_records").
		Select("revenue_journal_id, cogs_journal_id").
		Where("tenant_id = ?", tid).
		Scan(&saleRows)
	for _, r := range saleRows {
		if r.RevenueJournalID != 0 {
			seen[r.RevenueJournalID] = struct{}{}
		}
		if r.COGSJournalID != nil && *r.COGSJournalID != 0 {
			seen[*r.COGSJournalID] = struct{}{}
		}
	}

	var taxJournals []uint64
	db.Table("tax_obligations").
		Select("journal_entry_id").
		Where("tenant_id = ? AND journal_entry_id IS NOT NULL", tid).
		Pluck("journal_entry_id", &taxJournals)
	for _, id := range taxJournals {
		seen[id] = struct{}{}
	}

	ids := make([]uint64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}

// ── Core reset (FK-safe order, single transaction) ────────────────────────────

func executeReset(tx *gorm.DB, tid uint64) error {
	// Kumpulkan journal IDs SEBELUM menghapus tabel yang mereferensikannya.
	journalIDs := collectDemoJournalIDs(tx, tid)

	steps := []struct {
		label string
		fn    func() error
	}{
		{"tax_payments", func() error {
			return deleteWhere(tx, "tax_payments", "tenant_id = ?", tid)
		}},
		{"tax_obligations", func() error {
			return deleteWhere(tx, "tax_obligations", "tenant_id = ?", tid)
		}},
		{"payment_schedules", func() error {
			return deleteWhere(tx, "payment_schedules", "tenant_id = ?", tid)
		}},
		{"sale_contracts", func() error {
			return deleteWhere(tx, "sale_contracts", "tenant_id = ?", tid)
		}},
		{"termin_payments", func() error {
			return deleteWhere(tx, "termin_payments", "tenant_id = ?", tid)
		}},
		{"sale_records", func() error {
			return deleteWhere(tx, "sale_records", "tenant_id = ?", tid)
		}},
		{"journal_lines (demo)", func() error {
			if len(journalIDs) == 0 {
				return nil
			}
			return deleteWhere(tx, "journal_lines", "journal_entry_id IN ?", journalIDs)
		}},
		{"journal_entries (demo)", func() error {
			if len(journalIDs) == 0 {
				return nil
			}
			return deleteWhere(tx, "journal_entries", "id IN ?", journalIDs)
		}},
		{"units reset→available", func() error {
			return tx.Exec(`
				UPDATE units
				SET    status = 'available',
				       sale_price = NULL,
				       sale_date  = NULL,
				       buyer_ref  = NULL
				WHERE  tenant_id = ?
				  AND  status = 'sold'
			`, tid).Error
		}},
	}

	for _, step := range steps {
		if err := step.fn(); err != nil {
			return fmt.Errorf("step %q: %w", step.label, err)
		}
		log.Printf("[RESET-DEMO]   ✓ %s", step.label)
	}
	return nil
}

func deleteWhere(tx *gorm.DB, table, condition string, args ...interface{}) error {
	return tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE %s", table, condition), args...).Error
}
