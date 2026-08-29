// Command backfill-recognition menerbitkan JURNAL SUSULAN piutang biaya
// realisasi (keputusan klien R-3).
//
// Sebelum W-5, invoice realisasi terbit TANPA jurnal: tagihannya ada di layar
// tapi tidak ada di buku besar. Sejak W-5 setiap invoice baru langsung mengakui
// Dr 1-2000 / Cr <akun titipan>. Tanpa perintah ini dua aturan berjalan
// bersamaan — piutang baru ada di neraca, piutang lama tidak — dan tidak ada
// satu pun angka piutang yang bisa dipercaya.
//
// Idempoten: putaran kedua menghitung delta nol dan tidak menerbitkan apa pun.
// Append-only: tidak ada baris yang diubah atau dihapus.
//
// Default DRY-RUN (seluruh perhitungan dijalankan lalu di-rollback). Contoh:
//
//	go run ./cmd/backfill-recognition                 # dry-run semua tenant
//	go run ./cmd/backfill-recognition --tenant 11     # dry-run satu tenant
//	go run ./cmd/backfill-recognition --tenant 11 --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/billing"
	"esaproperti/internal/charge"
	"esaproperti/internal/domain"
	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
)

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID (0 = semua tenant yang punya grup titipan)")
	apply := flag.Bool("apply", false, "posting jurnal susulan (default: dry-run)")
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

	// Service disusun seperti di main.go: penerbit invoice terpasang karena
	// jurnal susulan membaca invoice hidup tiap grup untuk nomor & jatuh temponya.
	_, billingSvc := billing.NewHandler(gdb)
	chargeSvc := charge.NewHandler(gdb).Svc()
	chargeSvc.SetInvoiceIssuer(&invoiceAdapter{svc: billingSvc})

	var tenants []uint64
	if *tenantID != 0 {
		tenants = []uint64{*tenantID}
	} else {
		if err := gdb.WithContext(ctx).
			Table("charge_groups").Distinct().Order("tenant_id ASC").
			Pluck("tenant_id", &tenants).Error; err != nil {
			log.Fatalf("list tenants: %v", err)
		}
	}

	mode := "DRY-RUN (dihitung lalu di-rollback)"
	if *apply {
		mode = "APPLY (memposting jurnal susulan)"
	}
	fmt.Printf("=== Jurnal susulan piutang biaya realisasi · R-3 · %s ===\n", mode)
	if len(tenants) == 0 {
		fmt.Println("Tidak ada tenant dengan grup titipan. Selesai.")
		return
	}

	totalGroups, totalRecognized := 0, 0
	totalPosted := domain.Zero
	for _, tid := range tenants {
		rep, err := chargeSvc.CatchUpRecognition(ctx, tid, *apply, nil)
		if err != nil {
			log.Fatalf("tenant %d: %v", tid, err)
		}
		totalGroups += rep.GroupsScanned
		totalRecognized += rep.GroupsRecognize
		totalPosted = totalPosted.Add(rep.Posted)
		if rep.GroupsScanned == 0 {
			continue
		}
		fmt.Printf("\nTenant %d — %d grup dipindai, %d grup diakui, total %s\n",
			tid, rep.GroupsScanned, rep.GroupsRecognize, rep.Posted)
		for _, s := range rep.Skipped {
			fmt.Printf("  · dilewati — %s\n", s)
		}
	}
	fmt.Printf("\n=== Total: %d grup dipindai, %d grup diakui, %s diposting ===\n",
		totalGroups, totalRecognized, totalPosted)
	if !*apply {
		fmt.Println("Dry-run: tidak ada perubahan yang tersimpan. Jalankan ulang dengan --apply.")
	}
}

// invoiceAdapter mengadaptasi billing.Service → charge.InvoiceIssuer (salinan
// tipis dari wiring main.go — perintah ini hanya memakai jalur BACA invoice).
type invoiceAdapter struct{ svc *billing.Service }

func (a *invoiceAdapter) IssueChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID, createdBy uint64, outstanding domain.Money, dueDate time.Time, notes string) (uint64, string, time.Time, error) {
	inv, err := a.svc.GenerateChargeGroupInvoiceInTx(ctx, tx, tenantID, contractID, chargeGroupID, createdBy, outstanding, dueDate, notes)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	return inv.ID, inv.InvoiceNumber, inv.DueDate, nil
}

func (a *invoiceAdapter) FindLiveChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID uint64) (uint64, string, time.Time, error) {
	return a.svc.FindLiveChargeInvoiceInTx(ctx, tx, tenantID, contractID, chargeGroupID)
}

func (a *invoiceAdapter) SettleChargeInvoiceIfPaid(ctx context.Context, tenantID, contractID, chargeGroupID uint64, outstanding domain.Money) {
	a.svc.SettleChargeInvoiceIfPaid(ctx, tenantID, contractID, chargeGroupID, outstanding)
}
