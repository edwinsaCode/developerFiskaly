package project

import (
	"context"
	"fmt"
)

// ── Nama pemegang unit (satu definisi untuk semua layar) ──────────────────────
//
// Keluhan yang dijawab file ini: layar hanya menampilkan "C-01", sehingga tidak
// ada cara tahu unit itu sudah dipesan/dibeli siapa tanpa membuka penjualan.
//
// Nama pembelinya SUDAH ada di sistem jauh sebelum BAST — masalahnya tersebar:
//
//   - `units.buyer_ref` hanya terisi saat BAST. Unit `booked`/`ppjb` NULL.
//   - `sale_contracts.buyer_name` (+ `customer_id`) terisi sejak kontrak.
//   - `bookings.customer_id` terisi sejak booking, sebelum ada kontrak.
//
// Kalau tiap layar menyusun join-nya sendiri, tiap layar berpeluang menjawab
// beda untuk unit yang sama. Jadi urutannya dikunci SATU kali di sini, dan
// semua layar membaca `Unit.BuyerName`:
//
//	kontrak terbaru (belum dibatalkan) → booking aktif → units.buyer_ref
//
// Kontrak menang atas `buyer_ref` karena kontrak adalah dokumen komersialnya;
// `buyer_ref` cuma teks bebas yang disalin saat BAST. Di dalam kontrak,
// `buyer_name` menang atas nama master customer karena itulah nama yang
// tertulis di dokumen — master dipakai hanya bila kontraknya kosong.
//
// Unit `available` sengaja dikosongkan: setelah pembatalan, kontrak lamanya
// masih ada (scheme_state = 'cancelled', ledger append-only) dan tidak boleh
// tampil sebagai pemegang unit.

// BuyerSource menyebut dari mana `BuyerName` diambil, supaya layar bisa
// membedakan "sudah akad" dari "baru booking" tanpa menebak dari status.
const (
	BuyerSourceContract = "contract"
	BuyerSourceBooking  = "booking"
	BuyerSourceUnit     = "unit"
)

type buyerNameRow struct {
	UnitID      uint64
	BuyerName   string
	BuyerSource string
}

// AttachBuyerNames mengisi BuyerName/BuyerSource untuk sekumpulan unit dalam
// SATU perjalanan ke database — bukan N+1 per unit. Unit tanpa pemegang
// dibiarkan kosong (bukan error): itu keadaan normal untuk unit yang dijual.
func (r *GORMRepository) AttachBuyerNames(ctx context.Context, tenantID uint64, units []*Unit) error {
	if len(units) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(units))
	for _, u := range units {
		if u != nil {
			ids = append(ids, u.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	// ROW_NUMBER dipakai supaya satu unit dengan lebih dari satu kontrak
	// (mis. kontrak lama dibatalkan lalu dibuat ulang) menghasilkan TEPAT satu
	// baris — join polos akan menggandakan unitnya.
	const q = `
SELECT
    u.id AS unit_id,
    COALESCE(NULLIF(k.buyer_name, ''), NULLIF(k.customer_name, ''),
             NULLIF(b.customer_name, ''), NULLIF(u.buyer_ref, ''), '') AS buyer_name,
    CASE
        WHEN COALESCE(NULLIF(k.buyer_name, ''), NULLIF(k.customer_name, '')) IS NOT NULL THEN 'contract'
        WHEN NULLIF(b.customer_name, '') IS NOT NULL THEN 'booking'
        WHEN NULLIF(u.buyer_ref, '') IS NOT NULL THEN 'unit'
        ELSE ''
    END AS buyer_source
FROM units u
LEFT JOIN (
    SELECT c.unit_id, c.buyer_name, cu.name AS customer_name,
           ROW_NUMBER() OVER (PARTITION BY c.unit_id
                              ORDER BY c.contract_date DESC, c.id DESC) AS rn
    FROM sale_contracts c
    LEFT JOIN customers cu ON cu.id = c.customer_id AND cu.tenant_id = c.tenant_id
    WHERE c.tenant_id = ?
      AND COALESCE(c.scheme_state, '') <> 'cancelled'
) k ON k.unit_id = u.id AND k.rn = 1
LEFT JOIN (
    SELECT bo.unit_id, cu.name AS customer_name,
           ROW_NUMBER() OVER (PARTITION BY bo.unit_id
                              ORDER BY bo.booking_date DESC, bo.id DESC) AS rn
    FROM bookings bo
    JOIN customers cu ON cu.id = bo.customer_id AND cu.tenant_id = bo.tenant_id
    WHERE bo.tenant_id = ? AND bo.status = 'active'
) b ON b.unit_id = u.id AND b.rn = 1
WHERE u.tenant_id = ? AND u.id IN ? AND u.status <> 'available'`

	var rows []buyerNameRow
	if err := r.db.WithContext(ctx).
		Raw(q, tenantID, tenantID, tenantID, ids).Scan(&rows).Error; err != nil {
		return fmt.Errorf("AttachBuyerNames: %w", err)
	}

	byID := make(map[uint64]buyerNameRow, len(rows))
	for _, row := range rows {
		byID[row.UnitID] = row
	}
	for _, u := range units {
		if u == nil {
			continue
		}
		if row, ok := byID[u.ID]; ok && row.BuyerName != "" {
			u.BuyerName = row.BuyerName
			u.BuyerSource = row.BuyerSource
		}
	}
	return nil
}
