// Command backfill-land-schedule menutup celah arsitektur yang ditemukan UAT
// 2026-09-04: migrasi 000095 menambah kolom payment_schedules.land_sale_id
// dan mengubah Execute() (internal/sale/repository.go) supaya SETIAP Akad baru
// yang bundled Kelebihan Tanah otomatis menulis satu baris payment_schedules
// (type=land) — tapi migrasi itu murni skema, TIDAK PERNAH membackfill
// land_sales yang Akad-nya terjadi SEBELUM perubahan tsb dipasang.
//
// Akibatnya land_sales lama itu punya DUA wajah yang saling bertentangan:
//   - internal/land/receivable.go (Piutang Customer / AR aging) tetap benar
//     menampilkannya sebagai piutang terbuka (gross_amount, paid_amount
//     diperlakukan 0 bila tak ada jadwal ter-link — lihat komentar di sana).
//   - internal/sale/collection.go outstandingForContract (dasar
//     total_outstanding_actual → batas maksimum "+ Catat Penerimaan" di
//     halaman detail unit) TIDAK PERNAH melihatnya — tanpa baris jadwal,
//     piutang itu tidak ada di mata mesin waterfall pembayaran sama sekali.
//
// Nyata di kasus unit X-01 (tenant 9901026, kontrak #4088, land_sale #308,
// Rp20.000): laporan Piutang Customer benar menagih Rp20.000, tapi halaman
// unit tidak menampilkan info apa pun soal piutang itu dan TIDAK ADA cara
// untuk membayarnya — persis laporan UAT yang memicu perbaikan ini.
//
// Perintah ini menulis baris payment_schedules yang HILANG itu, dengan bentuk
// IDENTIK yang dibuat Execute() untuk Akad baru (InstallmentNumber 9999,
// Status scheduled, ScheduleVersion 1) — sehingga land_sale lama langsung
// masuk waterfall ReceivePayment/planAllocation yang sama, tanpa mesin kedua.
//
// PaidAmount SELALU dimulai dari 0 (status scheduled, bukan received) KECUALI
// rekonsiliasi generik (identik pemisahan houseAdvance/landAdvance di
// Service.RecordAkad) membuktikan sebagian sudah terbayar — dalam hal itu
// kandidat DILEWATI dengan peringatan, BUKAN ditebak: menulis paid_amount > 0
// tanpa baris payment_allocations/credit_applications yang menyertainya
// melanggar Guard #1 (lihat credit_repository.go) dan butuh peninjauan
// akuntansi manual, bukan skrip generik.
//
// Kandidat = land_sales berstatus akad, terhubung ke SATU unit lewat
// sale_contracts.land_reservation_id (land_sale berdiri sendiri tanpa unit
// TIDAK bisa punya baris payment_schedules — kolom unit_id/sale_contract_id
// NOT NULL), dan BELUM punya baris payment_schedules (type=land) yang hidup
// (status != superseded).
//
// Idempoten: kandidat yang sudah punya jadwal otomatis tersaring oleh LEFT
// JOIN ... IS NULL di findCandidates — jalan kedua tidak menemukan apa pun.
// Append-only: hanya INSERT baris baru, tidak pernah mengubah/menghapus.
//
// Default DRY-RUN. Contoh:
//
//	go run ./cmd/backfill-land-schedule                  # dry-run semua tenant
//	go run ./cmd/backfill-land-schedule --tenant 9901026  # dry-run satu tenant
//	go run ./cmd/backfill-land-schedule --tenant 9901026 --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
	"esaproperti/internal/sale"
)

type candidate struct {
	LandSaleID      uint64
	TenantID        uint64
	SaleContractID  uint64
	UnitID          uint64
	UnitCode        string
	LandGross       domain.Money
	RecognitionDate time.Time
	// LandPaidCandidate: dugaan sebagian sudah terbayar (Σtermin unit − harga
	// rumah, dibatasi 0..LandGross) — lihat komentar paket. > 0 berarti
	// kandidat ini DILEWATI (butuh peninjauan manual, bukan tebakan skrip).
	LandPaidCandidate domain.Money
}

func findCandidates(ctx context.Context, gdb *gorm.DB, tenantID uint64) ([]candidate, error) {
	var rows []struct {
		LandSaleID      uint64
		TenantID        uint64
		SaleContractID  uint64
		UnitID          uint64
		UnitCode        string
		LandGross       domain.Money `gorm:"column:land_gross"`
		HouseGross      domain.Money `gorm:"column:house_gross"`
		RecognitionDate time.Time
	}
	q := gdb.WithContext(ctx).Table("land_sales AS ls").
		Joins("JOIN sale_contracts sc ON sc.land_reservation_id = ls.reservation_id AND sc.tenant_id = ls.tenant_id").
		Joins("JOIN units u ON u.id = sc.unit_id AND u.tenant_id = ls.tenant_id").
		Joins(`LEFT JOIN payment_schedules ps ON ps.land_sale_id = ls.id AND ps.tenant_id = ls.tenant_id AND ps.status != 'superseded'`).
		Where("ls.status = ?", "akad").
		Where("ps.id IS NULL").
		Select(`ls.id AS land_sale_id, ls.tenant_id AS tenant_id, sc.id AS sale_contract_id,
			sc.unit_id AS unit_id, u.code AS unit_code, ls.gross_amount AS land_gross,
			sc.gross_amount AS house_gross, ls.recognition_date AS recognition_date`)
	if tenantID != 0 {
		q = q.Where("ls.tenant_id = ?", tenantID)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("cari kandidat: %w", err)
	}

	out := make([]candidate, 0, len(rows))
	for _, r := range rows {
		var collected domain.Money
		if err := gdb.WithContext(ctx).Table("termin_payments").
			Where("tenant_id = ? AND unit_id = ? AND counts_toward_price = ?", r.TenantID, r.UnitID, true).
			Select("COALESCE(SUM(amount), 0)").Scan(&collected).Error; err != nil {
			return nil, fmt.Errorf("hitung termin unit %d: %w", r.UnitID, err)
		}

		housePaid := collected
		if housePaid.GreaterThan(r.HouseGross) {
			housePaid = r.HouseGross
		}
		landPaidCandidate := collected.Sub(housePaid)
		if landPaidCandidate.IsNeg() {
			landPaidCandidate = domain.Zero
		}
		if landPaidCandidate.GreaterThan(r.LandGross) {
			landPaidCandidate = r.LandGross
		}

		out = append(out, candidate{
			LandSaleID: r.LandSaleID, TenantID: r.TenantID, SaleContractID: r.SaleContractID,
			UnitID: r.UnitID, UnitCode: r.UnitCode, LandGross: r.LandGross,
			RecognitionDate: r.RecognitionDate, LandPaidCandidate: landPaidCandidate,
		})
	}
	return out, nil
}

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID (0 = semua tenant)")
	apply := flag.Bool("apply", false, "tulis baris payment_schedules yang hilang; default: dry-run")
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
		mode = "APPLY (menulis baris payment_schedules yang hilang)"
	}
	fmt.Printf("=== Backfill Jadwal Piutang Kelebihan Tanah (UAT 2026-09-04) · %s ===\n", mode)

	candidates, err := findCandidates(ctx, gdb, *tenantID)
	if err != nil {
		log.Fatalf("cari kandidat: %v", err)
	}
	if len(candidates) == 0 {
		fmt.Println("Tidak ada land_sale ber-Akad yang kehilangan baris payment_schedules. Selesai.")
		return
	}

	fixed, skipped := 0, 0
	for _, c := range candidates {
		if !c.LandPaidCandidate.IsZero() {
			fmt.Printf("LEWATI tenant %d unit %d (%s) land_sale #%d — rekonsiliasi generik menduga %s SUDAH terbayar; "+
				"menulis paid_amount tanpa baris payment_allocations melanggar Guard #1, butuh peninjauan akuntansi manual.\n",
				c.TenantID, c.UnitID, c.UnitCode, c.LandSaleID, c.LandPaidCandidate)
			skipped++
			continue
		}
		fmt.Printf("Tenant %d unit %d (%s) land_sale #%d — jadwal baru: Rp%s, belum dibayar\n",
			c.TenantID, c.UnitID, c.UnitCode, c.LandSaleID, c.LandGross)
		fixed++
		if !*apply {
			continue
		}
		landSaleID := c.LandSaleID
		sched := &sale.PaymentSchedule{
			TenantID:          c.TenantID,
			SaleContractID:    c.SaleContractID,
			UnitID:            c.UnitID,
			LandSaleID:        &landSaleID,
			InstallmentNumber: 9999,
			DueDate:           c.RecognitionDate,
			Amount:            c.LandGross,
			Type:              sale.ScheduleTypeLand,
			Status:            sale.ScheduleStatusScheduled,
			ScheduleVersion:   1,
		}
		if err := gdb.WithContext(ctx).Create(sched).Error; err != nil {
			log.Fatalf("simpan jadwal land_sale #%d: %v", c.LandSaleID, err)
		}
	}

	fmt.Printf("\n=== Total: %d kandidat, %d %s, %d dilewati (butuh peninjauan manual) ===\n",
		len(candidates), fixed, map[bool]string{true: "ditulis", false: "AKAN ditulis"}[*apply], skipped)
	if !*apply {
		fmt.Println("Dry-run: tidak ada perubahan yang tersimpan. Jalankan ulang dengan --apply.")
	}
}
