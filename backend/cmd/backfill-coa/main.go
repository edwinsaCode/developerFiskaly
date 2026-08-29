// Command backfill-coa menambahkan akun COA baru yang belum ada ke tenant
// existing. Berbeda dari cmd/seed (yang JUGA menjalankan project.SeedLITHOS —
// TIDAK aman dipakai berulang di tenant produksi karena akan menyuntik data
// demo), perintah ini HANYA memanggil ledger.SeedCOA — idempoten via
// FirstOrCreate(tenant_id, code): tenant yang sudah punya semua akun tidak
// berubah sama sekali; tenant yang kekurangan akun baru (mis. 4-1100
// Pendapatan Penjualan Tanah, LT-5) mendapatkannya ditambahkan tanpa
// menyentuh baris yang sudah ada.
//
//	go run ./cmd/backfill-coa                # semua tenant
//	go run ./cmd/backfill-coa --tenant 11     # satu tenant
package main

import (
	"context"
	"flag"
	"log"

	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
)

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID (0 = semua tenant)")
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

	var tenants []uint64
	if *tenantID != 0 {
		tenants = []uint64{*tenantID}
	} else {
		if err := gdb.WithContext(ctx).Table("tenants").Order("id ASC").Pluck("id", &tenants).Error; err != nil {
			log.Fatalf("list tenants: %v", err)
		}
	}

	if len(tenants) == 0 {
		log.Println("Tidak ada tenant. Selesai.")
		return
	}

	for _, tid := range tenants {
		if err := ledger.SeedCOA(ctx, gdb, tid); err != nil {
			log.Fatalf("tenant %d: seed COA: %v", tid, err)
		}
		log.Printf("tenant %d: COA sinkron (akun baru ditambahkan bila belum ada, akun existing tidak disentuh)", tid)
	}
	log.Printf("Selesai — %d tenant diproses.", len(tenants))
}
