-- Audit kwitansi Kelebihan Tanah (2026-10-29) — perbaikan data.
--
-- Bug: penerimaan Kelebihan Tanah (kind='land_excess') SEBELUM Akad tersimpan
-- dengan counts_toward_price=TRUE. SumTerminsByUnit menjumlahkannya sebagai
-- pembayaran HARGA UNIT, sehingga sisa harga unit turun (klien: 185jt − DP 6jt
-- − tanah 45jt = 134jt, seharusnya 179jt) dan sisa Kelebihan Tanah tetap
-- penuh. Kode yang sudah diperbaiki menyimpan termin semacam ini sebagai
-- counts_toward_price=FALSE + satu alokasi buyer_credit (uang muka tanah,
-- dinetkan ke piutang tanah saat Akad).
--
-- Migrasi ini HANYA membalik flag pada termin yang bentuk datanya sudah sama
-- persis dengan hasil kode baru — selain flag, tidak ada yang perlu diubah:
--   * kind = 'land_excess' dan counts_toward_price = TRUE;
--   * punya alokasi, dan SEMUA alokasinya buyer_credit (tidak pernah masuk ke
--     cicilan rumah — bila ada, kasusnya lain dan TIDAK disentuh di sini);
--   * unit belum Akad: tidak ada payment_schedules type='land' yang hidup;
--   * saldo kredit unit belum pernah dipakai (tidak ada credit_applications),
--     agar tidak ada pemakaian/netting yang sudah memperhitungkan dana ini.
--
-- Jurnal TIDAK disentuh (Dr Kas / Cr Uang Muka Penjualan sudah benar —
-- Invariant #5 & #7). payment_allocations dan payment_schedules TIDAK diubah.
-- Idempoten: setelah dijalankan, baris yang sama tidak lagi memenuhi syarat.

UPDATE termin_payments tp
SET tp.counts_toward_price = FALSE,
    tp.updated_at = NOW(3)
WHERE tp.kind = 'land_excess'
  AND tp.counts_toward_price = TRUE
  AND EXISTS (
      SELECT 1 FROM payment_allocations pa
      WHERE pa.tenant_id = tp.tenant_id
        AND pa.termin_payment_id = tp.id
        AND pa.allocation_type = 'buyer_credit'
  )
  AND NOT EXISTS (
      SELECT 1 FROM payment_allocations pa
      WHERE pa.tenant_id = tp.tenant_id
        AND pa.termin_payment_id = tp.id
        AND pa.allocation_type <> 'buyer_credit'
  )
  AND NOT EXISTS (
      SELECT 1 FROM payment_schedules ps
      WHERE ps.tenant_id = tp.tenant_id
        AND ps.unit_id = tp.unit_id
        AND ps.type = 'land'
        AND ps.status <> 'superseded'
  )
  AND NOT EXISTS (
      SELECT 1 FROM credit_applications ca
      WHERE ca.tenant_id = tp.tenant_id
        AND ca.unit_id = tp.unit_id
  );
