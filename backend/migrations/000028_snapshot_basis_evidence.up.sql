-- P0-3 (lanjutan) — bukti basis alokasi di tiap baris snapshot HPP.
--
-- Melengkapi audit trail: setiap allocation_snapshot_line kini menyimpan DERIVASI
-- angka HPP-nya secara mandiri (self-describing), bukan hanya hasil akhirnya:
--   basis_type            : basis yang dipakai (saleable_area | sales_value)
--   basis_value           : bobot unit ini pada basis tsb (sqm ATAU rupiah list_price)
--   allocation_percentage : porsi unit ini terhadap TOTAL basis (0..100), yaitu
--                           basis_value / Σ(basis_value semua unit) × 100.
--
-- Dengan ini seorang auditor bisa membaca satu baris dan memverifikasi:
--   amount ≈ pool_kelas × allocation_percentage%   (deviasi ≤ 1 rupiah karena
--   largest-remainder — amount adalah nilai yang ACTUAL dialokasikan, sedangkan
--   allocation_percentage adalah rasio bobot yang MENDASARINYA).
--
-- Ketiganya bernilai sama untuk keempat baris (accounting_class) dalam satu
-- snapshot — sengaja didenormalisasi ke level baris agar tiap baris berdiri
-- sendiri sebagai bukti. Tabel append-only (Invariant #5) — kolom baru mengikuti.
--
-- Aman ditambahkan: allocation_snapshot_lines baru diperkenalkan di 000027 dan
-- belum menyimpan data produksi.

ALTER TABLE allocation_snapshot_lines
    ADD COLUMN basis_type            VARCHAR(20)    NOT NULL DEFAULT ''            AFTER inventory_account_code,
    ADD COLUMN basis_value           DECIMAL(20,4)  NOT NULL DEFAULT '0.0000'     AFTER basis_type,
    ADD COLUMN allocation_percentage DECIMAL(9,6)   NOT NULL DEFAULT '0.000000'   AFTER basis_value;
