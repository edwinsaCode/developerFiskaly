-- W-13 — Pusatkan Penerimaan: "Jenis Penerimaan" terstruktur di +Catat Penerimaan.
--
-- Klien: form "+ Catat Penerimaan" harus punya jenis terstruktur (DP/Cicilan
-- N/Pelunasan/Lainnya/Pencairan Dana Bank) — bukan teks bebas di `description`.
-- `payment_source` TIDAK bisa dipakai untuk ini: ia mengkodekan ASAL/ROUTING
-- akuntansi (collection|schedule_received|unit_termin|kpr_disbursement|...),
-- bukan label bisnis yang dipilih user. Kolom baru murni deskriptif — tidak
-- pernah dibaca oleh resolver akun/jurnal/gate, jadi baris lama (default
-- 'other') tetap benar tanpa backfill.
ALTER TABLE termin_payments
    ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'other' AFTER payment_source,
    ADD COLUMN installment_no SMALLINT UNSIGNED NULL AFTER kind;
