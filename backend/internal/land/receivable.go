package land

// Piutang Kelebihan Tanah — adapter ke mesin piutang bersama (W-4/receivable).
//
// Bug ditemukan 2026-08-31: land_sales sebelumnya tidak pernah dibaca oleh AR
// aging / collection dashboard sama sekali (walau jurnalnya sudah benar
// terposting ke GL sejak PrepareBundledAkad/RecordAkad). ReceivableRows adalah
// SATU-SATUNYA jalan land_sales muncul di laporan Piutang Customer — tidak ada
// bucketing atau perhitungan telat di sini, seluruhnya diserahkan ke
// receivable.BuildAging (INV-AR-1), pola identik legacyar.Service.ReceivableRows.
//
// PaidAmount/Received sekarang dihitung dari payment_schedules yang ter-link
// via land_sale_id (migrasi 000095) — land_sale menjadi AR anchor yang sah di
// mesin alokasi pembayaran yang sama dengan unit (lihat sale.Service.
// ReceivePayment / CommitPayment), bukan lagi hardcode "belum dibayar".
import (
	"context"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/receivable"
)

// LandSaleReceivable is the row shape ListReceivableLandSales scans into via
// raw SQL join (land_sales + customers + sale_contracts + units) — column
// tags match the query's aliases.
type LandSaleReceivable struct {
	ID              uint64       `gorm:"column:id"`
	QuantityM2      string       `gorm:"column:quantity_m2"`
	GrossAmount     domain.Money `gorm:"column:gross_amount"`
	RecognitionDate time.Time    `gorm:"column:recognition_date"`
	CustomerName    string       `gorm:"column:customer_name"`
	CustomerPhone   string       `gorm:"column:customer_phone"`
	CustomerEmail   string       `gorm:"column:customer_email"`
	ContractID      *uint64      `gorm:"column:contract_id"`
	UnitID          *uint64      `gorm:"column:unit_id"`
	UnitCode        *string      `gorm:"column:unit_code"`
	// PaidAmount: akumulasi pembayaran atas payment_schedules baris "land"
	// yang ter-link ke land_sale ini (migrasi 000095, land_sale_id). NULL
	// bila belum ada jadwal ter-link sama sekali (data lama pra-migrasi,
	// atau race sangat sempit sebelum Execute() membuat schedule-nya) —
	// diperlakukan sebagai belum dibayar.
	PaidAmount     *domain.Money `gorm:"column:paid_amount"`
	ScheduleStatus *string       `gorm:"column:schedule_status"`
}

// ReceivableRows menerjemahkan land_sales berstatus akad menjadi baris mesin
// piutang bersama. Dipasang ke reporting.Service lewat SetLandReceivable di
// main.go, sehingga paket ini tidak pernah meng-import paket laporan.
func (s *Service) ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error) {
	recs, err := s.store.ListReceivableLandSales(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]receivable.Row, 0, len(recs))
	for i := range recs {
		r := recs[i]
		var contractID, unitID uint64
		var unitCode string
		if r.ContractID != nil {
			contractID = *r.ContractID
		}
		if r.UnitID != nil {
			unitID = *r.UnitID
		}
		if r.UnitCode != nil {
			unitCode = *r.UnitCode
		}
		paidAmount := domain.Zero
		if r.PaidAmount != nil {
			paidAmount = *r.PaidAmount
		}
		// "received": string literal, bukan import sale.ScheduleStatusReceived —
		// sale sudah mengimpor land (WithLandAkadPreparer dkk), jadi land tidak
		// boleh balik mengimpor sale (aturan import CLAUDE.md: tidak ada siklik).
		received := r.ScheduleStatus != nil && *r.ScheduleStatus == "received"
		out = append(out, receivable.Row{
			Source:     receivable.SourceAddon,
			RefID:      r.ID,
			ContractID: contractID,
			UnitID:     unitID,
			BuyerName:  r.CustomerName,
			BuyerPhone: r.CustomerPhone,
			BuyerEmail: r.CustomerEmail,
			UnitCode:   unitCode,
			Label:      "Kelebihan Tanah " + r.QuantityM2 + " m²",
			DueDate:    r.RecognitionDate,
			Amount:     r.GrossAmount,
			PaidAmount: paidAmount,
			Received:   received,
		})
	}
	return out, nil
}
