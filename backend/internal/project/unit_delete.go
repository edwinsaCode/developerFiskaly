package project

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ── Hapus unit (hanya unit yang belum pernah dipakai) ─────────────────────────
//
// Kebutuhan: admin kadang membuat unit berlebih dalam satu blok (wizard bulk)
// dan perlu membuang unit yang salah. Unit tidak punya soft-delete; hard delete
// aman HANYA bila tidak ada satu baris pun di tabel lain yang menunjuk unit itu.
//
// Sebagian besar referensi ke units.id adalah FK logis (tanpa constraint DB),
// jadi database sendiri TIDAK akan menolak — daftar di bawah inilah penjaganya.
// Tidak ada cascade: unit yang punya histori ditolak dengan alasan per-relasi
// (Invariant #5 — histori tidak pernah dihapus). Integration test
// TestIntegration_DeleteUnit_GuardCoversEveryUnitIDColumn memastikan setiap
// kolom `unit_id` di schema tercakup di sini; tabel baru tanpa entri → test merah.

type unitUsageCheck struct {
	Table  string
	Reason string
}

var unitUsageChecks = []unitUsageCheck{
	{"unit_status_transitions", "sudah memiliki riwayat perubahan status"},
	{"bookings", "sudah memiliki booking"},
	{"sale_contracts", "sudah memiliki kontrak penjualan"},
	{"sale_records", "sudah tercatat penjualan (BAST)"},
	{"payment_schedules", "sudah memiliki jadwal pembayaran"},
	{"termin_payments", "sudah menerima pembayaran termin"},
	{"receipts", "sudah memiliki kwitansi/penerimaan"},
	{"credit_applications", "sudah memiliki pengajuan KPR"},
	{"cancellations", "sudah memiliki pembatalan"},
	{"refunds", "sudah memiliki refund"},
	{"commissions", "sudah memiliki komisi"},
	{"notary_deposits", "sudah memiliki titipan notaris"},
	{"charge_groups", "sudah memiliki tagihan tambahan"},
	{"charge_receivable_recognitions", "sudah memiliki pengakuan piutang tagihan"},
	{"tax_obligations", "sudah memiliki kewajiban pajak"},
	{"journal_lines", "sudah tercatat di jurnal akuntansi"},
	{"cost_entries", "sudah memiliki biaya langsung"},
	{"allocation_snapshots", "sudah masuk snapshot alokasi HPP"},
	{"allocation_snapshot_lines", "sudah masuk alokasi biaya HPP"},
	{"hpp_trueup_lines", "sudah masuk true-up HPP"},
}

// UnitUsageTables mengembalikan tabel `unit_id` yang diperiksa sebelum hapus.
// Dipakai integration test untuk mendeteksi tabel baru yang belum dijaga.
func UnitUsageTables() []string {
	out := make([]string, len(unitUsageChecks))
	for i, c := range unitUsageChecks {
		out[i] = c.Table
	}
	return out
}

// DeleteUnitIfUnused menghapus unit dalam SATU transaksi setelah memastikan
// unit tidak punya relasi apa pun. Baris unit dikunci (FOR UPDATE) sehingga
// transisi/booking yang berjalan bersamaan (keduanya meng-UPDATE units) antre
// di belakang pemeriksaan ini; FK bookings/cancellations/transitions menjadi
// jaring pengaman terakhir.
func (r *GORMRepository) DeleteUnitIfUnused(ctx context.Context, tenantID, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u Unit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&u, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
			return ErrUnitNotFound
		}

		var reasons []string
		if u.Status != UnitStatusAvailable {
			reasons = append(reasons, fmt.Sprintf("status unit %q (hanya unit available yang bisa dihapus)", u.Status))
		}
		if u.BuyerRef != nil || u.SaleDate != nil || u.SalePrice != nil {
			reasons = append(reasons, "sudah memiliki data pembeli/penjualan")
		}
		for _, c := range unitUsageChecks {
			q := tx.Table(c.Table).Where("tenant_id = ? AND unit_id = ?", tenantID, id)
			if c.Table == "unit_status_transitions" {
				// Baris backfill (migrasi 000043) = titik awal audit sintetis
				// untuk unit lama, bukan pemakaian. Tanpa pengecualian ini
				// seluruh unit pra-migrasi tak bisa dihapus meski belum dipakai.
				q = q.Where("event <> ?", string(EventBackfill))
			}
			var n int64
			if err := q.Count(&n).Error; err != nil {
				return fmt.Errorf("cek relasi %s: %w", c.Table, err)
			}
			if n > 0 {
				reasons = append(reasons, c.Reason)
			}
		}
		// Approval polimorfik (target_type/target_id), bukan kolom unit_id.
		var nApproval int64
		if err := tx.Table("approval_requests").
			Where("tenant_id = ? AND target_type = ? AND target_id = ?", tenantID, "unit_transition", id).
			Count(&nApproval).Error; err != nil {
			return fmt.Errorf("cek relasi approval_requests: %w", err)
		}
		if nApproval > 0 {
			reasons = append(reasons, "sudah memiliki permintaan approval")
		}
		if len(reasons) > 0 {
			return &UnitInUseError{Reasons: reasons}
		}

		// Satu-satunya baris yang ikut terhapus: backfill sintetis unit ini
		// (FK fk_ust_unit). Isinya hanya "unit ini ada & available" — gugur
		// bersama unitnya. Transisi nyata sudah menolak di atas.
		if err := tx.Where("tenant_id = ? AND unit_id = ? AND event = ?", tenantID, id, string(EventBackfill)).
			Delete(&UnitStatusTransition{}).Error; err != nil {
			return fmt.Errorf("hapus backfill transisi: %w", err)
		}
		res := tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&Unit{})
		if res.Error != nil {
			return fmt.Errorf("DeleteUnitIfUnused: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrUnitNotFound
		}
		return nil
	})
}
