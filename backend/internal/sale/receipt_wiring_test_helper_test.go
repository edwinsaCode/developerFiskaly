//go:build integration

package sale_test

// Helper wiring kwitansi untuk integration test sale.
//
// W-3.2 menutup fail-open: penerimaan kas TIDAK boleh jadi tanpa generator
// kwitansi, karena kwitansi itulah dokumen bernomor yang membuktikan jurnal
// kasnya (INV-DOC-1). Konsekuensinya test yang dulu lolos dengan repo tanpa
// generator kini gagal — dan itu benar: test yang menjalankan jalur kas harus
// menjalankannya dengan wiring yang sama seperti produksi, bukan versi lumpuh.
//
// Adapter ini sengaja menyalin bentuk `receiptGenAdapter` di cmd/api/main.go:
// ReceiptService produksi yang asli, seri nomor asli, idempotensi asli.

import (
	"context"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/billing"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

type slReceiptAdapter struct{ svc *billing.ReceiptService }

func (a *slReceiptAdapter) GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64,
	amount domain.Money, bankAccountCode string, date time.Time, notes string) (string, uint64, error) {
	rec, err := a.svc.GenerateReceiptInTx(ctx, tx, tenantID, createdBy, terminID, unitID, amount, bankAccountCode, date, notes)
	if err != nil {
		return "", 0, err
	}
	return rec.ReceiptNumber, rec.ID, nil
}

// slWireReceipts memasang generator kwitansi produksi ke repo sale.
func slWireReceipts(db *gorm.DB, repo *sale.GORMRepository) *sale.GORMRepository {
	brepo := billing.NewGORMRepository(db)
	repo.SetReceiptTxGenerator(&slReceiptAdapter{svc: billing.NewReceiptService(brepo, brepo, brepo)})
	return repo
}

// slSeedDocumentTypes menanam master jenis dokumen untuk tenant test.
// Produksi melakukannya saat tenant dibuat (cmd/api/main.go); resolver
// fail-closed, jadi tenant test yang melewatkannya tidak bisa menerbitkan
// dokumen apa pun.
func slSeedDocumentTypes(db *gorm.DB, tenantID uint64) error {
	return document.SeedDefaultDocumentTypes(context.Background(), db, tenantID)
}
